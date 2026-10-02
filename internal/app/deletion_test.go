package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"reelnest/internal/app"
)

type deletionPreview struct {
	Path         string `json:"path"`
	Confirmation string `json:"confirmation"`
	ExpiresAt    int64  `json:"expiresAt"`
}

func previewDeletion(t *testing.T, a *runningApp, id string) deletionPreview {
	t.Helper()
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/media/"+id+"/deletion-preview", map[string]any{}, 200)
	var result deletionPreview
	if err := json.Unmarshal(body, &result); err != nil || result.Confirmation == "" || result.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("invalid confirmation: %s", body)
	}
	return result
}
func deleteBody(p deletionPreview) map[string]any {
	return map[string]any{"confirmation": p.Confirmation, "confirmed": true}
}
func requireFile(t *testing.T, path, content string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != content {
		t.Fatalf("file changed: %s: %q %v", path, data, err)
	}
}
func requireAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("file not absent: %s %v", path, err)
	}
}
func requireAvailability(t *testing.T, a *runningApp, id string, available bool) {
	t.Helper()
	for _, file := range library(t, a) {
		if file.ID == id {
			if file.Available != available {
				t.Fatalf("wrong availability: %+v", file)
			}
			return
		}
	}
	t.Fatal("media record disappeared")
}

func TestPermanentDeletionOnlyRemovesConfirmedSingleFile(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	name := filepath.Join(root, "版本 & 中文", "Dark.S01E01.webm")
	keep := map[string]string{
		filepath.Join(root, "版本 & 中文", "Dark.S01E01.srt"):        "isolated subtitle",
		filepath.Join(root, "版本 & 中文", "Dark.S01E01.jpg"):        "isolated poster",
		filepath.Join(root, "版本 & 中文", "Dark.S01E01.1080p.webm"): "other version",
		filepath.Join(root, "版本 & 中文", "Dark.S01E02.webm"):       "other episode",
	}
	writeMedia(t, name, "original fixture")
	for path, content := range keep {
		writeMedia(t, path, content)
	}
	a := start(t, app.Config{DataDir: data, MediaRoots: []string{root}, Username: "family", Password: password})
	login(t, a)
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]string{"name": "测试媒体库", "path": root, "kind": "series"}, 201)
	var dir directory
	_ = json.Unmarshal(body, &dir)
	awaitScan(t, a, dir.ID, "completed")
	var file mediaFile
	for _, f := range library(t, a) {
		if f.Path == name {
			file = f
		}
	}
	if file.ID == "" || file.Episode == nil {
		t.Fatal("episode fixture not organized")
	}
	base := a.server.URL + "/api/media/" + file.ID
	saveProgress(t, a, file.ID, 23, false)
	body, _ = request(t, a.client, "POST", base+"/stream-link", map[string]any{}, 200)
	var stream struct{ Path string }
	_ = json.Unmarshal(body, &stream)
	p := previewDeletion(t, a, file.ID)
	if p.Path != name {
		t.Fatalf("wrong target: %s", p.Path)
	}
	requireFile(t, name, "original fixture") // Merely requesting or abandoning preview never deletes.
	requireAvailability(t, a, file.ID, true)
	request(t, a.client, "POST", base+"/delete", map[string]any{}, 409)
	request(t, a.client, "POST", base+"/delete", map[string]any{"confirmation": p.Confirmation, "confirmed": false}, 409)
	requireFile(t, name, "original fixture")
	body, _ = request(t, a.client, "POST", base+"/delete", deleteBody(p), 200)
	var outcome struct {
		Deleted, IndexUpdated bool
		Message               string
	}
	_ = json.Unmarshal(body, &outcome)
	if !outcome.Deleted || !outcome.IndexUpdated || !strings.Contains(outcome.Message, "本系统无法恢复") {
		t.Fatalf("wrong outcome: %s", body)
	}
	requireAbsent(t, name)
	for path, content := range keep {
		requireFile(t, path, content)
	}
	if stat, err := os.Stat(filepath.Dir(name)); err != nil || !stat.IsDir() {
		t.Fatal("parent removed")
	}
	requireAvailability(t, a, file.ID, false)
	if progress(t, a, file.ID).Position != 23 {
		t.Fatal("progress unexpectedly removed")
	}
	for _, suffix := range []string{"/content", "/playback", "/subtitles"} {
		request(t, a.client, "GET", base+suffix, nil, 410)
	}
	readContent(t, http.DefaultClient, "GET", a.server.URL+stream.Path, "bytes=0-2", 410)
	request(t, a.client, "POST", base+"/delete", deleteBody(p), 410)
	rescan(t, a, dir.ID)
	awaitScan(t, a, dir.ID, "completed")
	request(t, a.client, "GET", base, nil, 404)
	a.close()
	b := start(t, app.Config{DataDir: data, MediaRoots: []string{root}})
	login(t, b)
	request(t, b.client, "GET", b.server.URL+"/api/media/"+file.ID, nil, 404)
	for path, content := range keep {
		requireFile(t, path, content)
	}
	// Reusing the old pathname cannot make an old confirmation delete its new occupant.
	writeMedia(t, name, "new unrelated occupant")
	request(t, b.client, "POST", b.server.URL+"/api/media/"+file.ID+"/delete", deleteBody(p), 404)
	rescan(t, b, dir.ID)
	awaitScan(t, b, dir.ID, "completed")
	var replacementID string
	for _, item := range library(t, b) {
		if item.Path == name && item.Available {
			replacementID = item.ID
		}
	}
	if replacementID == "" || replacementID == file.ID {
		t.Fatal("replacement inherited deleted identity")
	}
	request(t, b.client, "POST", b.server.URL+"/api/media/"+replacementID+"/delete", deleteBody(p), 409)
	requireFile(t, name, "new unrelated occupant")
}

