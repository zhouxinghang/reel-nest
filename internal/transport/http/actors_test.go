package http_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestActorHTTPNameSearchDetailsImagesAndConfigurationIsolation(t *testing.T) {
	var searches, details, images atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/actors/search"):
			searches.Add(1)
			if r.URL.Query().Get("q") != "演员 & 别名" || r.Header.Get("Authorization") != "Bearer fixture-token" {
				t.Error(r.URL, "wrong auth/query")
			}
			fmt.Fprint(w, `{"data":[{"id":"Raw/中文","provider":"Example","name":"演员","aliases":["别名"]}]}`)
		case strings.Contains(r.URL.Path, "/actors/example/"):
			details.Add(1)
			fmt.Fprint(w, `{"data":{"id":"Raw/中文","provider":"EXAMPLE","name":"演员","summary":"简介","images":["https://not-fetched.invalid/a.png","https://not-fetched.invalid/b.png"]}}`)
		case strings.Contains(r.URL.Path, "/images/primary/example/"):
			images.Add(1)
			if r.Header.Get("Authorization") != "" {
				t.Error("image leaked bearer")
			}
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "\x89PNG\r\n\x1a\nfixture")
		default:
			t.Error("unexpected endpoint", r.URL)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	h := start(t)
	search := "/api/metadata/actors/search?q=" + url.QueryEscape("演员 & 别名")
	h.request(t, "GET", search, "", 401)
	h.request(t, "GET", "/api/metadata/actor-images/a.b", "", 401)
	h.login(t)
	h.request(t, "GET", search, "", 503)
	h.request(t, "PATCH", "/api/settings/metadata/metatube", fmt.Sprintf(`{"revision":1,"url":%q,"credential":"fixture-token"}`, upstream.URL+"/proxy"), 200)
	var page struct {
		Items []struct{ ID, Name, PhotoID string }
	}
	b := h.request(t, "GET", search, "", 200)
	if err := json.Unmarshal(b, &page); err != nil || len(page.Items) != 1 || page.Items[0].Name != "演员" {
		t.Fatal(string(b), err)
	}
	id := page.Items[0].ID
	var actor struct {
		ID, Name, Summary string
		Wallpapers        []string
	}
	b = h.request(t, "GET", "/api/metadata/actors/"+id, "", 200)
	if err := json.Unmarshal(b, &actor); err != nil || actor.Summary != "简介" || len(actor.Wallpapers) != 2 {
		t.Fatal(string(b), err)
	}
	for _, image := range append(actor.Wallpapers, page.Items[0].PhotoID) {
		h.request(t, "GET", "/api/metadata/actor-images/"+image, "", 200)
	}
	h.request(t, "HEAD", "/api/metadata/actor-images/"+page.Items[0].PhotoID, "", 200)
	// Identity/image token substitution and client-supplied image URLs are refused.
	h.request(t, "GET", "/api/metadata/actor-images/"+id, "", 400)
	h.request(t, "GET", "/api/metadata/actors/"+page.Items[0].PhotoID, "", 400)
	h.request(t, "GET", "/api/metadata/actors/x"+id, "", 400)
	h.request(t, "GET", "/api/metadata/actor-images/"+page.Items[0].PhotoID+"?url=http://evil.invalid", "", 400)
	for _, q := range []string{"", "?q=", "?q=%20", "?q=a&q=b", "?q=%00", "?q=" + strings.Repeat("a", 257)} {
		h.request(t, "GET", "/api/metadata/actors/search"+q, "", 400)
	}
	// A rotated credential invalidates outstanding refs even in the same catalog.
	h.request(t, "PATCH", "/api/settings/metadata/metatube", `{"revision":2,"credential":"new-token"}`, 200)
	h.request(t, "GET", "/api/metadata/actors/"+id, "", 409)
	h.request(t, "GET", "/api/metadata/actor-images/"+actor.Wallpapers[0], "", 409)
	h.request(t, "PATCH", "/api/settings/metadata/metatube", `{"revision":3,"url":"http://new.invalid","clearCredential":true}`, 200)
	h.request(t, "GET", "/api/metadata/actors/"+id, "", 409)
	if searches.Load() != 1 || details.Load() != 1 || images.Load() != 4 {
		t.Fatal("invalid/stale queries reached provider", searches.Load(), details.Load(), images.Load())
	}
	// Standalone search needs no library, and never creates a local person/title.
	h.request(t, "GET", "/api/people/"+url.PathEscape("unknown"), "", 404)
}
