package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reelnest/internal/app"
	"reelnest/internal/store"
)

func TestNewRuntimePersistsAndOwnsDataDirectoryLifecycle(t *testing.T) {
	config := app.RuntimeConfig{DataDir: t.TempDir(), MediaRoots: []string{t.TempDir()}, Username: "family", Password: "a-long-test-password"}
	root := config.MediaRoots[0]
	if e := os.WriteFile(filepath.Join(root, "movie.mp4"), []byte("temporary media"), 0600); e != nil {
		t.Fatal(e)
	}
	runtime, e := app.NewRuntime(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(runtime.Handler())
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	call := func(method, path, body string, status int) string {
		t.Helper()
		r, _ := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		resp, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, b)
		}
		return string(b)
	}
	call("POST", "/api/login", `{"username":"family","password":"a-long-test-password"}`, 200)
	call("PATCH", "/api/settings/metadata/tmdb", `{"revision":1,"credential":"isolated-runtime-token"}`, 200)
	call("PATCH", "/api/settings/metadata/tvdb", `{"revision":1,"credential":"isolated-runtime-key","pin":"isolated-runtime-pin"}`, 200)
	call("PATCH", "/api/settings/metadata/metatube", `{"revision":1,"url":"http://127.0.0.1:1/proxy"}`, 200)
	settings := call("GET", "/api/settings/metadata", "", 200)
	if strings.Contains(settings, "isolated-runtime-") {
		t.Fatal("configuration response exposes secrets")
	}
	if other, e := app.NewRuntime(context.Background(), config); !errors.Is(e, store.ErrDataDirLocked) {
		if other != nil {
			other.Close()
		}
		t.Fatal("second runtime opened same data directory", e)
	}
	var library struct{ ID, LatestScanID string }
	if e = json.Unmarshal([]byte(call("POST", "/api/libraries", fmt.Sprintf(`{"name":"重启验证","path":%q,"kind":"movie","sources":[]}`, root), 201)), &library); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var scan struct{ State string }
		if e = json.Unmarshal([]byte(call("GET", "/api/scans/"+library.LatestScanID, "", 200)), &scan); e != nil {
			t.Fatal(e)
		}
		if scan.State == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scan timeout", scan)
		}
		time.Sleep(time.Millisecond)
	}
	var page struct{ Items []struct{ ID string } }
	if e = json.Unmarshal([]byte(call("GET", "/api/catalog?libraryId="+library.ID, "", 200)), &page); e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	mediaBase := "/api/media/" + page.Items[0].ID
	call("GET", mediaBase+"/playback", "", 200)
	call("PUT", mediaBase+"/progress", `{"position":12.5,"duration":60,"completed":false}`, 200)
	savedProgress := call("GET", mediaBase+"/progress", "", 200)
	stream := call("POST", mediaBase+"/stream-link", "{}", 200)
	call("GET", "/readyz", "", 200)
	server.Close()
	if e = runtime.Close(); e != nil {
		t.Fatal(e)
	}
	if e = runtime.Close(); e != nil {
		t.Fatal(e)
	}
	// No credentials are required again, and deployment input cannot reset them.
	config.Username = "replacement"
	config.Password = ""
	runtime, e = app.NewRuntime(context.Background(), config)
	if e != nil {
		t.Fatal(e)
	}
	defer runtime.Close()
	server = httptest.NewServer(runtime.Handler())
	defer server.Close()
	if body := call("GET", "/api/session", "", 200); !strings.Contains(body, "family") {
		t.Fatal(body)
	}
	call("GET", "/api/libraries", "", 200)
	if got := call("GET", mediaBase+"/progress", "", 200); got != savedProgress {
		t.Fatal("restart reset progress", got)
	}
	if got := call("POST", mediaBase+"/stream-link", "{}", 200); got != stream {
		t.Fatal("restart rotated credential", got)
	}
	var link struct{ Path string }
	if e = json.Unmarshal([]byte(stream), &link); e != nil {
		t.Fatal(e)
	}
	// A sessionless client can still read only the originally issued file.
	resp, e := http.Get(server.URL + link.Path)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || resp.StatusCode != 200 || string(b) != "temporary media" {
		t.Fatal(string(b), resp.StatusCode, e)
	}

	if got := call("GET", "/api/settings/metadata", "", 200); got != settings {
		t.Fatal("restart changed provider settings")
	}
}

func TestNewRuntimeRejectsMissingSettingsKeyWithoutRegeneratingIt(t *testing.T) {
	config := app.RuntimeConfig{DataDir: t.TempDir(), MediaRoots: []string{t.TempDir()}, Username: "family", Password: "a-long-test-password"}
	runtime, err := app.NewRuntime(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	if err = runtime.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config.DataDir, "metadata-settings.key")
	key, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if next, err := app.NewRuntime(context.Background(), config); !errors.Is(err, store.ErrInvalidKey) {
		if next != nil {
			next.Close()
		}
		t.Fatal("missing key accepted", err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing key was regenerated", err)
	}
	if err = os.WriteFile(path, key, 0600); err != nil {
		t.Fatal(err)
	}
	runtime, err = app.NewRuntime(context.Background(), config)
	if err != nil {
		t.Fatal("complete key restore failed", err)
	}
	runtime.Close()
}