func TestDeletionRequiresSessionAndExactConfirmation(t *testing.T) {
	root := t.TempDir()
	writeMedia(t, filepath.Join(root, "one.mp4"), "one")
	writeMedia(t, filepath.Join(root, "two.mp4"), "two")
	a := start(t, app.Config{DataDir: t.TempDir(), MediaRoots: []string{root}, Username: "family", Password: password})
	cookie := login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	files := library(t, a)
	base := a.server.URL + "/api/media/" + files[0].ID
	p := previewDeletion(t, a, files[0].ID)
	body, _ := request(t, a.client, "POST", base+"/stream-link", map[string]any{}, 200)
	var stream struct{ Path string }
	_ = json.Unmarshal(body, &stream)
	u, _ := url.Parse(stream.Path)
	for _, suffix := range []string{"/deletion-preview", "/delete"} {
		request(t, http.DefaultClient, "POST", base+suffix, deleteBody(p), 401)
		request(t, http.DefaultClient, "POST", base+suffix+"?"+u.RawQuery, deleteBody(p), 401)
	}
	request(t, a.client, "POST", a.server.URL+"/api/media/"+files[1].ID+"/delete", deleteBody(p), 409)
	for _, confirmation := range []string{p.Confirmation + "0", "0." + strings.Repeat("0", 64), strings.Split(p.Confirmation, ".")[0] + "." + strings.Repeat("0", 64), u.Query().Get("token")} {
		request(t, a.client, "POST", base+"/delete", map[string]any{"confirmed": true, "confirmation": confirmation}, 409)
	}
	for _, payload := range []map[string]any{
		{"path": filepath.Join(root, "one.mp4"), "confirmed": true, "confirmation": p.Confirmation},
		{"paths": []string{files[0].Path, files[1].Path}},
	} {
		request(t, a.client, "POST", base+"/delete", payload, 400)
	}
	request(t, a.client, "POST", a.server.URL+"/api/media/not-managed/deletion-preview", map[string]any{}, 404)
	request(t, a.client, "POST", a.server.URL+"/api/media/%2e%2e%2foutside.mp4/deletion-preview", map[string]any{}, 404)
	payload, _ := json.Marshal(deleteBody(p))
	req, _ := http.NewRequest("POST", base+"/delete", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://untrusted.example")
	response, err := a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-site deletion allowed")
	}
	request(t, a.client, "POST", a.server.URL+"/api/logout", map[string]any{}, 200)
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse(a.server.URL)
	jar.SetCookies(origin, []*http.Cookie{cookie})
	for _, suffix := range []string{"/deletion-preview", "/delete"} {
		request(t, &http.Client{Jar: jar}, "POST", base+suffix, deleteBody(p), 401)
	}
	requireFile(t, files[0].Path, "one")
	requireFile(t, files[1].Path, "two")
}

