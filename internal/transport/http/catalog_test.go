package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reelnest/internal/model"
)

type catalogResult struct {
	Items []struct {
		ID, EntryKey, Title, EntryType string
		EpisodeCount, FileCount        int
		Episode                        *struct{ SeriesID string }
		Metadata                       json.RawMessage
	}
	Page struct {
		NextCursor *string
		ExpiresAt  *int64
	}
}

func readCatalog(t *testing.T, h *harness, path string) catalogResult {
	t.Helper()
	b := h.request(t, "GET", path, "", 200)
	var p catalogResult
	if e := json.Unmarshal(b, &p); e != nil {
		t.Fatal(e)
	}
	if len(b) > 256<<10 {
		t.Fatal("oversized response")
	}
	for _, bad := range []string{`"credits":`, `"bytes":`, `"seasons":`, `"path":`} {
		if strings.Contains(string(b), bad) {
			t.Fatal("heavy list DTO", bad)
		}
	}
	return p
}
func TestCatalogHTTPGroupingCursorsAndDetails(t *testing.T) {
	h := start(t)
	h.request(t, "GET", "/api/home", "", 401)
	h.login(t)
	for i := 0; i < 105; i++ {
		writeFile(t, filepath.Join(h.root, "movies", fmt.Sprintf("电影%d.webm", i)), "fixture")
	}
	lib, scan := h.create(t, filepath.Join(h.root, "movies"), "movie")
	h.await(t, scan, "completed")
	writeFile(t, filepath.Join(h.root, "tv", "节目", "S01E01.webm"), "fixture")
	writeFile(t, filepath.Join(h.root, "tv", "节目", "S01E01.4K.webm"), "fixture")
	writeFile(t, filepath.Join(h.root, "tv", "节目", "S01E02.webm"), "fixture")
	writeFile(t, filepath.Join(h.root, "tv", "节目", "S02E01.webm"), "fixture")
	tv, scan := h.create(t, filepath.Join(h.root, "tv"), "series")
	h.await(t, scan, "completed")
	if e := os.Mkdir(filepath.Join(h.root, "empty"), 0700); e != nil {
		t.Fatal(e)
	}
	_, scan = h.create(t, filepath.Join(h.root, "empty"), "movie")
	h.await(t, scan, "completed")
	b := h.request(t, "GET", "/api/home", "", 200)
	if len(b) > 256<<10 {
		t.Fatal(len(b))
	}
	var home struct {
		Libraries []struct{ Items []json.RawMessage }
		Recent    []json.RawMessage
	}
	json.Unmarshal(b, &home)
	if len(home.Libraries) != 3 || len(home.Recent) != 20 {
		t.Fatal(string(b))
	}
	p := readCatalog(t, h, "/api/catalog?libraryId="+lib)
	if len(p.Items) != 50 || p.Items[0].Title != "电影0" || p.Items[2].Title != "电影2" || p.Page.NextCursor == nil {
		t.Fatal(p)
	}
	seen := map[string]bool{}
	for {
		for _, c := range p.Items {
			if seen[c.ID] {
				t.Fatal("duplicate page item")
			}
			seen[c.ID] = true
		}
		if p.Page.NextCursor == nil {
			break
		}
		p = readCatalog(t, h, "/api/catalog?libraryId="+lib+"&cursor="+url.QueryEscape(*p.Page.NextCursor))
	}
	if len(seen) != 105 {
		t.Fatal(len(seen))
	}
	p = readCatalog(t, h, "/api/catalog?libraryId="+lib+"&limit=1")
	cursor := url.QueryEscape(*p.Page.NextCursor)
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&limit=2&cursor="+cursor, "", 409)
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&limit=1&q=movie&cursor="+cursor, "", 409)
	h.request(t, "GET", "/api/catalog?libraryId="+tv+"&limit=1&cursor="+cursor, "", 409)
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&limit=1&cursor=broken", "", 409)
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&limit=101", "", 400)
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&limit=0", "", 400)
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&sort=favorite", "", 400)
	h.request(t, "GET", "/api/catalog", "", 400)
	h.request(t, "GET", "/api/catalog/groups?view=recent", "", 400)
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&q=%ZZ", "", 400)
	h.request(t, "GET", "/api/media/missing", "", 404)
	id := p.Items[0].ID
	h.request(t, "GET", "/api/media/"+id, "", 200)
	h.request(t, "PUT", "/api/favorites/media/"+id, `{"favorite":true}`, 200)
	if _, e := h.store.SaveProgress(context.Background(), id, model.Progress{Position: 1, Duration: 60}); e != nil {
		t.Fatal(e)
	}
	// Ordinary cursors survive household state changes.
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&limit=1&cursor="+cursor, "", 200)
	h.request(t, "GET", "/api/catalog?view=continue", "", 200)
	b = h.request(t, "GET", "/api/catalog/groups?limit=1", "", 200)
	var groups struct {
		Items []struct {
			Library       struct{ ID string }
			Total         int
			EntriesCursor string
		}
		Page struct{ NextCursor *string }
	}
	json.Unmarshal(b, &groups)
	if len(groups.Items) != 1 || groups.Items[0].Total < 1 || groups.Items[0].EntriesCursor == "" || groups.Page.NextCursor == nil {
		t.Fatal(string(b))
	}
	g := groups.Items[0]
	h.request(t, "GET", "/api/catalog?libraryId="+g.Library.ID+"&limit=1&cursor="+url.QueryEscape(g.EntriesCursor), "", 200)
	h.request(t, "GET", "/api/catalog/groups?limit=1&cursor="+url.QueryEscape(*groups.Page.NextCursor), "", 200)
	p = readCatalog(t, h, "/api/catalog?libraryId="+tv+"&q=S01E01")
	if len(p.Items) != 1 || p.Items[0].FileCount != 4 || p.Items[0].EpisodeCount != 3 {
		t.Fatal(p)
	}
	series := p.Items[0].ID
	b = h.request(t, "GET", "/api/series/"+series+"?limit=1", "", 200)
	var detail struct {
		Seasons []struct{ Number int }
		Page    struct{ NextCursor *string }
	}
	json.Unmarshal(b, &detail)
	if len(detail.Seasons) != 1 || detail.Page.NextCursor == nil {
		t.Fatal(string(b))
	}
	h.request(t, "GET", "/api/series/"+series+"?limit=1&cursor="+url.QueryEscape(*detail.Page.NextCursor), "", 200)
	path := "/api/series/" + series + "/seasons/1/episodes?limit=1"
	n := 0
	for {
		b = h.request(t, "GET", path, "", 200)
		var page struct {
			Items []json.RawMessage
			Page  struct{ NextCursor *string }
		}
		json.Unmarshal(b, &page)
		n += len(page.Items)
		if page.Page.NextCursor == nil {
			break
		}
		path = "/api/series/" + series + "/seasons/1/episodes?limit=1&cursor=" + url.QueryEscape(*page.Page.NextCursor)
	}
	if n != 3 {
		t.Fatal(n)
	}
	unmatched := readCatalog(t, h, "/api/catalog?libraryId="+tv+"&view=unmatched")
	if len(unmatched.Items) != 4 || unmatched.Items[0].Episode == nil {
		t.Fatal(unmatched)
	}
	h.request(t, "GET", "/api/media/"+unmatched.Items[0].ID, "", 200)
	h.request(t, "PUT", "/api/favorites/series/"+series, `{"favorite":true}`, 200)
	h.request(t, "PUT", "/api/favorites/media/"+unmatched.Items[0].ID, `{"favorite":true}`, 200)
	fav := readCatalog(t, h, "/api/catalog?libraryId="+tv+"&view=favorites&limit=1")
	if fav.Page.NextCursor == nil {
		t.Fatal(fav)
	}
	h.request(t, "PUT", "/api/favorites/series/"+series, `{"favorite":false}`, 200)
	h.request(t, "GET", "/api/catalog?libraryId="+tv+"&view=favorites&limit=1&cursor="+url.QueryEscape(*fav.Page.NextCursor), "", 409)
	// A catalog mutation stales the old group seed and regular page alike.
	h.request(t, "PATCH", "/api/libraries/"+lib, `{"revision":1,"name":"改名"}`, 200)
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&limit=1&cursor="+cursor, "", 409)
	h.request(t, "GET", "/api/catalog?libraryId="+g.Library.ID+"&limit=1&cursor="+url.QueryEscape(g.EntriesCursor), "", 409)
}
func TestCatalogHTTPExpiryBoundsIncludingExcludedRows(t *testing.T) {
	h := start(t)
	h.login(t)
	ctx := context.Background()
	for _, name := range []string{"a", "b", "c"} {
		writeFile(t, filepath.Join(h.root, "movies", name+".webm"), "fixture")
	}
	lib, scan := h.create(t, filepath.Join(h.root, "movies"), "movie")
	h.await(t, scan, "completed")
	now := time.Now().UnixMilli()
	files, e := h.store.MediaFiles(ctx, lib, "", 10)
	if e != nil {
		t.Fatal(e)
	}
	ref := model.RemoteRef{Source: "tmdb", CatalogID: "tmdb", Namespace: "tmdb", Kind: "movie", RemoteID: "1", ProviderRevision: 1}
	target, e := h.store.BeginSelection(ctx, files[0].SubjectID, 1, 0, []model.ProviderVersion{{Source: "tmdb", CatalogID: "tmdb", Revision: 1}}, ref)
	if e != nil {
		t.Fatal(e)
	}
	// Trusted store fixture: short TTL, no external network, full transaction path.
	expiry := now + 1000
	title := model.Title{Ref: model.RemoteRef{Source: "tmdb", CatalogID: "tmdb", Namespace: "tmdb", Kind: "movie", RemoteID: "1", ProviderRevision: 1}, Title: "资料", FetchedAt: now, ExpiresAt: expiry, CreditsState: "ready", ArtworkState: "ready"}
	if e = h.store.CompleteMetadata(ctx, model.MetadataResult{Target: target, Title: &title, Selection: "manual"}, now); e != nil {
		t.Fatal(e)
	}
	p := readCatalog(t, h, "/api/catalog?libraryId="+lib+"&view=unmatched&limit=1")
	if p.Page.NextCursor == nil || p.Page.ExpiresAt == nil || *p.Page.ExpiresAt != expiry {
		t.Fatal(p)
	}
	time.Sleep(time.Until(time.UnixMilli(expiry)) + 10*time.Millisecond)
	h.request(t, "GET", "/api/catalog?libraryId="+lib+"&view=unmatched&limit=1&cursor="+url.QueryEscape(*p.Page.NextCursor), "", 409)
	fresh := readCatalog(t, h, "/api/catalog?libraryId="+lib+"&view=unmatched")
	if len(fresh.Items) != 3 {
		t.Fatal(fresh)
	}
}
