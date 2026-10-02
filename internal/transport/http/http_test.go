package http_test

import (
	"bytes"
	"context"
	"encoding/json"
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

	"reelnest/api"
	"reelnest/internal/catalog"
	"reelnest/internal/identity"
	"reelnest/internal/mediafs"
	"reelnest/internal/model"
	"reelnest/internal/store"
	transport "reelnest/internal/transport/http"
)

type harness struct {
	server   *httptest.Server
	client   *http.Client
	store    *store.Store
	catalog  *catalog.Catalog
	fs       *mediafs.FS
	root     string
	contract *api.Contract
}

func start(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	data, root := t.TempDir(), t.TempDir()
	s, e := store.Open(ctx, data)
	if e != nil {
		t.Fatal(e)
	}
	fs, e := mediafs.New([]string{root}, data)
	if e != nil {
		t.Fatal(e)
	}
	i, e := identity.New(ctx, s, "family", "a-long-test-password")
	if e != nil {
		t.Fatal(e)
	}
	c, e := catalog.New(ctx, s, fs)
	if e != nil {
		t.Fatal(e)
	}
	handler, e := transport.New(s, i, c, []string{root}, false)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(handler)
	jar, _ := cookiejar.New(nil)
	contract, e := api.Load()
	if e != nil {
		t.Fatal(e)
	}
	h := &harness{server, &http.Client{Jar: jar, Timeout: 15 * time.Second}, s, c, fs, root, contract}
	t.Cleanup(func() {
		server.Close()
		if e := c.Close(); e != nil {
			t.Error(e)
		}
		if e := s.Close(); e != nil {
			t.Error(e)
		}
	})
	return h
}
func (h *harness) request(t *testing.T, method, path, body string, status int, headers ...string) []byte {
	t.Helper()
	r, e := http.NewRequest(method, h.server.URL+path, strings.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for n := 0; n < len(headers); n += 2 {
		r.Header.Set(headers[n], headers[n+1])
	}
	resp, e := h.client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, path, resp.StatusCode, status, b)
	}
	if path == "/api/login" && status == 200 {
		cookies := resp.Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" || cookies[0].MaxAge != int(identity.Lifetime.Seconds()) {
			t.Fatalf("unsafe session cookie: %+v", cookies)
		}
	}
	// Validate every actual response, including unsuccessful writes and scan states.
	if e = h.contract.ValidateResponse(context.Background(), r, resp.StatusCode, resp.Header, io.NopCloser(bytes.NewReader(b))); e != nil {
		t.Fatalf("contract %s: %v %s", path, e, b)
	}
	return b
}
func (h *harness) login(t *testing.T) {
	h.request(t, "POST", "/api/login", `{"username":"family","password":"a-long-test-password"}`, 200)
}
func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(contents), 0600); e != nil {
		t.Fatal(e)
	}
}
func (h *harness) create(t *testing.T, path, kind string) (string, string) {
	t.Helper()
	input, _ := json.Marshal(map[string]any{"name": " 家庭媒体 ", "path": path, "kind": kind, "sources": []string{}})
	b := h.request(t, "POST", "/api/libraries", string(input), 201)
	var l struct {
		ID, LatestScanID, Name string
		Revision               int64
		Sources                []string
	}
	if e := json.Unmarshal(b, &l); e != nil {
		t.Fatal(e)
	}
	if l.Name != "家庭媒体" || l.Revision != 1 || len(l.Sources) != 0 {
		t.Fatal(string(b))
	}
	return l.ID, l.LatestScanID
}
func (h *harness) await(t *testing.T, scan, state string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		b := h.request(t, "GET", "/api/scans/"+scan, "", 200)
		var s struct{ State string }
		json.Unmarshal(b, &s)
		if s.State == state {
			return
		}
		if s.State != "queued" && s.State != "running" {
			t.Fatalf("unexpected terminal state %s", b)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("scan timeout")
}
func TestLoginCreateScanOrganizeAndOpenWithoutProviders(t *testing.T) {
	h := start(t)
	h.request(t, "GET", "/healthz", "", 200)
	h.request(t, "GET", "/readyz", "", 200)
	h.request(t, "GET", "/api/libraries", "", 401)
	h.request(t, "POST", "/api/login", `{"username":"family","password":"a-long-test-password"}`, 403, "Origin", "https://other.invalid")
	h.login(t)
	h.request(t, "GET", "/api/session", "", 200)
	for _, path := range []string{"Show/Season 2/01.mp4", "Show.S02E01.4K.mp4", "Show.S02E10.mp4", "Unknown.mp4"} {
		writeFile(t, filepath.Join(h.root, "tv", path), "isolated video")
	}
	library, scan := h.create(t, filepath.Join(h.root, "tv"), "series")
	h.await(t, scan, "completed")
	files, e := h.store.MediaFiles(context.Background(), library, "", 100)
	if e != nil || len(files) != 4 {
		t.Fatal(files, e)
	}
	series := ""
	for _, f := range files {
		if f.SubjectID == "" {
			t.Fatal("missing metadata target")
		}
		if f.Relative != "Unknown.mp4" {
			if f.SeriesID == "" || f.Season != 2 {
				t.Fatal("organization failed", f)
			}
			if series != "" && f.SeriesID != series {
				t.Fatal("series split")
			}
			series = f.SeriesID
		}
		video, e := h.catalog.OpenVideo(context.Background(), f.ID)
		if e != nil {
			t.Fatal(e)
		}
		b, e := io.ReadAll(video)
		video.Close()
		if e != nil || string(b) != "isolated video" {
			t.Fatal(string(b), e)
		}
	}
	h.request(t, "PATCH", "/api/libraries/"+library, `{"revision":1,"name":"新名称","sources":["tvdb","tmdb"]}`, 200)
	h.request(t, "PATCH", "/api/libraries/"+library, `{"revision":1,"name":"旧页面"}`, 409)
	// Stream-shaped credentials confer no management, scan, progress or session access.
	saved := h.client
	h.client = &http.Client{Timeout: time.Second}
	h.request(t, "POST", "/api/libraries/"+library+"/scans", "{}", 401, "Cookie", identity.CookieName+"="+strings.Repeat("a", 64))
	h.client = saved
	h.request(t, "POST", "/api/logout", "{}", 200)
	h.request(t, "GET", "/api/session", "", 401)
}
func TestLibraryValidationPaginationAndRootFailure(t *testing.T) {
	h := start(t)
	h.login(t)
	h.request(t, "POST", "/api/libraries", `{"name":"a","path":"/tmp","kind":"movie","unexpected":1}`, 400)
	for _, name := range []string{"   ", "a\nb"} {
		input, _ := json.Marshal(map[string]any{"name": name, "path": h.root, "kind": "movie"})
		h.request(t, "POST", "/api/libraries", string(input), 400)
	}
	h.request(t, "POST", "/api/libraries", fmt.Sprintf(`{"name":"a","path":%q,"kind":"movie"}`, t.TempDir()), 400)
	ids := []string{}
	for n := 0; n < 3; n++ {
		path := filepath.Join(h.root, fmt.Sprint(n))
		if e := os.Mkdir(path, 0700); e != nil {
			t.Fatal(e)
		}
		id, scan := h.create(t, path, "movie")
		h.await(t, scan, "completed")
		ids = append(ids, id)
	}
	input, _ := json.Marshal(map[string]any{"name": "same", "path": h.root, "kind": "movie"})
	h.request(t, "POST", "/api/libraries", string(input), 409)
	var page struct {
		Items []struct{ ID string }
		Page  struct{ NextCursor *string }
	}
	b := h.request(t, "GET", "/api/libraries?limit=1", "", 200)
	json.Unmarshal(b, &page)
	if len(page.Items) != 1 || page.Page.NextCursor == nil {
		t.Fatal(string(b))
	}
	cursor := *page.Page.NextCursor
	b = h.request(t, "GET", "/api/libraries?limit=1&cursor="+cursor, "", 200)
	var second struct{ Items []struct{ ID string } }
	json.Unmarshal(b, &second)
	if len(second.Items) != 1 || second.Items[0].ID == page.Items[0].ID {
		t.Fatal("duplicate page")
	}
	h.request(t, "GET", "/api/libraries?limit=2&cursor="+cursor, "", 409)
	h.request(t, "GET", "/api/libraries?limit=1&cursor="+cursor+"x", "", 409)
	h.request(t, "PATCH", "/api/libraries/"+ids[0], `{"revision":1,"name":"新名称"}`, 200)
	h.request(t, "GET", "/api/libraries?limit=1&cursor="+cursor, "", 409)
	// A replaced root must fail, with a contract-valid terminal state and no publish.
	path := filepath.Join(h.root, "0")
	if e := os.Rename(path, path+"-old"); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(path, 0700); e != nil {
		t.Fatal(e)
	}
	b = h.request(t, "POST", "/api/libraries/"+ids[0]+"/scans", "{}", 202)
	var scan model.Scan
	json.Unmarshal(b, &scan)
	h.await(t, scan.ID, "failed")
	h.request(t, "POST", "/api/libraries/"+ids[0]+"/scans", "{}", 403, "Sec-Fetch-Site", "cross-site")
	h.request(t, "POST", "/api/login", `{"username":"family","password":"a-long-test-password"}{}`, 400)
}