func TestDeletionRejectsChangedMissingAndUnsafeTargets(t *testing.T) {
	for _, scenario := range []string{"missing", "replacement", "in-place", "restored-mtime", "directory", "fifo", "symlink-inside", "symlink-outside", "parent-symlink", "moved-parent", "mount-missing", "mount-replaced", "no-write-permission"} {
		t.Run(scenario, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "parent", "one.mp4")
			writeMedia(t, path, "original")
			writeMedia(t, filepath.Join(root, "safe.txt"), "inside preserved")
			writeMedia(t, filepath.Join(outside, "safe.txt"), "outside preserved")
			a := start(t, app.Config{DataDir: t.TempDir(), MediaRoots: []string{root}, Username: "family", Password: password})
			login(t, a)
			d := addDirectory(t, a, root)
			awaitScan(t, a, d.ID, "completed")
			file := library(t, a)[0]
			p := previewDeletion(t, a, file.ID)
			want := 409
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "missing":
				must(os.Remove(path))
				want = 410
			case "replacement":
				must(os.Rename(path, path+".original"))
				writeMedia(t, path, "replacement")
			case "in-place":
				writeMedia(t, path, "changed contents")
			case "restored-mtime":
				info, err := os.Stat(path)
				must(err)
				writeMedia(t, path, "modified")
				must(os.Chtimes(path, info.ModTime(), info.ModTime()))
			case "directory":
				must(os.Rename(path, path+".original"))
				must(os.Mkdir(path, 0755))
			case "fifo":
				must(os.Rename(path, path+".original"))
				must(syscall.Mkfifo(path, 0600))
			case "symlink-inside", "symlink-outside":
				must(os.Rename(path, path+".original"))
				target := filepath.Join(root, "safe.txt")
				if scenario == "symlink-outside" {
					target = filepath.Join(outside, "safe.txt")
				}
				must(os.Symlink(target, path))
			case "parent-symlink":
				must(os.Rename(filepath.Dir(path), filepath.Join(outside, "moved")))
				must(os.Symlink(filepath.Join(outside, "moved"), filepath.Dir(path)))
			case "moved-parent":
				must(os.Rename(filepath.Dir(path), filepath.Join(root, "moved")))
				must(os.Mkdir(filepath.Dir(path), 0755))
				must(os.Link(filepath.Join(root, "moved", "one.mp4"), path))
			case "mount-missing", "mount-replaced":
				must(os.Rename(root, filepath.Join(outside, "mount")))
				t.Cleanup(func() { _ = os.Remove(root); _ = os.Rename(filepath.Join(outside, "mount"), root) })
				if scenario == "mount-replaced" {
					must(os.Mkdir(root, 0755))
				}
			case "no-write-permission":
				if os.Geteuid() == 0 {
					t.Skip("requires non-root filesystem permissions")
				}
				must(os.Chmod(filepath.Dir(path), 0555))
				t.Cleanup(func() { _ = os.Chmod(filepath.Dir(path), 0755) })
				want = 403
			}
			body, _ := request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/delete", deleteBody(p), want)
			if !strings.Contains(string(body), "未删除") {
				t.Fatalf("ambiguous failure: %s", body)
			}
			requireAvailability(t, a, file.ID, true) // Failure cannot pretend a successful delete/index update.
			if scenario == "mount-missing" || scenario == "mount-replaced" {
				requireFile(t, filepath.Join(outside, "mount", "parent", "one.mp4"), "original")
			} else {
				requireFile(t, filepath.Join(root, "safe.txt"), "inside preserved")
			}
			requireFile(t, filepath.Join(outside, "safe.txt"), "outside preserved")
			switch scenario {
			case "replacement":
				requireFile(t, path, "replacement")
				requireFile(t, path+".original", "original")
			case "in-place":
				requireFile(t, path, "changed contents")
			case "restored-mtime":
				requireFile(t, path, "modified")
			case "parent-symlink":
				requireFile(t, filepath.Join(outside, "moved", "one.mp4"), "original")
			case "moved-parent", "no-write-permission":
				requireFile(t, path, "original")
			case "directory", "fifo", "symlink-inside", "symlink-outside":
				requireFile(t, path+".original", "original")
				if _, err := os.Lstat(path); err != nil {
					t.Fatal("replacement removed", err)
				}
			}
		})
	}
}

