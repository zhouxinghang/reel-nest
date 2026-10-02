package metadata_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reelnest/internal/metadata"
	"reelnest/internal/model"
	"reelnest/internal/store"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func engineSetup(t *testing.T, h http.HandlerFunc) (*store.Store, *metadata.Service) {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	s, old, _ := setup(t)
	old.Close()
	m, e := metadata.NewWithOptions(context.Background(), s, metadata.Options{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		next := r.Clone(r.Context())
		v := *r.URL
		next.URL = &v
		next.Header.Set("X-Upstream", r.URL.Host)
		next.URL.Host = u.Host
		next.URL.Scheme = u.Scheme
		return http.DefaultTransport.RoundTrip(next)
	})})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	return s, m
}
func subjects(t *testing.T, s *store.Store, kind string, sources []string, n int) (model.Library, []model.MediaFile) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	l, e := s.CreateLibrary(ctx, model.Library{ID: model.NewID(), Name: "isolated", Path: root, Identity: model.NewID(), Kind: kind, Sources: sources})
	if e != nil {
		t.Fatal(e)
	}
	scan, e := s.ClaimScan(ctx)
	if e != nil {
		t.Fatal(e)
	}
	entries := []model.ScanEntry{}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("Movie%d (2024).mp4", i)
		if e = os.WriteFile(filepath.Join(root, name), []byte("fixture"), 0600); e != nil {
			t.Fatal(e)
		}
		entries = append(entries, model.ScanEntry{Relative: name, Fingerprint: model.NewID(), Size: 7, ModifiedAt: time.Now().UnixMilli()})
	}
	if e = s.StageScan(ctx, scan.ID, entries); e != nil {
		t.Fatal(e)
	}
	if e = s.CommitScan(ctx, scan.ID); e != nil {
		t.Fatal(e)
	}
	files, e := s.MediaFiles(ctx, l.ID, "", 256)
	if e != nil || len(files) != n {
		t.Fatal(files, e)
	}
	return l, files
}
func versions(t *testing.T, m *metadata.Service) []model.ProviderVersion {
	t.Helper()
	p, e := m.Settings()
	if e != nil {
		t.Fatal(e)
	}
	out := []model.ProviderVersion{}
	for _, v := range p {
		out = append(out, model.ProviderVersion{Source: v.Source(), CatalogID: v.CatalogID(), Revision: v.Revision()})
	}
	return out
}
func seed(t *testing.T, s *store.Store, m *metadata.Service, file model.MediaFile, source, id string) model.Match {
	t.Helper()
	ctx := context.Background()
	p := snapshot(t, m, source)
	ref := model.RemoteRef{Source: source, CatalogID: p.CatalogID(), ProviderRevision: p.Revision(), Namespace: source, RemoteID: id, Kind: "movie"}
	if source == "metatube" {
		ref.Namespace = "test"
	}
	target, e := s.BeginSelection(ctx, file.SubjectID, 1, 0, []model.ProviderVersion{{Source: source, CatalogID: p.CatalogID(), Revision: p.Revision()}}, ref)
	if e != nil {
		t.Fatal(e)
	}
	reference := "/" + id + ".jpg"
	if source == "tvdb" {
		reference = "https://artworks.thetvdb.com/banners/" + id + ".jpg"
	}
	if source == "metatube" {
		b, _ := json.Marshal(ref)
		reference = string(b)
	}
	title := model.Title{Ref: ref, Title: "电影", OriginalTitle: "Movie", FetchedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), CreditsState: "ready", ArtworkState: "ready", Credits: []model.Credit{{Key: "7", Name: "Actor", Role: "actor"}}, Artwork: map[string]string{"primary": reference}}
	if source == "metatube" {
		title.Credits[0].Key = ""
	}
	if e = s.CompleteMetadata(ctx, model.MetadataResult{Target: target, Title: &title, Selection: "manual"}, time.Now().UnixMilli()); e != nil {
		t.Fatal(e)
	}
	out, e := s.MetadataMatch(ctx, file.SubjectID, time.Now().UnixMilli())
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func waitUntil(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal("condition timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func reply(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, s)
}
func TestWorkerPriorityFallbackAndCachedManualRefresh(t *testing.T) {
	var tmdb, tvdb, details atomic.Int32
	s, m := engineSetup(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/3/search/movie":
			tmdb.Add(1)
			if r.URL.Query().Get("query") != "Movie0" {
				t.Error("filename query", r.URL)
			}
			reply(w, `{"results":[{"id":1,"title":"Movie0","release_date":"2024-01-01"}],"total_pages":2,"total_results":21}`)
		case "/v4/login":
			reply(w, `{"status":"success","data":{"token":"jwt"}}`)
		case "/v4/search":
			tvdb.Add(1)
			reply(w, `{"status":"success","data":[{"tvdb_id":"42","type":"movie","name":"Movie0","year":"2024"}],"links":{"next":null}}`)
		case "/v4/movies/42/extended":
			details.Add(1)
			reply(w, `{"status":"success","data":{"id":42,"name":"Movie0","year":"2024","characters":[],"artworks":[]}}`)
		case "/v4/artwork/types":
			reply(w, `{"status":"success","data":[]}`)
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	})
	save(t, m, "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("t")})
	save(t, m, "tvdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("v")})
	_, files := subjects(t, s, "movie", []string{"tmdb", "tvdb"}, 1)
	if e := m.Start(); e != nil {
		t.Fatal(e)
	}
	var matched model.Match
	waitUntil(t, func() bool {
		matched, _ = s.MetadataMatch(context.Background(), files[0].SubjectID, time.Now().UnixMilli())
		return matched.Title != nil
	})
	if matched.Target.Ref.Source != "tvdb" || tmdb.Load() != 1 || tvdb.Load() != 1 {
		t.Fatal(matched, tmdb.Load(), tvdb.Load())
	}
	got, e := m.Select(context.Background(), matched.Target.ID, matched.Target.SubjectRevision, matched.Target.BindingRevision, matched.Target.Ref)
	if e != nil || got.Target.Selection != "manual" {
		t.Fatal(got, e)
	}
	before := details.Load()
	m.Refresh(context.Background(), "")
	waitUntil(t, func() bool {
		status, _ := s.MetadataStatus(context.Background(), "", "", 100, 0)
		return status.Counts["succeeded"] == 1
	})
	if details.Load() != before {
		t.Fatal("fresh cache refetched")
	}
}
func TestSlowWorkersBoundedSettingsImmediateAndOldResultsRejected(t *testing.T) {
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	var active, maxActive atomic.Int32
	s, m := engineSetup(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/search/movie") && r.Header.Get("Authorization") == "Bearer old" {
			n := active.Add(1)
			for {
				old := maxActive.Load()
				if n <= old || maxActive.CompareAndSwap(old, n) {
					break
				}
			}
			defer active.Add(-1)
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			reply(w, `{"results":[{"id":1,"title":"old"}],"total_pages":1,"total_results":1}`)
			return
		}
		reply(w, `{"results":[],"total_pages":0,"total_results":0}`)
	})
	save(t, m, "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("old")})
	_, files := subjects(t, s, "movie", []string{"tmdb"}, 6)
	if e := m.Start(); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("workers not entered")
		}
	}

	samples := make([]time.Duration, 40)
	for i := range samples {
		start := time.Now()
		save(t, m, "tmdb", metadata.SettingsPatch{Revision: int64(i + 2), Credential: ptr(fmt.Sprintf("new-%d", i))})
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	if samples[37] > 200*time.Millisecond {
		t.Fatal("settings p95 waited for network", samples[37])
	}
	t.Logf("40 settings saves with both workers blocked: p95=%s max=%s (small fixture, not production benchmark)", samples[37], samples[39])

	status, e := s.MetadataStatus(context.Background(), "", "", 100, 0)
	if e != nil || status.Counts["queued"] != 6 {
		t.Fatal(status, e)
	}
	close(release)
	waitUntil(t, func() bool {
		status, _ = s.MetadataStatus(context.Background(), "", "", 100, 0)
		return status.Counts["failed"] == 6
	})
	if maxActive.Load() != 2 {
		t.Fatal("worker bound", maxActive.Load())
	}
	for _, f := range files {
		v, _ := s.MetadataMatch(context.Background(), f.SubjectID, time.Now().UnixMilli())
		if v.Title != nil {
			t.Fatal("old result published")
		}
	}
}
func TestManualInFlightConfigurationChangePreservesChoice(t *testing.T) {
	for _, upstreamFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(upstreamFailure), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			s, m := engineSetup(t, func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				if upstreamFailure {
					w.WriteHeader(500)
					return
				}
				reply(w, `{"data":{"id":"NewID","provider":"test","title":"New","actors":[]}}`)
			})
			save(t, m, "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("http://old.invalid")})
			_, files := subjects(t, s, "movie", []string{"metatube"}, 1)
			old := seed(t, s, m, files[0], "metatube", "OldID")
			ref := old.Target.Ref
			ref.RemoteID = "NewID"
			done := make(chan error, 1)
			go func() {
				_, e := m.Select(context.Background(), old.Target.ID, old.Target.SubjectRevision, old.Target.BindingRevision, ref)
				done <- e
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("not started")
			}
			save(t, m, "metatube", metadata.SettingsPatch{Revision: 2, URL: ptr("http://new.invalid")})
			close(release)
			if e := <-done; !errors.Is(e, model.ErrConflict) {
				t.Fatal(e)
			}
			got, e := s.MetadataMatch(context.Background(), old.Target.ID, time.Now().UnixMilli())
			if e != nil || got.Target.Ref.RemoteID != "OldID" || got.Target.Ref.CatalogID != old.Target.Ref.CatalogID || got.AttemptState != "succeeded" {
				t.Fatal(got, e)
			}
			if _, e = m.Select(context.Background(), old.Target.ID, 1, got.Target.BindingRevision, ref); !errors.Is(e, model.ErrCandidate) {
				t.Fatal("old catalog accepted", e)
			}
		})
	}
}
func TestAssetsSingleflightCancellationHitAndFailureBackoff(t *testing.T) {
	entered, release := make(chan struct{}, 4), make(chan struct{})
	var downloads atomic.Int32
	s, m := engineSetup(t, func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		if strings.Contains(r.URL.Path, "/2.jpg") {
			w.WriteHeader(500)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("credential leak")
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\nfixture"))
	})
	save(t, m, "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("token")})
	_, files := subjects(t, s, "movie", []string{"tmdb"}, 3)
	first := seed(t, s, m, files[0], "tmdb", "1")
	second := seed(t, s, m, files[1], "tmdb", "2")
	cached := seed(t, s, m, files[2], "tmdb", "3")
	a, e := s.MetadataAsset(context.Background(), cached.Artwork["primary"], time.Now().UnixMilli())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteAsset(context.Background(), a, []byte("\x89PNG\r\n\x1a\nfixture"), "image/png", false, time.Now().UnixMilli()); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() { _, e := m.Asset(ctx, first.Artwork["primary"]); canceled <- e }()
	<-entered
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		go func() { _, e := m.Asset(context.Background(), first.Artwork["primary"]); results <- e }()
	}
	cancel()
	if e := <-canceled; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	start := time.Now()
	if _, e = m.Asset(context.Background(), cached.Artwork["primary"]); e != nil || time.Since(start) > 100*time.Millisecond {
		t.Fatal("hit blocked behind slow image", e)
	}
	close(release)
	for i := 0; i < 20; i++ {
		if e = <-results; e != nil {
			t.Fatal(e)
		}
	}
	if downloads.Load() != 1 {
		t.Fatal("duplicate downloads", downloads.Load())
	}
	if _, e = m.Asset(context.Background(), second.Artwork["primary"]); !errors.Is(e, model.ErrAsset) {
		t.Fatal(e)
	}
	if _, e = m.Asset(context.Background(), second.Artwork["primary"]); !errors.Is(e, model.ErrAsset) {
		t.Fatal(e)
	}
	if downloads.Load() != 2 {
		t.Fatal("negative cache not respected", downloads.Load())
	}
}
func TestAssetsGlobalAndSourceBoundsAndStaleConfig(t *testing.T) {
	entered := make(chan struct{}, 20)
	release := make(chan struct{})
	var mu sync.Mutex
	active, total, maxTotal := map[string]int{}, 0, 0
	maximum := map[string]int{}
	s, m := engineSetup(t, func(w http.ResponseWriter, r *http.Request) {
		source := r.Header.Get("X-Upstream")
		mu.Lock()
		active[source]++
		total++
		maximum[source] = max(maximum[source], active[source])
		maxTotal = max(maxTotal, total)
		mu.Unlock()
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		defer func() { mu.Lock(); active[source]--; total--; mu.Unlock() }()
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("\x89PNG\r\n\x1a\nfixture"))
	})
	save(t, m, "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("t")})
	save(t, m, "tvdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("v")})
	save(t, m, "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("http://meta.invalid")})
	_, files := subjects(t, s, "movie", []string{"tmdb", "tvdb", "metatube"}, 8)
	matches := []model.Match{}
	for i, f := range files {
		source := []string{"tmdb", "tmdb", "tmdb", "tmdb", "tvdb", "tvdb", "metatube", "metatube"}[i]
		matches = append(matches, seed(t, s, m, f, source, fmt.Sprint(i+1)))
	}
	results := make(chan error, 8)
	for _, v := range matches {
		go func(id string) { _, e := m.Asset(context.Background(), id); results <- e }(v.Artwork["primary"])
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("global downloads not concurrent")
		}
	}
	save(t, m, "tmdb", metadata.SettingsPatch{Revision: 2, Credential: ptr("rotated")})
	close(release)
	for i := 0; i < 8; i++ {
		e := <-results
		if e != nil && !errors.Is(e, model.ErrConflict) {
			t.Fatal(e)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if maxTotal != 4 {
		t.Fatal("global bound", maxTotal)
	}
	for source, n := range maximum {
		if n > 2 {
			t.Fatal("source bound", source, n)
		}
	}
	for i := 0; i < 4; i++ {
		a, e := s.MetadataAsset(context.Background(), matches[i].Artwork["primary"], time.Now().UnixMilli())
		if e != nil || a.State == "ready" || a.State == "cooling" {
			t.Fatal("stale config polluted image", a.State, e)
		}
	}
}

func TestEngineRestartRecoversRunningWorkAndRetainsManualIdentity(t *testing.T) {
	entered := make(chan struct{}, 1)
	var allow atomic.Bool
	s, old := engineSetup(t, func(w http.ResponseWriter, r *http.Request) {
		if !allow.Load() {
			entered <- struct{}{}
			<-r.Context().Done()
			return
		}
		switch {
		case strings.Contains(r.URL.Path, "/movies/search"):
			reply(w, `{"data":[{"id":"Raw-ID","provider":"test","title":"Movie0","release_date":"2024-01-01"}]}`)
		default:
			reply(w, `{"data":{"id":"Raw-ID","provider":"test","title":"Movie0","actors":[]}}`)
		}
	})
	p := save(t, old, "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("http://restart.invalid")})
	_, files := subjects(t, s, "movie", []string{"metatube"}, 1)
	if e := old.Start(); e != nil {
		t.Fatal(e)
	}
	<-entered
	old.Close()
	// Restart with a controlled upstream at the same configured directory space.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "search") {
			reply(w, `{"data":[{"id":"Raw-ID","provider":"test","title":"Movie0","release_date":"2024-01-01"}]}`)
		} else {
			reply(w, `{"data":{"id":"Raw-ID","provider":"test","title":"Movie0","actors":[]}}`)
		}
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	next, e := metadata.NewWithOptions(context.Background(), s, metadata.Options{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		v := r.Clone(r.Context())
		q := *r.URL
		v.URL = &q
		v.URL.Scheme = u.Scheme
		v.URL.Host = u.Host
		return http.DefaultTransport.RoundTrip(v)
	})})
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	if e = next.Start(); e != nil {
		t.Fatal(e)
	}
	var m model.Match
	waitUntil(t, func() bool {
		m, _ = s.MetadataMatch(context.Background(), files[0].SubjectID, time.Now().UnixMilli())
		return m.Title != nil
	})
	if m.Target.Ref.CatalogID != p.CatalogID() || m.Target.Ref.RemoteID != "Raw-ID" {
		t.Fatal(m)
	}
	m, e = next.Select(context.Background(), m.Target.ID, 1, m.Target.BindingRevision, m.Target.Ref)
	if e != nil || m.Target.Selection != "manual" {
		t.Fatal(m, e)
	}
	next.Close()
	third, e := metadata.New(context.Background(), s)
	if e != nil {
		t.Fatal(e)
	}
	defer third.Close()
	m, e = s.MetadataMatch(context.Background(), m.Target.ID, time.Now().UnixMilli())
	if e != nil || m.Target.Selection != "manual" {
		t.Fatal(m, e)
	}
}
func TestMetaTubePeopleNamesAndCatalogsNeverMerge(t *testing.T) {
	s, m := engineSetup(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected network"); w.WriteHeader(500) })
	save(t, m, "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("http://one.invalid")})
	_, files := subjects(t, s, "movie", []string{"metatube"}, 3)
	a := seed(t, s, m, files[0], "metatube", "Same-ID")
	b := seed(t, s, m, files[1], "metatube", "Other-ID")
	if a.Credits[0].PersonID == b.Credits[0].PersonID {
		t.Fatal("names merged across titles")
	}
	save(t, m, "metatube", metadata.SettingsPatch{Revision: 2, URL: ptr("http://two.invalid")})
	c := seed(t, s, m, files[2], "metatube", "Same-ID")
	if a.Target.RemoteTitleID == c.Target.RemoteTitleID || a.Credits[0].PersonID == c.Credits[0].PersonID {
		t.Fatal("catalogs merged")
	}
	for _, v := range []model.Match{a, b, c} {
		works, _, _, e := s.MetadataPersonWorks(context.Background(), v.Credits[0].PersonID, "", 100, 0, time.Now().UnixMilli())
		if e != nil || len(works) != 1 {
			t.Fatal(works, e)
		}
	}
}

func TestOnlyDueOptionalPartsRetryWithoutExtendingTTL(t *testing.T) {
	var calls atomic.Int32
	s, m := engineSetup(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/3/movie/1/credits" {
			t.Error("refetched valid basic/artwork", r.URL)
		}
		reply(w, `{"id":1,"cast":[{"id":9,"name":"Actor","character":"Role"}],"crew":[]}`)
	})
	save(t, m, "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("token")})
	_, files := subjects(t, s, "movie", []string{"tmdb"}, 1)
	cached := seed(t, s, m, files[0], "tmdb", "1")
	ref := cached.Target.Ref
	ctx := context.Background()
	target, e := s.BeginSelection(ctx, cached.Target.ID, 1, cached.Target.BindingRevision, versions(t, m), ref)
	if e != nil {
		t.Fatal(e)
	}
	title := *cached.Title
	past := time.Now().Add(-time.Second).UnixMilli()
	title.Partial = true
	title.UpdateCredits = true
	title.CreditsState = "failed"
	title.CreditsRetryAt = &past
	if e = s.CompleteMetadata(ctx, model.MetadataResult{Target: target, Title: &title, Selection: "manual"}, time.Now().UnixMilli()); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Refresh(ctx, ""); e != nil {
		t.Fatal(e)
	}
	if e = m.Start(); e != nil {
		t.Fatal(e)
	}
	var got model.Match
	waitUntil(t, func() bool {
		got, _ = s.MetadataMatch(ctx, target.ID, time.Now().UnixMilli())
		return got.Title != nil && got.Title.CreditsState == "ready"
	})
	if calls.Load() != 1 || got.Title.ExpiresAt != cached.Title.ExpiresAt || got.Title.FetchedAt != cached.Title.FetchedAt || len(got.Credits) != 1 {
		t.Fatal(got, calls.Load())
	}
	if _, e = m.Refresh(ctx, ""); e != nil {
		t.Fatal(e)
	}
	waitUntil(t, func() bool {
		status, _ := s.MetadataStatus(ctx, "", "", 100, 0)
		return status.Counts["succeeded"] == 1
	})
	if calls.Load() != 1 {
		t.Fatal("fresh supplemental parts refetched")
	}
}

func TestInFlightAssetCannotSurviveRebindingOrScanRemoval(t *testing.T) {
	for _, action := range []string{"rebind", "remove", "delete"} {
		t.Run(action, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			s, m := engineSetup(t, func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				w.Header().Set("Content-Type", "image/png")
				w.Write([]byte("\x89PNG\r\n\x1a\nold"))
			})
			save(t, m, "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("token")})
			library, files := subjects(t, s, "movie", []string{"tmdb"}, 1)
			old := seed(t, s, m, files[0], "tmdb", "1")
			ctx := context.Background()
			done := make(chan error, 1)
			go func() { _, e := m.Asset(ctx, old.Artwork["primary"]); done <- e }()
			<-entered
			switch action {
			case "rebind":
				ref := old.Target.Ref
				ref.RemoteID = "2"
				target, e := s.BeginSelection(ctx, old.Target.ID, 1, 1, versions(t, m), ref)
				if e != nil {
					t.Fatal(e)
				}
				title := *old.Title
				title.Ref = ref
				title.Title = "new"
				title.Artwork = map[string]string{"primary": "/new.jpg"}
				if e = s.CompleteMetadata(ctx, model.MetadataResult{Target: target, Title: &title, Selection: "manual"}, time.Now().UnixMilli()); e != nil {
					t.Fatal(e)
				}
			case "remove":
				if _, e := s.QueueScan(ctx, library.ID); e != nil {
					t.Fatal(e)
				}
				scan, e := s.ClaimScan(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if e = s.CommitScan(ctx, scan.ID); e != nil {
					t.Fatal(e)
				}
			case "delete":
				if e := s.MarkFileDeleted(ctx, files[0].ID); e != nil {
					t.Fatal(e)
				}
			}
			close(release)
			if e := <-done; !errors.Is(e, model.ErrConflict) {
				t.Fatal("obsolete image committed", e)
			}
		})
	}
}
