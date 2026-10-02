package integration_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
