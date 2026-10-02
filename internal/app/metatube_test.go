package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reelnest/internal/app"
)

const fakeMetaTubeToken = "isolated-metatube-secret"
const metaTubeRemoteID = "abc/def +?%:一"

type metaTubeFixture struct {
	mu       sync.Mutex
	mode     string
	requests []string
	server   *httptest.Server
}

func fakeMetaTube(t *testing.T) *metaTubeFixture {
	t.Helper()
	f := &metaTubeFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		mode := f.mode
		f.requests = append(f.requests, r.URL.String())
		f.mu.Unlock()
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/bridge/v1/") || strings.Contains(r.URL.String(), fakeMetaTubeToken) {
			t.Error("unexpected MetaTube method, API prefix or credential in URL")
		}
		if strings.HasPrefix(r.URL.Path, "/bridge/v1/images/") {
			if r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
				t.Error("public images must not receive token or arbitrary URL query")
			}
			if mode == "image-redirect" {
				w.Header().Set("Location", f.server.URL+"/unexpected")
				w.WriteHeader(307)
				return
			}
			if mode == "image-invalid" {
				w.Header().Set("Content-Type", "image/png")
				fmt.Fprint(w, "not an image")
				return
			}
			if mode == "image-large" {
				w.Header().Set("Content-Type", "image/png")
				w.Write(append(tinyPoster, bytes.Repeat([]byte("x"), 5<<20)...))
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write(tinyPoster)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fakeMetaTubeToken {
			t.Error("MetaTube private request lacks Bearer token")
		}
		switch mode {
		case "unauthorized", "failure", "empty", "rate":
			status := map[string]int{"unauthorized": 401, "failure": 500, "empty": 404, "rate": 429}[mode]
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"error":{"code":%d,"message":%q}}`, status, fakeMetaTubeToken)
			return
		case "timeout":
			<-r.Context().Done()
			return
		case "malformed":
			fmt.Fprint(w, fakeMetaTubeToken)
			return
		case "envelope-error":
			fmt.Fprintf(w, `{"error":{"code":500,"message":%q}}`, fakeMetaTubeToken)
			return
		case "large":
			w.Write(bytes.Repeat([]byte("x"), (1<<20)+1))
			return
		case "null":
			fmt.Fprint(w, "null")
			return
		}
		movie := func(provider string) map[string]any {
			return map[string]any{"id": metaTubeRemoteID, "provider": provider, "number": "ABC-123", "title": "示例影片", "summary": "MetaTube 隔离测试简介", "homepage": "https://example.invalid/movie/abc123", "actors": []string{"演员甲", "演员甲", "演员乙"}, "director": "导演甲", "cover_url": "http://169.254.169.254/untrusted-cover", "big_cover_url": "https://untrusted.invalid/image", "release_date": "2024-01-01T00:00:00Z"}
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/bridge/v1/movies/search" {
			if r.URL.Query().Get("fallback") != "true" || r.URL.Query().Get("provider") != "" {
				t.Error("search should use service provider priorities and database fallback")
			}
			q := r.URL.Query().Get("q")
			items := []map[string]any{}
			if q != "unknown" {
				items = append(items, movie("Example"))
			}
			if q == "ambiguous" || mode == "ambiguous" {
				items = append(items, movie("Other"), movie("EXAMPLE"))
			}
			if q == "many" {
				for i := 0; i < 21; i++ {
					m := movie("Example")
					m["id"] = fmt.Sprint(i)
					items = append(items, m)
				}
			}
			if mode == "bad-row" {
				items = append(items, map[string]any{"id": "invalid", "provider": "Example"})
			}
			json.NewEncoder(w).Encode(map[string]any{"data": items})
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.EscapedPath(), "/bridge/v1/movies/"), "/")
		if len(parts) != 2 {
			t.Errorf("opaque ID not escaped into one path segment: %s", r.URL.EscapedPath())
			w.WriteHeader(400)
			return
		}
		provider, _ := url.PathUnescape(parts[0])
		id, _ := url.PathUnescape(parts[1])
		if id != metaTubeRemoteID || r.URL.Query().Get("lazy") != "true" {
			t.Error("detail lost opaque ID or lazy parameter")
		}
		m := movie(provider)
		if mode == "mismatch" {
			m["id"] = "wrong"
		}
		if mode == "missing" {
			json.NewEncoder(w).Encode(map[string]any{})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": m})
	}))
	t.Cleanup(f.server.Close)
	return f
}
func (f *metaTubeFixture) setMode(mode string) { f.mu.Lock(); defer f.mu.Unlock(); f.mode = mode }
func (f *metaTubeFixture) count() int          { f.mu.Lock(); defer f.mu.Unlock(); return len(f.requests) }
func (f *metaTubeFixture) config(t *testing.T, root string) app.Config {
	return app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}, MetaTubeURL: f.server.URL + "/bridge/v1/", MetaTubeToken: fakeMetaTubeToken, MetaTubeTimeout: 150 * time.Millisecond}
}

func metaTubeSearch(t *testing.T, a *runningApp, id, query string) []app.TitleMetadata {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/matching/movie/"+id+"/search?q="+url.QueryEscape(query), nil, 200)
	var result struct {
		Items []app.TitleMetadata `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result.Items
}

