package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"reelnest/internal/model"
)

func catalogPage(t *testing.T, s *Store, q model.CatalogQuery) model.CatalogPage {
	t.Helper()
	p, e := s.Catalog(context.Background(), q, model.CatalogPosition{}, 100, 0, time.Now().UnixMilli())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestCatalogCollapseSearchUnmatchedFavoritesAndContinue(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l := newScanLibrary(t, s, "series")
	scan := beginScan(t, s, l, true)
	one := entry("电视剧/特殊名字-S01E01.webm", "1")
	one.Organization = model.Organization{Title: "电视剧", Key: "show", Season: 1, Episode: 1}
	two := entry("电视剧/S01E02.webm", "2")
	two.Organization = model.Organization{Title: "电视剧", Key: "show", Season: 1, Episode: 2}
	version := entry("电视剧/4K-S01E01.webm", "3")
	version.Organization = one.Organization
	raw := entry("unknown.webm", "4")
	raw.Organization.Reason = "unrecognized"
	stage(t, s, scan, one, two, version, raw)
	publish(t, s, scan)
	q := model.CatalogQuery{LibraryID: l.ID, View: "browse", Sort: "title"}
	p := catalogPage(t, s, q)
	if len(p.Items) != 2 {
		t.Fatal(p)
	}
	q.Query = "特殊名字"
	p = catalogPage(t, s, q)
	if len(p.Items) != 1 || p.Items[0].SeriesID == "" || p.Items[0].EpisodeCount != 2 || p.Items[0].FileCount != 3 {
		t.Fatal(p)
	}
	series := p.Items[0].SeriesID
	q.Query = ""
	q.View = "unmatched"
	p = catalogPage(t, s, q)
	if len(p.Items) != 4 {
		t.Fatal(p)
	}
	var media string
	for _, c := range p.Items {
		if c.ParentSeriesID != "" {
			media = c.MediaFileID
			break
		}
	}
	for _, v := range []struct{ kind, id string }{{"media", media}, {"series", series}} {
		if _, e := s.Favorite(ctx, v.kind, v.id, true); e != nil {
			t.Fatal(e)
		}
	}
	q.View = "favorites"
	q.Sort = "favorite"
	p = catalogPage(t, s, q)
	if len(p.Items) != 2 {
		t.Fatal(p)
	}
	if _, e := s.SaveProgress(ctx, media, model.Progress{Position: 10, Duration: 60}); e != nil {
		t.Fatal(e)
	}
	q.View = "continue"
	q.Sort = "progress"
	p = catalogPage(t, s, q)
	if len(p.Items) != 1 || p.Items[0].MediaFileID != media || p.Items[0].Progress.Position != 10 {
		t.Fatal(p)
	}
	episodes, e := s.Episodes(ctx, series, 1, 0, "", 1, 0, time.Now().UnixMilli())
	if e != nil || !episodes.More || len(episodes.Items) != 1 {
		t.Fatal(episodes, e)
	}
	first := episodes.Items[0]
	next, e := s.Episodes(ctx, series, 1, first.Episode, first.MediaFileID, 100, episodes.Revision, time.Now().UnixMilli())
	if e != nil || len(next.Items) != 2 || next.Items[1].Episode != 2 {
		t.Fatal(next, e)
	}
	if e = s.MarkFileDeleted(ctx, media); e != nil {
		t.Fatal(e)
	}
	p = catalogPage(t, s, q)
	if len(p.Items) != 0 {
		t.Fatal("deleted file continued")
	}
	d, e := s.SeriesDetail(ctx, series, -1, 50, 0, time.Now().UnixMilli())
	if e != nil || d.Card.AvailableFileCount != 2 || d.Seasons[0].FileCount != 3 {
		t.Fatal(d, e)
	}
}
func TestCatalogNaturalOrderLiteralSubstringAndStaticPages(t *testing.T) {
	s := openTestStore(t)
	l := newScanLibrary(t, s, "movie")
	scan := beginScan(t, s, l, true)
	names := []string{"第10集", "第2集", "第1集", "北京", "上海", "百分%号", "下划_线", "ÉCOLE", "same", "SAME"}
	for i, name := range names {
		stage(t, s, scan, entry(name+".webm", fmt.Sprint(i)))
	}
	publish(t, s, scan)
	q := model.CatalogQuery{LibraryID: l.ID, View: "browse", Sort: "title"}
	all := catalogPage(t, s, q)
	var pos model.CatalogPosition
	keys := []string{}
	var revision int64
	for {
		p, e := s.Catalog(context.Background(), q, pos, 2, revision, time.Now().UnixMilli())
		if e != nil {
			t.Fatal(e)
		}
		revision = p.Revision
		for _, c := range p.Items {
			keys = append(keys, c.EntryKey)
		}
		if !p.More {
			break
		}
		pos = p.Last
	}
	want := []string{}
	titles := []string{}
	for _, c := range all.Items {
		want = append(want, c.EntryKey)
		titles = append(titles, c.Title)
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatal(keys, want)
	}
	index := map[string]int{}
	for i, title := range titles {
		index[title] = i
	}
	if !(index["第1集"] < index["第2集"] && index["第2集"] < index["第10集"] && index["北京"] < index["上海"]) {
		t.Fatal(titles)
	}
	for _, term := range []string{"%", "_", "école", "e\u0301cole"} {
		q.Query = term
		p := catalogPage(t, s, q)
		if len(p.Items) != 1 {
			t.Fatal(term, p)
		}
	}
	// Case folding is concurrency safe and deterministic.
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				if titleKey("北京2") >= titleKey("北京10") {
					t.Error("unstable collation")
				}
			}
		}()
	}
	wg.Wait()
}
func TestCatalogGroupOrderingGlobalRecentAndEmptyLibraries(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	libs := []model.Library{}
	for n := 0; n < 3; n++ {
		l := newScanLibrary(t, s, "movie")
		libs = append(libs, l)
		scan := beginScan(t, s, l, true)
		if n < 2 {
			for i := 0; i < 25; i++ {
				stage(t, s, scan, entry(fmt.Sprintf("%c-%02d.webm", 'B'-n, i), fmt.Sprint(i)))
			}
		}
		publish(t, s, scan)
	}
	// Equal times across libraries deliberately exercise the stable entry key.
	execSQL(t, s, `UPDATE media_files SET added_at=1000`)
	execSQL(t, s, `UPDATE catalog_entries SET added_at=1000`)
	q := model.CatalogQuery{View: "browse", Sort: "title"}
	groups, e := s.CatalogGroups(ctx, q, model.CatalogPosition{}, 1, 0, time.Now().UnixMilli())
	if e != nil || len(groups.Items) != 1 || !groups.More || groups.Items[0].Library.ID != libs[1].ID || groups.Items[0].Total != 25 {
		t.Fatal(groups, e)
	}
	next, e := s.CatalogGroups(ctx, q, groups.Items[0].Position, 1, groups.Revision, time.Now().UnixMilli())
	if e != nil || next.More || next.Items[0].Library.ID != libs[0].ID {
		t.Fatal(next, e)
	}
	h, e := s.Home(ctx, "", 0, time.Now().UnixMilli())
	if e != nil || len(h.Libraries) != 3 || len(h.Recent) != 20 {
		t.Fatal(h, e)
	}
	empty := false
	keys := []string{}
	for _, l := range h.Libraries {
		if len(l.Items) == 0 {
			empty = true
		}
	}
	for _, c := range h.Recent {
		keys = append(keys, c.EntryKey)
	}
	if !empty || !sort.StringsAreSorted(keys) {
		t.Fatal("empty libraries or stable tie order")
	}
	// The newest 20 can all belong to one library: never a per-library top-N merge.
	execSQL(t, s, `UPDATE catalog_entries SET added_at=2000 WHERE library_id=?`, libs[0].ID)
	h, e = s.Home(ctx, "", 0, time.Now().UnixMilli())
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range h.Recent {
		if c.LibraryID != libs[0].ID {
			t.Fatal("per-library truncated recent")
		}
	}
}
func TestCatalogRevisionsExpiryAndProjectionRollback(t *testing.T) {
	s, l, target, title := metadataFixture(t)
	ctx := context.Background()
	finishTitle(t, s, target, title)
	f := files(t, s, l)[0]
	q := model.CatalogQuery{LibraryID: l.ID, View: "browse", Sort: "title"}
	p := catalogPage(t, s, q)
	if len(p.Items) != 1 || p.Items[0].Title != title.Title {
		t.Fatal(p)
	}
	if _, e := s.Favorite(ctx, "media", f.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveProgress(ctx, f.ID, model.Progress{Position: 10, Duration: 60}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Catalog(ctx, q, model.CatalogPosition{}, 50, p.Revision, time.Now().UnixMilli()); e != nil {
		t.Fatal("ordinary cursor invalidated by household state", e)
	}
	q.View = "favorites"
	q.Sort = "favorite"
	fav := catalogPage(t, s, q)
	if _, e := s.Favorite(ctx, "media", f.ID, false); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Catalog(ctx, q, model.CatalogPosition{}, 50, fav.Revision, time.Now().UnixMilli()); !errors.Is(e, model.ErrCursor) {
		t.Fatal(e)
	}
	// Expiry affects title, match and unmatched immediately, without any writes.
	expiry := title.ExpiresAt
	q.View = "unmatched"
	q.Sort = "title"
	fresh, e := s.Catalog(ctx, q, model.CatalogPosition{}, 50, 0, expiry-1)
	if e != nil || len(fresh.Items) != 0 || fresh.Expires != expiry {
		t.Fatal(fresh, e)
	}
	stale, e := s.Catalog(ctx, q, model.CatalogPosition{}, 50, 0, expiry)
	if e != nil || len(stale.Items) != 1 || stale.Items[0].Match.Title != nil || stale.Items[0].Title == title.Title {
		t.Fatal(stale, e)
	}
	detail, e := s.MediaDetail(ctx, f.ID, expiry)
	if e != nil || detail.Match.CacheState != "expired" || detail.Match.Target.Selection != "manual" || detail.Match.Title != nil {
		t.Fatal(detail, e)
	}
	var person string
	s.reader.QueryRow(`SELECT person_id FROM title_credits LIMIT 1`).Scan(&person)
	if _, e = s.MetadataPerson(ctx, person, expiry); !errors.Is(e, model.ErrNotFound) {
		t.Fatal(e)
	}
	if count(t, s, "title_credits") != 2 {
		t.Fatal("expiry destroyed relationships")
	}
	// A late projection failure rolls back the media state and every counter.
	execSQL(t, s, `CREATE TRIGGER reject_projection BEFORE UPDATE ON catalog_entries BEGIN SELECT RAISE(ABORT,'projection fault'); END`)
	if e = s.MarkFileDeleted(ctx, f.ID); e == nil {
		t.Fatal("expected fault")
	}
	after, e := s.MediaDetail(ctx, f.ID, time.Now().UnixMilli())
	if e != nil || after.File.State != "available" {
		t.Fatal(after, e)
	}
	execSQL(t, s, `DROP TRIGGER reject_projection`)

}

type countingReader struct {
	queryReader
	queries []string
	args    [][]any
}

func (c *countingReader) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	c.queries = append(c.queries, q)
	c.args = append(c.args, args)
	return c.queryReader.QueryRowContext(ctx, q, args...)
}
func (c *countingReader) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.queries = append(c.queries, q)
	c.args = append(c.args, args)
	return c.queryReader.QueryContext(ctx, q, args...)
}
func TestMediaDetailConstantQueriesAndNoUnrelatedCredits(t *testing.T) {
	s := openTestStore(t)
	populateSynthetic(t, s, 1000, 1000, 3)
	// Structurally valid JSON but wrong credit type; reading this unrelated row
	// would fail json.Unmarshal. The target must not parse it.
	execSQL(t, s, `UPDATE title_credits SET characters='[42]' WHERE remote_title_id<>'remote-series-00000'`)
	q := &countingReader{queryReader: s.reader}
	d, e := readMediaDetail(context.Background(), q, "file-00000", 1790800000100)
	if e != nil || len(q.queries) > 8 || len(d.Match.Credits) != 3 {
		t.Fatal(len(q.queries), d, e)
	}
	for _, query := range q.queries {
		if strings.Contains(query, "title_credits") && !strings.Contains(query, "remote_title_id=?") {
			t.Fatal(query)
		}
	}
}

