package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reelnest/internal/model"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type config struct{ source, base string }

func (c config) Source() string     { return c.source }
func (c config) CatalogID() string  { return c.source }
func (c config) BaseURL() string    { return c.base }
func (c config) Credential() string { return "synthetic-secret" }
func (c config) PIN() string        { return "synthetic-pin" }
func (c config) Revision() int64    { return 2 }
func (c config) Configured() bool   { return true }

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, source string, handler http.HandlerFunc) *client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		next := r.Clone(r.Context())
		u2 := *r.URL
		next.URL = &u2
		next.Header.Set("X-Upstream", r.URL.Host)
		next.URL.Scheme = u.Scheme
		next.URL.Host = u.Host
		return http.DefaultTransport.RoundTrip(next)
	})
	base := map[string]string{"tmdb": "https://api.themoviedb.org/3", "tvdb": "https://api4.thetvdb.com/v4", "metatube": "http://configured.invalid/prefix/v1"}[source]
	return New(config{source, base}, transport, nil, nil).(*client)
}
func jsonReply(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, s)
}
func TestTMDBStructuredContractChineseFallbackAndImageIsolation(t *testing.T) {
	c := fixture(t, "tmdb", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/t/p/") {
			if r.Header.Get("Authorization") != "" || r.Header.Get("X-Upstream") != "image.tmdb.org" {
				t.Error("credential leaked")
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\nsynthetic"))
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-secret" {
			t.Error("missing bearer")
		}
		switch r.URL.Path {
		case "/3/search/movie":
			if r.URL.Query().Get("language") != "zh-CN" {
				t.Error(r.URL)
			}
			jsonReply(w, `{"results":[{"id":9007199254740993,"title":"中文","original_title":"Original","release_date":"2024-01-01"}],"total_pages":1,"total_results":1}`)
		case "/3/movie/9007199254740993":
			if r.URL.Query().Get("language") == "zh-CN" {
				jsonReply(w, `{"id":9007199254740993,"title":"中文","original_title":"Original","poster_path":"/poster.jpg"}`)
			} else {
				jsonReply(w, `{"id":9007199254740993,"title":"English","overview":"fallback"}`)
			}
		case "/3/movie/9007199254740993/credits":
			jsonReply(w, `{"id":9007199254740993,"cast":[{"id":7,"name":"演员","profile_path":"/person.jpg","character":"角色","order":0}],"crew":[{"id":8,"name":"导演","job":"Director"}]}`)
		case "/3/movie/9007199254740993/images":
			jsonReply(w, `{"id":9007199254740993,"backdrops":[{"file_path":"/cn.jpg","iso_639_1":"zh","width":1600,"height":900},{"file_path":"/neutral.jpg","iso_639_1":null,"width":1920,"height":1080}]}`)
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	})
	result, e := c.Search(context.Background(), "movie", "Original")
	if e != nil || !result.Complete || len(result.Items) != 1 || result.Items[0].Ref.RemoteID != "9007199254740993" {
		t.Fatal(result, e)
	}
	title, e := c.FetchTitle(context.Background(), result.Items[0].Ref, Parts{true, true, true})
	if e != nil || title.Title != "中文" || title.Overview != "fallback" || len(title.Credits) != 2 || title.Artwork["thumb"] != "/cn.jpg" || title.Artwork["backdrop"] != "/neutral.jpg" {
		t.Fatal(title, e)
	}
	b, m, e := c.FetchImage(context.Background(), "primary", title.Artwork["primary"])
	if e != nil || len(b) == 0 || m != "image/png" {
		t.Fatal(m, e)
	}
}
func TestTVDBTranslationPeopleArtworkAndLoginContract(t *testing.T) {
	var logins atomic.Int32
	c := fixture(t, "tvdb", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v4/login" {
			logins.Add(1)
			var in map[string]string
			json.NewDecoder(r.Body).Decode(&in)
			if in["apikey"] != "synthetic-secret" || in["pin"] != "synthetic-pin" {
				t.Error(in)
			}
			jsonReply(w, `{"status":"success","data":{"token":"jwt"}}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer jwt" {
			t.Error("wrong auth")
		}
		switch r.URL.Path {
		case "/v4/search":
			if r.URL.Query().Has("language") {
				t.Error("original-language filter")
			}
			jsonReply(w, `{"status":"success","data":[{"tvdb_id":"42","type":"series","name":"Original","year":"2023","translations":{"zho":"中文"},"overviews":{"eng":"English overview"}}],"links":{"next":null}}`)
		case "/v4/series/42/extended":
			jsonReply(w, `{"status":"success","data":{"id":42,"name":"Original","year":"2023","image":"https://artworks.thetvdb.com/banners/poster.jpg","translations":{"nameTranslations":[{"language":"eng","name":"English"},{"language":"zho","name":"中文"}]},"characters":[{"peopleId":9,"personName":"演员","peopleType":"Actor","name":"角色"},{"peopleId":10,"personName":"导演","peopleType":"Director"},{"peopleId":11,"personName":"不猜职业","peopleType":"3"}],"artworks":[{"image":"https://artworks.thetvdb.com/banners/back.jpg","type":3,"width":1600,"height":900}]}}`)
		case "/v4/artwork/types":
			jsonReply(w, `{"status":"success","data":[{"id":3,"name":"Series Background","recordType":"series"}]}`)
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	})
	results, e := c.Search(context.Background(), "series", "Original")
	if e != nil || !results.Complete || len(results.Items) != 1 || results.Items[0].Title != "中文" {
		t.Fatal(results, e)
	}
	title, e := c.FetchTitle(context.Background(), results.Items[0].Ref, Parts{true, true, true})
	if e != nil || title.Title != "中文" || len(title.Credits) != 2 || title.Artwork["backdrop"] == "" || logins.Load() != 1 {
		t.Fatal(title, e, logins.Load())
	}
}
func TestMetaTubeRawIdentitySingleFetchAndSafeImageEndpoint(t *testing.T) {
	var details, images atomic.Int32
	c := fixture(t, "metatube", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/movies/search"):
			jsonReply(w, `{"data":[{"id":"Ab-C/中文","provider":"TEST","number":"ABC-123","title":"Title"}]}`)
		case strings.Contains(r.URL.Path, "/movies/test/"):
			details.Add(1)
			if !strings.Contains(r.RequestURI, "Ab-C%2F") {
				t.Error("raw ID not escaped", r.RequestURI)
			}
			jsonReply(w, `{"data":{"id":"Ab-C/中文","provider":"Test","number":"ABC-123","title":"Title","actors":["同名"],"director":"导演","big_thumb_url":"http://evil.invalid/secret"}}`)
		case strings.Contains(r.URL.Path, "/images/"):
			images.Add(1)
			if r.Header.Get("Authorization") != "" {
				t.Error("image credential")
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\nfixture"))
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	})
	result, e := c.Search(context.Background(), "movie", "ABC-123")
	if e != nil || len(result.Items) != 1 || result.Items[0].Ref.Namespace != "test" || result.Items[0].Ref.RemoteID != "Ab-C/中文" {
		t.Fatal(result, e)
	}
	title, e := c.FetchTitle(context.Background(), result.Items[0].Ref, Parts{true, true, true})
	if e != nil || details.Load() != 1 || len(title.Credits) != 2 || title.Credits[0].Key != "" || strings.Contains(title.Artwork["primary"], "evil") {
		t.Fatal(title, e)
	}
	if _, _, e = c.FetchImage(context.Background(), "primary", title.Artwork["primary"]); e != nil || images.Load() != 1 {
		t.Fatal(e)
	}
	if _, e = c.Search(context.Background(), "series", "x"); !errors.Is(e, model.ErrProvider) {
		t.Fatal(e)
	}
	if details.Load() != 1 {
		t.Fatal("series network request")
	}
}
func TestSearchCompletenessAndUntrustedResponses(t *testing.T) {
	for _, tc := range []struct {
		name, source, body string
		err, complete      bool
	}{
		{"truncated", "tmdb", `{"results":[{"id":1,"title":"A"}],"total_pages":2,"total_results":2}`, false, false},
		{"invalid-row", "tmdb", `{"results":[{"id":1,"title":"A"},{"id":2}],"total_pages":1,"total_results":2}`, false, false},
		{"duplicate", "tmdb", `{"results":[{"id":1,"title":"A"},{"id":1,"title":"A"}],"total_pages":1,"total_results":2}`, false, false},
		{"empty", "tmdb", `{"results":[],"total_pages":0,"total_results":0}`, false, true},
		{"malformed", "tmdb", `<html>synthetic-secret`, true, false},
		{"huge", "tmdb", strings.Repeat("x", (1<<20)+1), true, false},
		{"no-data", "metatube", `{}`, true, false},
		{"wrong-kind", "tvdb", `{"status":"success","data":[{"tvdb_id":"1","type":"movie","name":"A"}],"links":{"next":null}}`, false, false},
		{"next-page", "tvdb", `{"status":"success","data":[],"links":{"next":"url"}}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t, tc.source, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/login") {
					jsonReply(w, `{"status":"success","data":{"token":"jwt"}}`)
					return
				}
				jsonReply(w, tc.body)
			})
			kind := "movie"
			if tc.source == "tvdb" {
				kind = "series"
			}
			r, e := c.Search(context.Background(), kind, "A")
			if (e != nil) != tc.err || (e == nil && r.Complete != tc.complete) {
				t.Fatal(r, e)
			}
			if e != nil && strings.Contains(e.Error(), "synthetic") {
				t.Fatal("error leaked upstream")
			}
		})
	}
}
func TestHTTPBudgetsCooldownAndImageValidation(t *testing.T) {
	var requests atomic.Int32
	c := fixture(t, "tmdb", func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(429)
		fmt.Fprint(w, "synthetic-secret")
	})
	c.Search(context.Background(), "movie", "A")
	if until := c.api.Until(); until == nil || *until < time.Now().Add(110*time.Second).UnixMilli() {
		t.Fatal(until)
	}
	c.Search(context.Background(), "movie", "B")
	if requests.Load() != 1 {
		t.Fatal("cooldown bypass")
	}
	if c.images.Until() != nil {
		t.Fatal("API cools images")
	}
	other := &Gate{}
	if e := other.wait(context.Background()); e != nil {
		t.Fatal("other source blocked", e)
	}
	for _, tc := range []struct {
		name, mime, body string
		status           int
	}{
		{"html", "image/png", "<html>evil", 200}, {"svg", "image/svg+xml", "<svg/>", 200}, {"mismatch", "image/jpeg", "\x89PNG\r\n\x1a\n", 200}, {"huge", "image/png", strings.Repeat("a", (5<<20)+1), 200}, {"redirect", "image/png", "", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t, "tmdb", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.mime)
				w.Header().Set("Location", "http://evil.invalid/")
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			if _, _, e := c.FetchImage(context.Background(), "primary", "/x.jpg"); e == nil {
				t.Fatal("unsafe image accepted")
			}
		})
	}
	c = fixture(t, "tmdb", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := c.Search(ctx, "movie", "A"); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	for _, path := range []string{"https://evil.invalid/a.jpg", "/../a.jpg", "/a.svg", "//a.jpg"} {
		if _, _, e := c.FetchImage(context.Background(), "primary", path); e == nil {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"https://artworks.thetvdb.com.evil/banners/a.jpg", "https://artworks.thetvdb.com/banners/../a.jpg", "https://user@artworks.thetvdb.com/banners/a.jpg", "https://artworks.thetvdb.com/banners/%2e%2e/a.jpg"} {
		if tvdbImage(path) != "" {
			t.Fatal(path)
		}
	}
}

func TestOptionalFailureAndWrongRemoteIdentity(t *testing.T) {
	c := fixture(t, "tvdb", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/login") {
			jsonReply(w, `{"status":"success","data":{"token":"jwt"}}`)
			return
		}
		if r.URL.Query().Get("short") == "true" {
			jsonReply(w, `{"status":"success","data":{"id":1,"name":"Verified basic"}}`)
			return
		}
		w.WriteHeader(500)
	})
	title, e := c.FetchTitle(context.Background(), c.ref("movie", "1", "tvdb"), Parts{true, true, true})
	if e != nil || title.Title != "Verified basic" || title.CreditsState != "failed" || title.ArtworkState != "failed" || title.CreditsRetryAt == nil {
		t.Fatal(title, e)
	}
	for _, source := range []string{"tmdb", "tvdb", "metatube"} {
		t.Run(source, func(t *testing.T) {
			c := fixture(t, source, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/login") {
					jsonReply(w, `{"status":"success","data":{"token":"jwt"}}`)
					return
				}
				switch source {
				case "tmdb":
					jsonReply(w, `{"id":2,"title":"Wrong"}`)
				case "tvdb":
					jsonReply(w, `{"status":"success","data":{"id":2,"name":"Wrong"}}`)
				default:
					jsonReply(w, `{"data":{"id":"2","provider":"metatube","title":"Wrong"}}`)
				}
			})
			if _, e := c.FetchTitle(context.Background(), c.ref("movie", "1", source), Parts{true, true, true}); e == nil {
				t.Fatal("wrong remote identity accepted")
			}
		})
	}
}