func TestMetaTubeMatchingImagesAndRestart(t *testing.T) {
	root := t.TempDir()
	f := fakeMetaTube(t)
	config := f.config(t, root)
	path := filepath.Join(root, "ABC-123 (2024).webm")
	writeMedia(t, path, "untouched media")
	a := start(t, config)
	login(t, a)
	dir := addSourceDirectory(t, a, root, "movie", []string{"metatube"})
	file := sourceFile(t, a, dir.ID)
	m := file.Match.Metadata
	if m == nil || m.Source != "MetaTube" || m.SourceID <= 0 || m.Number != "ABC-123" || m.Year != "2024" || m.Title != "示例影片" {
		t.Fatalf("wrong metadata: %+v", m)
	}
	if m.Credits == nil || len(m.Credits.Cast) != 2 || len(m.Credits.Directors) != 1 || m.Credits.Cast[0].ID != 0 || m.Credits.Cast[0].PhotoURL != "" {
		t.Fatalf("incorrect name-only credits: %+v", m.Credits)
	}
	if f.count() != 3 {
		t.Fatalf("expected one search, one full detail and one poster; got %d", f.count())
	}
	for _, path := range []string{m.PosterURL, m.ThumbURL, m.BackdropURL} {
		if path == "" {
			t.Fatal("missing artwork URL")
		}
		body, _ := request(t, a.client, "GET", a.server.URL+path, nil, 200)
		if !bytes.Equal(body, tinyPoster) {
			t.Fatal("bad image cache")
		}
		request(t, http.DefaultClient, "GET", a.server.URL+path, nil, 401)
	}
	person := m.Credits.Cast[0]
	request(t, a.client, "GET", a.server.URL+"/api/people/metatube/"+person.Key, nil, 200)
	request(t, a.client, "GET", a.server.URL+"/api/people/metatube/"+person.Key+"/photo", nil, 404)
	request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/progress", map[string]any{"position": 14, "duration": 60, "completed": false}, 200)
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/metadata/status", nil, 200)
	if bytes.Contains(body, []byte(fakeMetaTubeToken)) || !bytes.Contains(body, []byte(`"name":"MetaTube"`)) {
		t.Fatalf("invalid source status: %s", body)
	}
	count := f.count()
	a.close()
	disabled := config
	disabled.MetaTubeURL = ""
	disabled.MetaTubeToken = ""
	a = start(t, disabled)
	login(t, a)
	awaitMetadata(t, a)
	cached := sourceFile(t, a, dir.ID)
	if cached.Match.Metadata.SourceID != m.SourceID || cached.Progress.Position != 14 {
		t.Fatal("restart changed identity or progress")
	}
	for _, path := range []string{m.PosterURL, m.ThumbURL, m.BackdropURL} {
		request(t, a.client, "GET", a.server.URL+path, nil, 200)
	}
	if f.count() != count {
		t.Fatal("disabled service contacted for cached metadata/images")
	}
	a.close()
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE metadata_cache SET expires_at=0")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	a = start(t, config)
	login(t, a)
	awaitMetadata(t, a)
	if sourceFile(t, a, dir.ID).Match.Metadata.SourceID != m.SourceID || f.count() != count+2 {
		t.Fatal("opaque identity did not survive expiry and restart")
	}
	contents, _ := os.ReadFile(path)
	if string(contents) != "untouched media" {
		t.Fatal("source media changed")
	}
	a.close()
	contents, _ = os.ReadFile(filepath.Join(config.DataDir, "reelnest.sqlite"))
	if bytes.Contains(contents, []byte(fakeMetaTubeToken)) {
		t.Fatal("credential persisted in database")
	}
}