func TestCatalogSeriesRecentRescanAndTargetedDeletion(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l := newScanLibrary(t, s, "series")
	a := entry("节目/S01E01.webm", "a")
	a.Organization = model.Organization{Title: "节目", Key: "show", Season: 1, Episode: 1}
	b := entry("别的/S01E01.webm", "b")
	b.Organization = model.Organization{Title: "别的", Key: "other", Season: 1, Episode: 1}
	scan := beginScan(t, s, l, true)
	stage(t, s, scan, a, b)
	publish(t, s, scan)
	before := files(t, s, l)
	var original model.MediaFile
	for _, f := range before {
		if f.Relative == a.Relative {
			original = f
		}
	}
	// Set a known older first-collection time without relying on timer precision.
	execSQL(t, s, `UPDATE media_files SET added_at=1000`)
	c := entry("节目/S02E01.webm", "c")
	c.Organization = model.Organization{Title: "节目", Key: "show", Season: 2, Episode: 1}
	scan = beginScan(t, s, l, false)
	stage(t, s, scan, a, b, c)
	publish(t, s, scan)
	q := model.CatalogQuery{LibraryID: l.ID, View: "recent", Sort: "recent"}
	p := catalogPage(t, s, q)
	if p.Items[0].SeriesID != original.SeriesID || p.Items[0].AddedAt <= 1000 || p.Items[0].SeasonCount != 2 {
		t.Fatal(p)
	}
	f, e := s.MediaFile(ctx, original.ID)
	if e != nil || f.AddedAt != 1000 {
		t.Fatal("rescan refreshed original collection time", f, e)
	}
	// Reject any INSERT/DELETE or unrelated UPDATE to the projection; single file
	// deletion must touch only the file and its own series summary.
	execSQL(t, s, `CREATE TRIGGER reject_projection_insert BEFORE INSERT ON catalog_entries BEGIN SELECT RAISE(ABORT,'unrelated insert'); END`)
	execSQL(t, s, `CREATE TRIGGER reject_projection_delete BEFORE DELETE ON catalog_entries BEGIN SELECT RAISE(ABORT,'unrelated delete'); END`)
	execSQL(t, s, fmt.Sprintf(`CREATE TRIGGER reject_other_projection BEFORE UPDATE ON catalog_entries WHEN NEW.entry_key NOT IN ('media:%s','series:%s') BEGIN SELECT RAISE(ABORT,'unrelated update'); END`, original.ID, original.SeriesID))
	if e = s.MarkFileDeleted(ctx, original.ID); e != nil {
		t.Fatal(e)
	}
	d, e := s.SeriesDetail(ctx, original.SeriesID, -1, 1, 0, time.Now().UnixMilli())
	if e != nil || !d.More || len(d.Seasons) != 1 || d.Card.AvailableFileCount != 1 {
		t.Fatal(d, e)
	}
	next, e := s.SeriesDetail(ctx, original.SeriesID, d.Seasons[0].Number, 1, d.Revision, time.Now().UnixMilli())
	if e != nil || next.More || next.Seasons[0].Number != 2 {
		t.Fatal(next, e)
	}
}
func TestCatalogReadWriteSnapshotsAndProgressRevision(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l := newScanLibrary(t, s, "movie")
	scan := beginScan(t, s, l, true)
	for i := 0; i < 6; i++ {
		stage(t, s, scan, entry(fmt.Sprintf("%d.webm", i), fmt.Sprint(i)))
	}
	publish(t, s, scan)
	id := files(t, s, l)[0].ID
	q := model.CatalogQuery{LibraryID: l.ID, View: "continue", Sort: "progress"}
	if _, e := s.SaveProgress(ctx, id, model.Progress{Position: 1, Duration: 60}); e != nil {
		t.Fatal(e)
	}
	p := catalogPage(t, s, q)
	if _, e := s.SaveProgress(ctx, id, model.Progress{Position: 2, Duration: 60}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Catalog(ctx, q, model.CatalogPosition{}, 1, p.Revision, time.Now().UnixMilli()); !errors.Is(e, model.ErrCursor) {
		t.Fatal("continue cursor survived progress change", e)
	}
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if n == 0 {
					if _, e := s.Favorite(ctx, "media", id, i%2 == 0); e != nil {
						t.Error(e)
					}
					if _, e := s.SaveProgress(ctx, id, model.Progress{Position: float64(i), Duration: 60}); e != nil {
						t.Error(e)
					}
				} else {
					q := model.CatalogQuery{View: "favorites", Sort: "favorite"}
					g, e := s.CatalogGroups(ctx, q, model.CatalogPosition{}, 50, 0, time.Now().UnixMilli())
					if e != nil {
						t.Error(e)
					}
					for _, group := range g.Items {
						if group.Total != 1 {
							t.Error("inconsistent group snapshot")
						}
					}
					if _, e := s.Home(ctx, "", 0, time.Now().UnixMilli()); e != nil {
						t.Error(e)
					}
				}
			}
		}(n)
	}
	wg.Wait()
}

// Regression for the measured ~1s global recent query after bulk metadata
// refresh: no unused relationship may be left for SQLite to (sometimes fail to)
// eliminate. End-to-end timings for that exact refreshed fixture live in the
// opt-in HTTP benchmark, not flaky wall-clock assertions under -race.
func TestCatalogEventSelectionOmitsUnusedRelations(t *testing.T) {
	for _, v := range []struct{ view, order string }{{"recent", "recent"}, {"favorites", "favorite"}, {"continue", "progress"}} {
		q := model.CatalogQuery{View: v.view, Sort: v.order}
		query, args := catalogSelection(q, time.Now().UnixMilli())
		if strings.Contains(query, "metadata_snapshots") || strings.Contains(query, "metadata_bindings") || len(args) != 0 {
			t.Fatal("unused metadata join can become an expiry-index nested loop", query, args)
		}
		q.Query = "标题"
		query, args = catalogSelection(q, time.Now().UnixMilli())
		if !strings.Contains(query, "metadata_snapshots") || len(args) != 4 {
			t.Fatal("search must still consult fresh titles", query, args)
		}
	}
}