// A second SQLite connection produces real persistent-store failures/corruption;
// no application database or deletion functions are replaced.
func externalDB(t *testing.T, data string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(data, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}
func TestDeletionRejectsCorruptIndexedPaths(t *testing.T) {
	for _, relative := range []string{"../outside.mp4", "/outside.mp4", "safe.txt", "parent/../one.mp4", "."} {
		t.Run(relative, func(t *testing.T) {
			root, data := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "one.mp4")
			writeMedia(t, path, "original")
			writeMedia(t, filepath.Join(root, "safe.txt"), "not video")
			a := start(t, app.Config{DataDir: data, MediaRoots: []string{root}, Username: "family", Password: password})
			login(t, a)
			d := addDirectory(t, a, root)
			awaitScan(t, a, d.ID, "completed")
			file := library(t, a)[0]
			p := previewDeletion(t, a, file.ID)
			if _, err := externalDB(t, data).Exec("UPDATE media_files SET relative_path=? WHERE id=?", relative, file.ID); err != nil {
				t.Fatal(err)
			}
			request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/delete", deleteBody(p), 409)
			requireFile(t, path, "original")
			requireFile(t, filepath.Join(root, "safe.txt"), "not video")
		})
	}
}

func TestDeletionReportsRealIndexWriteFailureAndScanRepairs(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "one.mp4")
	writeMedia(t, path, "original")
	a := start(t, app.Config{DataDir: data, MediaRoots: []string{root}, Username: "family", Password: password})
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	file := library(t, a)[0]
	p := previewDeletion(t, a, file.ID)
	db := externalDB(t, data)
	if _, err := db.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec("ROLLBACK") })
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/delete", deleteBody(p), 200)
	var result struct {
		Deleted, IndexUpdated bool
		Message               string
	}
	_ = json.Unmarshal(body, &result)
	if !result.Deleted || result.IndexUpdated || !strings.Contains(result.Message, "索引未更新") {
		t.Fatalf("wrong partial outcome: %s", body)
	}
	requireAbsent(t, path)
	if _, err := db.Exec("ROLLBACK"); err != nil {
		t.Fatal(err)
	}
	requireAvailability(t, a, file.ID, true) // Committed index actually stayed unchanged.
	request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID+"/content", nil, 410)
	request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/delete", deleteBody(p), 410)
	rescan(t, a, d.ID)
	awaitScan(t, a, d.ID, "completed")
	request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID, nil, 404)
}

func TestConcurrentDeletionAndScanCannotResurrectFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "one.mp4")
	writeMedia(t, path, "original")
	a := start(t, app.Config{DataDir: t.TempDir(), MediaRoots: []string{root}, Username: "family", Password: password})
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	file := library(t, a)[0]
	p := previewDeletion(t, a, file.ID)
	for i := 0; i < 300; i++ {
		writeMedia(t, filepath.Join(root, fmt.Sprintf("others/%04d.mp4", i)), "keep")
	}
	rescan(t, a, d.ID)
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func() {
			payload, _ := json.Marshal(deleteBody(p))
			req, _ := http.NewRequest("POST", a.server.URL+"/api/media/"+file.ID+"/delete", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			response, err := a.client.Do(req)
			if err != nil {
				results <- 0
				return
			}
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			results <- response.StatusCode
		}()
	}
	one, two := <-results, <-results
	if !((one == 200 && two == 410) || (one == 410 && two == 200)) {
		t.Fatalf("duplicate outcomes %d %d", one, two)
	}
	awaitScan(t, a, d.ID, "completed")
	requireAbsent(t, path)
	requireAvailability(t, a, file.ID, false)
	for i := 0; i < 300; i++ {
		requireFile(t, filepath.Join(root, fmt.Sprintf("others/%04d.mp4", i)), "keep")
	}
}