func TestMetaTubeManualIdentityAndAtomicFailure(t *testing.T) {
	root := t.TempDir()
	f := fakeMetaTube(t)
	config := f.config(t, root)
	writeMedia(t, filepath.Join(root, "unknown.webm"), "video")
	a := start(t, config)
	login(t, a)
	dir := addSourceDirectory(t, a, root, "movie", []string{"metatube"})
	file := sourceFile(t, a, dir.ID)
	items := metaTubeSearch(t, a, file.ID, "ambiguous")
	if len(items) != 2 || items[0].SourceID == items[1].SourceID {
		t.Fatalf("provider identity collision or case-sensitive duplicate: %+v", items)
	}
	repeated := metaTubeSearch(t, a, file.ID, "ambiguous")
	if repeated[0].SourceID != items[0].SourceID || repeated[1].SourceID != items[1].SourceID {
		t.Fatal("unstable local IDs")
	}
	base := a.server.URL + "/api/matching/movie/" + file.ID + "/selection"
	choose := func(id int64, status int) {
		request(t, a.client, "POST", base, map[string]any{"source": "MetaTube", "sourceId": id, "kind": "movie"}, status)
	}
	choose(items[1].SourceID, 200)
	selected := sourceFile(t, a, dir.ID)
	if selected.Match.Selection != "manual" || !strings.EqualFold(selected.Match.Metadata.Provider, "Other") {
		t.Fatal("wrong manual provider")
	}
	for _, mode := range []string{"mismatch", "missing", "malformed", "envelope-error", "null"} {
		f.setMode(mode)
		choose(items[0].SourceID, 503)
	}
	if sourceFile(t, a, dir.ID).Match.Metadata.SourceID != items[1].SourceID {
		t.Fatal("failed detail replaced manual match")
	}
	f.setMode("")
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/matching/movie/"+file.ID+"/search?q=many", nil, 200)
	var search struct {
		Items []app.TitleMetadata `json:"items"`
		More  bool                `json:"moreResults"`
	}
	json.Unmarshal(body, &search)
	if len(search.Items) != 20 || !search.More {
		t.Fatal("search results not bounded")
	}
	q := "https://example.invalid/movie/abc?q=a+b&lang=zh"
	metaTubeSearch(t, a, file.ID, q)
	f.mu.Lock()
	last := f.requests[len(f.requests)-1]
	f.mu.Unlock()
	u, _ := url.Parse(last)
	if u.Query().Get("q") != q {
		t.Fatal("URL search was not preserved")
	}
	setSources(t, a, dir.ID, []string{"tmdb", "tvdb", "metatube"})
	if sourceFile(t, a, dir.ID).Match.Metadata.SourceID != items[1].SourceID {
		t.Fatal("reordering overwrote manual choice")
	}
}

func TestMetaTubeFallbackAndSafeErrors(t *testing.T) {
	for _, mode := range []string{"unauthorized", "failure", "rate", "empty", "timeout", "malformed", "envelope-error", "large", "null"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			f := fakeMetaTube(t)
			f.setMode(mode)
			tmdb := fakeTMDB(t)
			config := tmdb.config(t, root)
			config.MetaTubeURL = f.server.URL + "/bridge"
			config.MetaTubeToken = fakeMetaTubeToken
			config.MetaTubeTimeout = 50 * time.Millisecond
			writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "video")
			a := start(t, config)
			login(t, a)
			dir := addSourceDirectory(t, a, root, "movie", []string{"metatube", "tmdb"})
			file := sourceFile(t, a, dir.ID)
			if file.Match.Metadata == nil || file.Match.Metadata.Source != "TMDB" || strings.Contains(file.Match.Error, fakeMetaTubeToken) {
				t.Fatalf("fallback failed or leaked credential: %+v", file.Match)
			}
			status, _ := request(t, a.client, "GET", a.server.URL+"/api/metadata/status", nil, 200)
			if bytes.Contains(status, []byte(fakeMetaTubeToken)) {
				t.Fatal("token leaked in status")
			}
		})
	}
}

