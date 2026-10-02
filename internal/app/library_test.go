package app_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reelnest/internal/app"
)

type directory struct {
	ID         string `json:"id"`
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	ScanStatus string `json:"scanStatus"`
	ScanError  string `json:"scanError"`
}

type mediaFile struct {
	Episode            *episodeReference `json:"episode"`
	OrganizationReason string            `json:"organizationReason"`
	ID                 string            `json:"id"`
	Filename           string            `json:"filename"`
	Path               string            `json:"path"`
	Available          bool              `json:"available"`
	Matched            bool              `json:"matched"`
	Kind               string            `json:"kind"`
}

func writeMedia(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func addDirectory(t *testing.T, a *runningApp, path string) directory {
	t.Helper()
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]string{"path": path, "kind": "movie"}, 201)
	var result directory
	if err := json.Unmarshal(body, &result); err != nil || result.ID == "" {
		t.Fatalf("invalid directory: %s", body)
	}
	return result
}

func awaitScan(t *testing.T, a *runningApp, id, expected string) directory {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body, _ := request(t, a.client, "GET", a.server.URL+"/api/directories", nil, 200)
		var result struct {
			Directories []directory `json:"directories"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		for _, dir := range result.Directories {
			if dir.ID == id && dir.ScanStatus != "running" && dir.ScanStatus != "pending" {
				if dir.ScanStatus != expected {
					t.Fatalf("scan %s: got %s want %s (%s)", id, dir.ScanStatus, expected, dir.ScanError)
				}
				return dir
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scan did not reach an observable terminal status")
	return directory{}
}

func library(t *testing.T, a *runningApp) []mediaFile {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/library", nil, 200)
	var result struct {
		Items []mediaFile `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result.Items
}

func rescan(t *testing.T, a *runningApp, id string) {
	t.Helper()
	request(t, a.client, "POST", a.server.URL+"/api/directories/"+id+"/scan", map[string]string{}, 202)
}

func TestDirectoryAutomaticAndManualScanKeepsFileIdentity(t *testing.T) {
	root := t.TempDir()
	data := t.TempDir()
	writeMedia(t, filepath.Join(root, "未识别 & collection", "random name.MKV"), "isolated video fixture")
	writeMedia(t, filepath.Join(root, "poster.jpg"), "not a video")
	a := start(t, app.Config{DataDir: data, Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	dir := addDirectory(t, a, root)
	awaitScan(t, a, dir.ID, "completed")
	items := library(t, a)
	if len(items) != 1 || items[0].Matched || !items[0].Available || items[0].Filename != "random name.MKV" {
		t.Fatalf("unexpected library: %+v", items)
	}
	id := items[0].ID
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/media/"+id, nil, 200)
	var detail mediaFile
	if err := json.Unmarshal(body, &detail); err != nil || detail.Path != filepath.Join(root, "未识别 & collection", "random name.MKV") {
		t.Fatalf("wrong detail: %s", body)
	}
	rescan(t, a, dir.ID)
	awaitScan(t, a, dir.ID, "completed")
	if items = library(t, a); len(items) != 1 || items[0].ID != id {
		t.Fatalf("repeat scan changed identity: %+v", items)
	}
	writeMedia(t, filepath.Join(root, "another", "random name.MKV"), "another video version")
	rescan(t, a, dir.ID)
	awaitScan(t, a, dir.ID, "completed")
	if items = library(t, a); len(items) != 2 || items[0].ID == items[1].ID {
		t.Fatalf("distinct files merged: %+v", items)
	}
	a.close()
	b := start(t, app.Config{DataDir: data, MediaRoots: []string{root}})
	login(t, b)
	if items = library(t, b); len(items) != 2 {
		t.Fatalf("library lost after restart: %+v", items)
	}
	request(t, b.client, "GET", b.server.URL+"/api/media/"+id, nil, 200)
	awaitScan(t, b, dir.ID, "completed")
}

func TestSuccessfulRescanRemovesMissingMediaFromLibrary(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "待删除.mp4")
	writeMedia(t, path, "isolated video fixture")
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	file := library(t, a)[0]
	saveProgress(t, a, file.ID, 3, false)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	rescan(t, a, d.ID)
	awaitScan(t, a, d.ID, "completed")
	if items := library(t, a); len(items) != 0 {
		t.Fatalf("missing source file remained in library: %+v", items)
	}
	request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID, nil, 404)
}

func TestFailedScanPreservesRecordsAndCompleteScanRemovesMissing(t *testing.T) {
	allowed := t.TempDir()
	root := filepath.Join(allowed, "movies")
	movie := filepath.Join(root, "珍藏.mp4")
	writeMedia(t, movie, "isolated original version")
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{allowed}})
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	original := library(t, a)[0]
	// Simulate an offline mount. The fallback empty mount point must not clear the index.
	offline := filepath.Join(allowed, "offline")
	if err := os.Rename(root, offline); err != nil {
		t.Fatal(err)
	}
	rescan(t, a, d.ID)
	if state := awaitScan(t, a, d.ID, "failed"); state.ScanError == "" {
		t.Fatal("offline mount failure has no explanation")
	}
	if items := library(t, a); len(items) != 1 || items[0].ID != original.ID {
		t.Fatalf("failed scan removed records: %+v", items)
	}
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	rescan(t, a, d.ID)
	awaitScan(t, a, d.ID, "failed")
	if items := library(t, a); len(items) != 1 || !items[0].Available {
		t.Fatalf("replacement root reconciled as empty: %+v", items)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(offline, root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(movie); err != nil {
		t.Fatal(err)
	}
	rescan(t, a, d.ID)
	awaitScan(t, a, d.ID, "completed")
	if items := library(t, a); len(items) != 0 {
		t.Fatalf("missing file remained in library: %+v", items)
	}
	request(t, a.client, "GET", a.server.URL+"/api/media/"+original.ID, nil, 404)
	writeMedia(t, movie, "isolated replacement with different contents and length")
	rescan(t, a, d.ID)
	awaitScan(t, a, d.ID, "completed")
	items := library(t, a)
	if len(items) != 1 {
		t.Fatalf("replacement must have a new identity: %+v", items)
	}
	if !items[0].Available || items[0].ID == original.ID {
		t.Fatal("old identity reused for replacement")
	}
}

func TestDirectoryAuthorizationAndFilesystemBoundaries(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeMedia(t, filepath.Join(root, "inside", "real.mp4"), "real video")
	writeMedia(t, filepath.Join(outside, "private.mp4"), "outside file never exposed")
	for link, target := range map[string]string{
		filepath.Join(root, "escape.mp4"):        filepath.Join(outside, "private.mp4"),
		filepath.Join(root, "escaped-directory"): outside,
		filepath.Join(root, "internal-link"):     filepath.Join(root, "inside"),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]string{"path": root, "kind": "movie"}, 401)
	request(t, a.client, "GET", a.server.URL+"/api/directories", nil, 401)
	request(t, a.client, "POST", a.server.URL+"/api/directories/unknown/scan", map[string]string{}, 401)
	request(t, a.client, "GET", a.server.URL+"/api/media/unknown", nil, 401)
	login(t, a)
	for _, path := range []string{outside, filepath.Join(root, "escaped-directory"), filepath.Join(root, "internal-link"), root + "/../", filepath.Join(root, "inside", "real.mp4")} {
		request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]string{"path": path, "kind": "movie"}, 400)
	}
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	items := library(t, a)
	if len(items) != 1 || items[0].Filename != "real.mp4" {
		t.Fatalf("symlink scanned or real file missing: %+v", items)
	}
	request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]string{"path": root, "kind": "movie"}, 409)
	request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]string{"path": filepath.Join(root, "inside"), "kind": "movie"}, 409)
	request(t, a.client, "GET", a.server.URL+"/api/media/not-a-media-id", nil, 404)
	request(t, a.client, "GET", a.server.URL+"/api/library?kind=invalid", nil, 400)
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/library?q=real&kind=movie", nil, 200)
	var found struct {
		Items []mediaFile `json:"items"`
	}
	if err := json.Unmarshal(body, &found); err != nil || len(found.Items) != 1 {
		t.Fatalf("filename search failed: %s", body)
	}
	body, _ = request(t, a.client, "GET", a.server.URL+"/api/library?kind=series", nil, 200)
	if err := json.Unmarshal(body, &found); err != nil || len(found.Items) != 0 {
		t.Fatalf("media classification failed: %s", body)
	}
}

func TestPermissionFailureDoesNotReconcilePartialScan(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires non-root process to enforce filesystem permissions")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "nested")
	writeMedia(t, filepath.Join(root, "visible.mp4"), "first")
	writeMedia(t, filepath.Join(locked, "other.mp4"), "second")
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	original := library(t, a)
	if err := os.Chmod(locked, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0755) })
	rescan(t, a, d.ID)
	awaitScan(t, a, d.ID, "failed")
	items := library(t, a)
	if len(items) != 2 {
		t.Fatalf("permission failure cleared records: %+v", items)
	}
	for index, item := range items {
		if !item.Available || item.ID != original[index].ID {
			t.Fatal("partial scan reconciled records")
		}
	}
	if err := os.Chmod(locked, 0755); err != nil {
		t.Fatal(err)
	}
	rescan(t, a, d.ID)
	awaitScan(t, a, d.ID, "completed")
	if items = library(t, a); len(items) != 2 || items[0].ID != original[0].ID || items[1].ID != original[1].ID {
		t.Fatal("permissions recovery changed media identities")
	}
}
