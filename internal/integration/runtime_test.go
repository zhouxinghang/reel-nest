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
}