func TestMetaTubeEmptyAndUnsupportedSeries(t *testing.T) {
	root := t.TempDir()
	f := fakeMetaTube(t)
	config := f.config(t, root)
	writeMedia(t, filepath.Join(root, "movie", "ABC-123.webm"), "video")
	writeMedia(t, filepath.Join(root, "series", "Show.S01E01.webm"), "video")
	a := start(t, config)
	login(t, a)
	f.setMode("empty")
	dir := addSourceDirectory(t, a, filepath.Join(root, "movie"), "movie", []string{"metatube"})
	file := sourceFile(t, a, dir.ID)
	if file.Match.Status != "unmatched" || len(metaTubeSearch(t, a, file.ID, "nothing")) != 0 {
		t.Fatal("404 search must be empty, not an upstream failure")
	}
	f.setMode("")
	count := f.count()
	series := addSourceDirectory(t, a, filepath.Join(root, "series"), "series", []string{"metatube"})
	if sourceFile(t, a, series.ID).Matched || f.count() != count+3 { // queued movie retry may now match, but series must not issue any request
		t.Fatal("unexpected series match or request count")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, endpoint := range f.requests {
		if strings.Contains(endpoint, "Show") {
			t.Fatal("series sent to movie search")
		}
	}
}

func TestMetaTubeBadArtworkDoesNotLoseMetadata(t *testing.T) {
	for _, mode := range []string{"image-redirect", "image-invalid", "image-large"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			f := fakeMetaTube(t)
			f.setMode(mode)
			config := f.config(t, root)
			writeMedia(t, filepath.Join(root, "ABC-123.webm"), "video")
			a := start(t, config)
			login(t, a)
			dir := addSourceDirectory(t, a, root, "movie", []string{"metatube"})
			file := sourceFile(t, a, dir.ID)
			if file.Match.Metadata == nil || file.Match.Metadata.PosterURL != "" || file.Match.Error == "" {
				t.Fatal("image failure discarded metadata or was not reported")
			}
			endpoint := a.server.URL + file.Match.Metadata.ThumbURL
			request(t, a.client, "GET", endpoint, nil, 404)
			count := f.count()
			request(t, a.client, "GET", endpoint, nil, 404)
			if f.count() != count {
				t.Fatal("failed image was not backed off")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, endpoint := range f.requests {
				if endpoint == "/unexpected" {
					t.Fatal("followed redirect")
				}
			}
		})
	}
}

func TestMetaTubeConfigAndNoAuthentication(t *testing.T) {
	for _, base := range []string{"ftp://localhost", "http://user:secret@example.test", "https://example.test/?token=secret", "https://example.test/#secret", "http://"} {
		config := app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MetaTubeURL: base}
		application, err := app.New(config)
		if err == nil {
			application.Close()
			t.Fatal("accepted invalid service URL")
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatal("URL credentials leaked in validation error")
		}
	}
	for _, base := range []string{"http://192.168.1.5:8080", "http://metatube:8080", "https://meta.example.test/prefix"} {
		a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MetaTubeURL: base})
		login(t, a)
		awaitMetadata(t, a)
		a.close()
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("token sent to unauthenticated deployment")
		}
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer upstream.Close()
	root := t.TempDir()
	writeMedia(t, filepath.Join(root, "Unknown.webm"), "video")
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}, MetaTubeURL: upstream.URL})
	login(t, a)
	dir := addSourceDirectory(t, a, root, "movie", []string{"metatube"})
	if sourceFile(t, a, dir.ID).Matched {
		t.Fatal("empty search matched")
	}
}

func TestMetaTubeAmbiguityAndMalformedCandidatesAreNotAutoMatched(t *testing.T) {
	for _, mode := range []string{"ambiguous", "bad-row"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			f := fakeMetaTube(t)
			f.setMode(mode)
			writeMedia(t, filepath.Join(root, "ABC-123.webm"), "video")
			a := start(t, f.config(t, root))
			login(t, a)
			dir := addSourceDirectory(t, a, root, "movie", []string{"metatube"})
			if sourceFile(t, a, dir.ID).Matched || f.count() != 1 {
				t.Fatal("ambiguous/incomplete search automatically selected a movie")
			}
		})
	}
}
