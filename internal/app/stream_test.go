package app_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reelnest/internal/app"
)

func streamPath(t *testing.T, a *runningApp, id string) string {
	t.Helper()
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/media/"+id+"/stream-link", map[string]any{}, 200)
	var result struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result.Path, "/stream/"+id+"?token=") {
		t.Fatal("unexpected stream path")
	}
	return result.Path
}
func TestReadOnlyStreamsPersistWithoutSessionsAndCannotChangeTarget(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "中文 space ? & #.webm")
	writeMedia(t, path, "0123456789 original bytes")
	writeMedia(t, filepath.Join(root, "second.mp4"), "different video")
	a := start(t, app.Config{DataDir: data, Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	d := addDirectory(t, a, root)
	awaitScan(t, a, d.ID, "completed")
	files := library(t, a)
	var id, other string
	for _, f := range files {
		if f.Path == path {
			id = f.ID
		} else {
			other = f.ID
		}
	}
	saveProgress(t, a, id, 15, false)
	before := progress(t, a, id)
	link := streamPath(t, a, id)
	u, _ := url.Parse(link)
	token := u.Query().Get("token")
	if len(token) != 64 || strings.Contains(link, "中文") {
		t.Fatal("credential or path unsafe")
	}
	request(t, http.DefaultClient, "POST", a.server.URL+"/api/media/"+id+"/stream-link", map[string]any{}, 401)
	body, _ := readContent(t, http.DefaultClient, "GET", a.server.URL+link, "bytes=2-5", 206)
	if string(body) != "2345" {
		t.Fatal("wrong stream bytes")
	}
	body, headers := readContent(t, http.DefaultClient, "HEAD", a.server.URL+link, "", 200)
	if len(body) != 0 || headers.Get("Content-Length") != "25" {
		t.Fatal("stream HEAD invalid")
	}
	for _, bad := range []string{"/stream/" + id, "/stream/" + id + "?token=broken", strings.Replace(link, token, strings.Repeat("0", 64), 1), strings.Replace(link, id, other, 1), link + "&token=" + token} {
		readContent(t, http.DefaultClient, "GET", a.server.URL+bad, "", 401)
	}
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/session", nil}, {"GET", "/api/library", nil}, {"GET", "/api/directories", nil}, {"GET", "/api/media/" + id, nil}, {"GET", "/api/media/" + id + "/progress", nil},
		{"POST", "/api/directories", map[string]any{"path": root, "kind": "movie"}}, {"POST", "/api/directories/" + d.ID + "/scan", map[string]any{}},
		{"POST", "/api/media/" + id + "/progress", map[string]any{"position": 30, "duration": 60, "completed": false}},
		{"POST", "/api/media/" + id + "/stream-link", map[string]any{}}, {"DELETE", "/api/media/" + id, nil},
	} {
		request(t, http.DefaultClient, tc.method, a.server.URL+tc.path+"?token="+token, tc.body, 401)
	}
	if after := progress(t, a, id); after != before {
		t.Fatal("external playback wrote watch progress")
	}
	request(t, a.client, "POST", a.server.URL+"/api/logout", map[string]any{}, 200)
	readContent(t, http.DefaultClient, "GET", a.server.URL+link, "", 200)
	a.close()
	b := start(t, app.Config{DataDir: data, MediaRoots: []string{root}})
	readContent(t, http.DefaultClient, "GET", b.server.URL+link, "bytes=-5", 206)
	login(t, b)
	if next := streamPath(t, b, id); next != link {
		t.Fatal("restart rotated stream credential")
	}
	if after := progress(t, b, id); after != before {
		t.Fatal("stream/restart changed progress")
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	writeMedia(t, path, "replacement must not be exposed")
	body, _ = readContent(t, http.DefaultClient, "GET", b.server.URL+link, "", 409)
	if bytes.Contains(body, []byte("must not be exposed")) {
		t.Fatal("old link exposed replacement")
	}
	rescan(t, b, d.ID)
	awaitScan(t, b, d.ID, "completed")
	readContent(t, http.DefaultClient, "GET", b.server.URL+link, "", 404)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	readContent(t, http.DefaultClient, "GET", b.server.URL+link, "", 404)
}
