package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestFrontendRuntimeBoundary(t *testing.T) {
	handler := Handler(fstest.MapFS{"index.html": {Data: []byte("new-ui")}, "assets/main.js": {Data: []byte("script")}}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "api-only", 404) }))
	for _, path := range []string{"/media/file", "/library?category=movie", "/settings/metadata"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.String() != "new-ui" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "media-src 'self' blob:") {
			t.Fatal(path, w)
		}
	}
	for _, path := range []string{"/api", "/api/old-route", "/stream/old", "/assets/missing.js"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 || strings.Contains(w.Body.String(), "new-ui") {
			t.Fatal(path, w)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("POST", "/library", nil))
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("HEAD", "/media/file", nil))
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatal(w)
	}
}
