package http_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRetiredContractsAreNotAliased(t *testing.T) {
	h := start(t)
	h.login(t)
	for _, path := range []string{"/api/directories", "/api/library", "/api/metadata", "/api/posters/1", "/api/tmdb/search", "/api/media/1/stream", "/api/media/1/delete-preview"} {
		response, err := h.client.Get(h.server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusNotFound || strings.Contains(string(body), "<html") {
			t.Fatalf("old contract aliased: %s %d %s", path, response.StatusCode, body)
		}
	}
	for _, body := range []string{`{"username":"family","password":"a-long-test-password","tmdbId":1}`, `{"username":"family","password":"a-long-test-password"}{}`, strings.Repeat("x", 16385)} {
		h.request(t, "POST", "/api/login", body, 400)
	}
	h.request(t, "GET", "/api/catalog?tmdbId=1", "", 400)
}
