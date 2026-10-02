package store

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"reelnest/internal/model"
)

func newScanLibrary(t *testing.T, s *Store, kind string) model.Library {
	t.Helper()
	l, e := s.CreateLibrary(context.Background(), model.Library{ID: model.NewID(), Name: "同名库", Path: "/synthetic/" + model.NewID(), Identity: model.NewID(), Kind: kind, Sources: []string{}})
	if e != nil {
		t.Fatal(e)
	}
	return l
}
func beginScan(t *testing.T, s *Store, l model.Library, first bool) model.Scan {
	t.Helper()
	if !first {
		if _, e := s.QueueScan(context.Background(), l.ID); e != nil {
			t.Fatal(e)
		}
	}
	scan, e := s.ClaimScan(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	return scan
}
func stage(t *testing.T, s *Store, scan model.Scan, entries ...model.ScanEntry) {
	t.Helper()
	if e := s.StageScan(context.Background(), scan.ID, entries); e != nil {
		t.Fatal(e)
	}
}
func entry(path, fingerprint string) model.ScanEntry {
	return model.ScanEntry{Relative: path, Fingerprint: fingerprint, Size: 10, ModifiedAt: 1000}
}
func publish(t *testing.T, s *Store, scan model.Scan) {
	t.Helper()
	if e := s.CommitScan(context.Background(), scan.ID); e != nil {
		t.Fatal(e)
	}
}
func files(t *testing.T, s *Store, l model.Library) []model.MediaFile {
	t.Helper()
	f, e := s.MediaFiles(context.Background(), l.ID, "", 256)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func TestScanPublishesAtomicallyPreservesIdentityAndHouseholdState(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l := newScanLibrary(t, s, "movie")
	scan := beginScan(t, s, l, true)
	stage(t, s, scan, entry("movie.mp4", "version1"))
	if len(files(t, s, l)) != 0 {
		t.Fatal("staging visible")
	}
	publish(t, s, scan)
	before := files(t, s, l)[0]
	p, e := s.SaveProgress(ctx, before.ID, model.Progress{Position: 12, Duration: 60})
	if e != nil {
		t.Fatal(e)
	}
	fav, e := s.Favorite(ctx, "media", before.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	scan = beginScan(t, s, l, false)
	stage(t, s, scan, entry("movie.mp4", "version1"))
	publish(t, s, scan)
	after := files(t, s, l)[0]
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("stable identity changed: %+v %+v", before, after)
	}
	progress, e := s.Progress(ctx, after.ID)
	if e != nil || !reflect.DeepEqual(&p, progress) {
		t.Fatal("progress lost", progress, e)
	}
	var at int64
	if e = s.reader.QueryRow("SELECT favorited_at FROM media_favorites WHERE media_file_id=?", after.ID).Scan(&at); e != nil || at != *fav {
		t.Fatal("favorite lost", at, e)
	}
	// Completed metadata jobs may be retired by stage 03. An unchanged scan
	// must not recreate them or refresh the existing target's identity.
	execSQL(t, s, "DELETE FROM metadata_jobs WHERE subject_id=?", before.SubjectID)
	scan = beginScan(t, s, l, false)
	stage(t, s, scan, entry("movie.mp4", "version1"))
	publish(t, s, scan)
	if count(t, s, "metadata_jobs") != 0 {
		t.Fatal("unchanged subject was requeued")
	}
	// A genuine SQLite trigger fails late, after files and subjects have changed.
	execSQL(t, s, `CREATE TEMP TRIGGER fail_publication BEFORE UPDATE OF state ON scan_runs WHEN NEW.state='completed' BEGIN SELECT RAISE(ABORT,'publication fault'); END`)
	scan = beginScan(t, s, l, false)
	stage(t, s, scan, entry("movie.mp4", "version2"), entry("new.mp4", "new"))
	if e = s.CommitScan(ctx, scan.ID); e == nil {
		t.Fatal("expected SQLite failure")
	}
	if got := files(t, s, l); len(got) != 1 || !reflect.DeepEqual(got[0], before) {
		t.Fatalf("partial publication: %+v", got)
	}
	if count(t, s, "metadata_subjects") != 1 || count(t, s, "watch_progress") != 1 || count(t, s, "media_favorites") != 1 {
		t.Fatal("dependencies did not roll back")
	}
	observed, e := s.Scan(ctx, scan.ID)
	if e != nil || observed.State != "running" {
		t.Fatal("scan completion escaped rollback", observed, e)
	}
	execSQL(t, s, "DROP TRIGGER fail_publication")
	publish(t, s, scan)
	got := files(t, s, l)
	if len(got) != 2 || count(t, s, "watch_progress") != 0 || count(t, s, "media_favorites") != 0 {
		t.Fatal("new file version inherited state", got)
	}
	for _, f := range got {
		if f.ID == before.ID || f.SubjectID == before.SubjectID {
			t.Fatal("old file identity survived replacement")
		}
	}
	scan = beginScan(t, s, l, false)
	publish(t, s, scan)
	for _, table := range []string{"media_files", "metadata_subjects", "metadata_jobs", "catalog_entries", "scan_entries", "scan_organization"} {
		if count(t, s, table) != 0 {
			t.Fatal("incomplete cleanup", table)
		}
	}
}
func TestOrganizationSubjectChangesDoNotSpreadManualSelections(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l := newScanLibrary(t, s, "series")
	scan := beginScan(t, s, l, true)
	f := entry("Show/01.mp4", "v1")
	f.Organization.Reason = "unknown"
	stage(t, s, scan, f)
	publish(t, s, scan)
	before := files(t, s, l)[0]
	execSQL(t, s, `INSERT INTO remote_titles VALUES('remote','tmdb','tmdb','series','AbC'); INSERT INTO metadata_bindings(subject_id,expected_kind,remote_title_id,selection,revision) VALUES(?,'series','remote','manual',1)`, before.SubjectID)
	f.Organization = model.Organization{Title: "Show", Key: "show", Season: 1, Episode: 1}
	scan = beginScan(t, s, l, false)
	stage(t, s, scan, f)
	publish(t, s, scan)
	after := files(t, s, l)[0]
	if after.ID != before.ID || after.SeriesID == "" || after.SubjectID == before.SubjectID {
		t.Fatal(after)
	}
	if count(t, s, "metadata_bindings") != 0 {
		t.Fatal("file manual selection spread to series")
	}
	if _, e := s.Favorite(ctx, "series", after.SeriesID, true); e != nil {
		t.Fatal(e)
	}
	// A second file version at the same episode belongs to the same series.
	g := entry("Show/01-4K.mp4", "v2")
	g.Organization = f.Organization
	scan = beginScan(t, s, l, false)
	stage(t, s, scan, f, g)
	publish(t, s, scan)
	if count(t, s, "series") != 1 || count(t, s, "metadata_subjects") != 1 || count(t, s, "file_organization") != 2 {
		t.Fatal("multiple versions merged or subjects duplicated")
	}
	var episodes, fileCount int
	if e := s.reader.QueryRow("SELECT episode_count,file_count FROM catalog_entries").Scan(&episodes, &fileCount); e != nil || episodes != 1 || fileCount != 2 {
		t.Fatal(episodes, fileCount, e)
	}
	scan = beginScan(t, s, l, false)
	publish(t, s, scan)
	for _, table := range []string{"series", "series_favorites", "file_organization", "metadata_subjects", "metadata_jobs"} {
		if count(t, s, table) != 0 {
			t.Fatal("empty series retained", table)
		}
	}
}
func TestFailedInterruptedAndIncompleteStagingNeverPublishes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l := newScanLibrary(t, s, "movie")
	scan := beginScan(t, s, l, true)
	stage(t, s, scan, entry("old.mp4", "old"))
	publish(t, s, scan)
	before := files(t, s, l)
	for _, interrupted := range []bool{false, true} {
		scan = beginScan(t, s, l, false)
		stage(t, s, scan, entry("new.mp4", "new"))
		if e := s.FailScan(ctx, scan.ID, interrupted); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(before, files(t, s, l)) || count(t, s, "scan_entries") != 0 {
			t.Fatal("failed scan mutated library")
		}
	}
	scan = beginScan(t, s, l, false)
	stage(t, s, scan, entry("broken.mp4", "broken"))
	execSQL(t, s, "DELETE FROM scan_organization WHERE scan_id=?", scan.ID)
	if e := s.CommitScan(ctx, scan.ID); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("incomplete organization accepted", e)
	}
	if !reflect.DeepEqual(before, files(t, s, l)) {
		t.Fatal("partial commit")
	}
	// Losing BOTH staging rows also must not turn an incomplete manifest into a
	// valid empty scan: the discovered counter is part of publication validation.
	execSQL(t, s, "DELETE FROM scan_entries WHERE scan_id=?", scan.ID)
	if e := s.CommitScan(ctx, scan.ID); !errors.Is(e, model.ErrInvalid) {
		t.Fatal("lost staging published", e)
	}
	if e := s.InterruptScans(ctx); e != nil {
		t.Fatal(e)
	}
	got, e := s.Scan(ctx, scan.ID)
	if e != nil || got.State != "interrupted" || count(t, s, "scan_entries") != 0 {
		t.Fatal(got, e)
	}
}
func TestLibraryTransactionsOverlapSourcesAndRevision(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l := newScanLibrary(t, s, "series")
	if _, e := s.QueueScan(ctx, l.ID); !errors.Is(e, model.ErrBusy) {
		t.Fatal("duplicate queue", e)
	}
	for _, path := range []string{l.Path, l.Path + "/nested", "/synthetic"} {
		other := l
		other.ID = model.NewID()
		other.Identity = model.NewID()
		other.Path = path
		if _, e := s.CreateLibrary(ctx, other); !errors.Is(e, model.ErrOverlap) {
			t.Fatal("overlap", path, e)
		}
	}
	other := l
	other.ID = model.NewID()
	other.Path += "-other"
	other.Identity = model.NewID()
	if _, e := s.CreateLibrary(ctx, other); e != nil {
		t.Fatal("same-name libraries merged", e)
	}
	// Clear all initial scans so helper claims the intended one below.
	if e := s.InterruptScans(ctx); e != nil {
		t.Fatal(e)
	}
	scan := beginScan(t, s, l, false)
	f := entry("Show/01.mp4", "v1")
	f.Organization = model.Organization{Title: "Show", Key: "show", Season: 1, Episode: 1}
	stage(t, s, scan, f)
	publish(t, s, scan)
	subject := files(t, s, l)[0].SubjectID
	sources := []string{"tvdb", "tmdb"}
	name := "Renamed"
	updated, e := s.UpdateLibrary(ctx, l.ID, l.Revision, &name, &sources)
	if e != nil {
		t.Fatal(e)
	}
	if updated.PolicyRevision != 2 || updated.Revision != 2 || !reflect.DeepEqual(updated.Sources, sources) {
		t.Fatal(updated)
	}
	var generation int
	if e = s.reader.QueryRow("SELECT requested_generation FROM metadata_jobs WHERE subject_id=?", subject).Scan(&generation); e != nil || generation != 2 {
		t.Fatal("policy did not requeue", generation, e)
	}
	if _, e = s.UpdateLibrary(ctx, l.ID, 1, &name, nil); !errors.Is(e, model.ErrConflict) {
		t.Fatal(e)
	}
	execSQL(t, s, `INSERT INTO remote_titles VALUES('remote','tmdb','tmdb','series','42');INSERT INTO metadata_bindings(subject_id,expected_kind,remote_title_id,selection,revision) VALUES(?,'series','remote','manual',1)`, subject)
	sources = []string{}
	if _, e = s.UpdateLibrary(ctx, l.ID, 2, nil, &sources); e != nil {
		t.Fatal(e)
	}
	if e = s.reader.QueryRow("SELECT requested_generation FROM metadata_jobs WHERE subject_id=?", subject).Scan(&generation); e != nil || generation != 2 {
		t.Fatal("manual selection requeued", generation, e)
	}
	invalid := []string{"tmdb", "tmdb"}
	if _, e = s.UpdateLibrary(ctx, l.ID, 3, &name, &invalid); e == nil {
		t.Fatal("invalid sources committed")
	}
	after, e := s.Library(ctx, l.ID)
	if e != nil || after.Revision != 3 || len(after.Sources) != 0 {
		t.Fatal("library/source partial commit", after, e)
	}
}
func TestProgressValidationAndDeletedState(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	l := newScanLibrary(t, s, "movie")
	scan := beginScan(t, s, l, true)
	stage(t, s, scan, entry("movie.mp4", "v"))
	publish(t, s, scan)
	f := files(t, s, l)[0]
	for _, p := range []model.Progress{{Position: math.NaN(), Duration: 10}, {Position: 1, Duration: math.Inf(1)}, {Position: 11, Duration: 10}, {Position: 5, Duration: 10, Completed: true}} {
		if _, e := s.SaveProgress(ctx, f.ID, p); !errors.Is(e, model.ErrInvalid) {
			t.Fatal(p, e)
		}
	}
	if e := s.MarkFileDeleted(ctx, f.ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.SaveProgress(ctx, f.ID, model.Progress{Position: 1, Duration: 10}); !errors.Is(e, model.ErrConflict) {
		t.Fatal("progress on deleted", e)
	}
	got, e := s.MediaFile(ctx, f.ID)
	if e != nil || got.State != "deleted" {
		t.Fatal(got, e)
	}
}

var scanFixtureFiles = flag.Int("scan-fixture-files", 1000, "bounded scan publication fixture size (1..50000)")

func TestLargeScanUsesBoundedStagingPages(t *testing.T) {
	if testing.Short() {
		t.Skip("large staged snapshot")
	}
	s := openTestStore(t)
	l := newScanLibrary(t, s, "series")
	scan := beginScan(t, s, l, true)
	total := *scanFixtureFiles
	if total < 1 || total > 50000 {
		t.Fatal("fixture size outside 1..50000")
	}
	started := time.Now()
	for start := 0; start < total; start += 256 {
		batch := []model.ScanEntry{}
		for n := start; n < min(start+256, total); n++ {
			f := entry(fmt.Sprintf("Show/%04d.mp4", n+1), fmt.Sprint(n))
			f.Organization = model.Organization{Key: "show", Title: "Show", Season: n/100 + 1, Episode: n%100 + 1}
			batch = append(batch, f)
		}
		stage(t, s, scan, batch...)
	}
	staged := time.Since(started)
	started = time.Now()
	publish(t, s, scan)
	t.Logf("files=%d batch_limit=256 staging=%s publication=%s", total, staged, time.Since(started))
	if count(t, s, "media_files") != total || count(t, s, "scan_entries") != 0 {
		t.Fatal("large snapshot missing files")
	}
}
