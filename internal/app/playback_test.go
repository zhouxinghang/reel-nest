package app_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"reelnest/internal/app"
)

func readContent(t *testing.T, client *http.Client, method, url, byteRange string, status int) ([]byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s range %s: status %d want %d: %s", method, byteRange, response.StatusCode, status, body)
	}
	return body, response.Header
}
func progress(t *testing.T, a *runningApp, id string) app.WatchProgress {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/progress", nil, 200)
	var p app.WatchProgress
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func saveProgress(t *testing.T, a *runningApp, id string, position float64, completed bool) {
	t.Helper()
	request(t, a.client, "POST", a.server.URL+"/api/media/"+id+"/progress", map[string]any{"position": position, "duration": 60, "completed": completed}, 200)
}
func TestPlaybackHTTPRangesAndSharedPersistentProgress(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	content := "0123456789中文 & video bytes"
	writeMedia(t, filepath.Join(root, "one", "中文 ? &#.mp4"), content)
	writeMedia(t, filepath.Join(root, "two", "中文 ? &#.mp4"), "other version")
	a := start(t, app.Config{DataDir: data, Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	files := library(t, a)
	id := files[0].ID
	u := a.server.URL + "/api/media/" + id + "/content"
	readContent(t, http.DefaultClient, "GET", u, "", 401)
	body, headers := readContent(t, a.client, "HEAD", u, "", 200)
	if len(body) != 0 || headers.Get("Content-Length") == "" || headers.Get("Content-Type") != "video/mp4" {
		t.Fatal("invalid HEAD", headers)
	}
	body, _ = readContent(t, a.client, "GET", u, "", 200)
	if string(body) != content {
		t.Fatalf("wrong original bytes: %s", body)
	}
	for _, tc := range []struct {
		r, want string
		status  int
	}{
		{"bytes=2-5", "2345", 206}, {"bytes=8-", content[8:], 206}, {"bytes=-5", "bytes", 206}, {"bytes=0-999", content, 206},
		{"bytes=999-", "", 416}, {"bytes=5-2", "", 416}, {"bytes=wat", "", 416},
	} {
		body, headers = readContent(t, a.client, "GET", u, tc.r, tc.status)
		if tc.status == 206 && (string(body) != tc.want || headers.Get("Content-Range") == "") {
			t.Fatalf("bad range %s: %s %v", tc.r, body, headers)
		}
	}
	body, headers = readContent(t, a.client, "HEAD", u, "bytes=2-5", 206)
	if len(body) != 0 || headers.Get("Content-Length") != "4" {
		t.Fatal("bad range HEAD")
	}
	body, _ = readContent(t, a.client, "GET", u, "bytes=0-1,8-9", 206)
	if !bytes.Contains(body, []byte("Content-Range: bytes 8-9/")) {
		t.Fatal("multipart range failed")
	}
	readContent(t, a.client, "GET", a.server.URL+"/api/media/unknown/content", "", 404)
	request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/playback", nil, 200)
	if p := progress(t, a, id); p.UpdatedAt != 0 {
		t.Fatal("reads created progress")
	}
	saveProgress(t, a, id, 12.5, false)
	jar, _ := cookiejar.New(nil)
	other := &runningApp{client: &http.Client{Jar: jar}, server: a.server}
	login(t, other)
	if p := progress(t, other, id); p.Position != 12.5 || p.Completed {
		t.Fatal("progress not shared", p)
	}
	saveProgress(t, other, id, 18, false)
	if p := progress(t, a, id); p.Position != 18 {
		t.Fatal("last update not shared", p)
	}
	if p := progress(t, a, files[1].ID); p.UpdatedAt != 0 {
		t.Fatal("file versions share progress")
	}
	for _, body := range []any{map[string]any{"position": -1, "duration": 60, "completed": false}, map[string]any{"position": 61, "duration": 60, "completed": false}, map[string]any{"position": 3, "duration": 60, "completed": true}, map[string]any{"position": 0, "duration": 0, "completed": false}, map[string]any{"position": 1, "duration": 60}, map[string]any{"position": nil, "duration": 60, "completed": false}} {
		request(t, a.client, "POST", a.server.URL+"/api/media/"+id+"/progress", body, 400)
	}
	request(t, http.DefaultClient, "POST", a.server.URL+"/api/media/"+id+"/progress", map[string]any{"position": 10, "duration": 60, "completed": false}, 401)
	rescan(t, a, d.ID)
	awaitScan(t, a, d.ID, "completed")
	if p := progress(t, a, id); p.Position != 18 {
		t.Fatal("rescan reset progress")
	}
	body, _ = request(t, a.client, "GET", a.server.URL+"/api/library", nil, 200)
	if !bytes.Contains(body, []byte(`"position":18`)) {
		t.Fatal("continue watching absent from library")
	}
	a.close()
	b := start(t, app.Config{DataDir: data, MediaRoots: []string{root}})
	login(t, b)
	if p := progress(t, b, id); p.Position != 18 {
		t.Fatal("restart reset progress")
	}
	saveProgress(t, b, id, 60, true)
	if p := progress(t, b, id); !p.Completed {
		t.Fatal("ended state not saved")
	}
	saveProgress(t, b, id, 0, false)
	if p := progress(t, b, id); p.Completed || p.Position != 0 {
		t.Fatal("replay cannot restart")
	}
}

func TestPlaybackRejectsChangedMissingAndUnsafeFiles(t *testing.T) {
	for _, scenario := range []string{"missing", "replacement", "in-place change", "symlink outside", "symlink inside", "parent symlink", "directory", "fifo", "permissions", "root replaced", "root missing"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "nested", "original.mp4")
			writeMedia(t, path, "original video")
			a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
			login(t, a)
			d := addDirectory(t, a, root)
			awaitScan(t, a, d.ID, "completed")
			id := library(t, a)[0].ID
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "missing":
				must(os.Remove(path))
			case "replacement":
				must(os.Rename(path, path+".old"))
				writeMedia(t, path, "replacement")
			case "in-place change":
				writeMedia(t, path, "edited video")
			case "symlink outside", "symlink inside":
				target := filepath.Join(root, "another.mp4")
				if strings.HasSuffix(scenario, "outside") {
					target = filepath.Join(t.TempDir(), "secret.mp4")
				}
				writeMedia(t, target, "do not expose")
				must(os.Remove(path))
				must(os.Symlink(target, path))
			case "parent symlink":
				must(os.Rename(filepath.Dir(path), filepath.Join(root, "moved")))
				must(os.Symlink(filepath.Join(root, "moved"), filepath.Dir(path)))
			case "directory":
				must(os.Remove(path))
				must(os.Mkdir(path, 0755))
			case "fifo":
				must(os.Remove(path))
				must(syscall.Mkfifo(path, 0600))
			case "permissions":
				if os.Geteuid() == 0 {
					t.Skip("requires non-root permissions")
				}
				must(os.Chmod(path, 0))
				t.Cleanup(func() { _ = os.Chmod(path, 0644) })
			case "root replaced":
				must(os.Rename(root, root+"-original"))
				t.Cleanup(func() { _ = os.RemoveAll(root + "-original") })
				writeMedia(t, path, "new root video")
			case "root missing":
				must(os.Rename(root, root+"-original"))
				t.Cleanup(func() { _ = os.RemoveAll(root + "-original") })
			}
			status := 409
			if scenario == "missing" {
				status = 410
			}
			readContent(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/content", "", status)
			request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/playback", nil, status)
		})
	}
}
