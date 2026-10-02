package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"reelnest/internal/model"
)

func settingsWrite(t *testing.T, s *Store, source, base string, expected int64) model.StoredProviderSettings {
	t.Helper()
	p, err := s.SaveProviderSettings(context.Background(), model.StoredProviderSettings{Source: source, BaseURL: base, Revision: expected + 1, Secrets: make([]byte, 28)}, expected)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func settingsLibrary(t *testing.T, s *Store, source string, names ...string) map[string]model.MediaFile {
	t.Helper()
	l := newScanLibrary(t, s, "movie")
	sources := []string{source}
	if _, err := s.UpdateLibrary(context.Background(), l.ID, 1, nil, &sources); err != nil {
		t.Fatal(err)
	}
	scan := beginScan(t, s, l, true)
	for _, name := range names {
		stage(t, s, scan, entry(name, "v1"))
	}
	publish(t, s, scan)
	result := map[string]model.MediaFile{}
	for _, f := range files(t, s, l) {
		result[f.Relative] = f
	}
	return result
}
func manualBinding(t *testing.T, s *Store, f model.MediaFile, catalog, namespace string) string {
	t.Helper()
	id := model.NewID()
	execSQL(t, s, `INSERT INTO remote_titles VALUES(?,?,?,'movie',?)`, id, catalog, namespace, "Opaque-ID-001")
	execSQL(t, s, `INSERT INTO metadata_bindings(subject_id,expected_kind,remote_title_id,selection,revision) VALUES(?,'movie',?,'manual',7)`, f.SubjectID, id)
	now := time.Now().UnixMilli()
	execSQL(t, s, `INSERT INTO metadata_snapshots(remote_title_id,title,original_title,overview,source_url,fetched_at,expires_at,revision,credits_state,artwork_state)
 VALUES(?,'Title','Original','Overview','',?,?,1,'ready','ready')`, id, now, now+86400000)
	return id
}

type taskState struct {
	Generation, Attempts    int64
	State                   string
	Started, Finished, Next *int64
	Error                   *string
}

func task(t *testing.T, s *Store, subject string) taskState {
	t.Helper()
	var got taskState
	if err := s.reader.QueryRow(`SELECT requested_generation,attempts,state,started_at,finished_at,next_attempt_at,error_code FROM metadata_jobs WHERE subject_id=?`, subject).
		Scan(&got.Generation, &got.Attempts, &got.State, &got.Started, &got.Finished, &got.Next, &got.Error); err != nil {
		t.Fatal(err)
	}
	return got
}
func runningJobs(t *testing.T, s *Store) {
	t.Helper()
	execSQL(t, s, `UPDATE metadata_jobs SET state='running',claimed_generation=requested_generation,attempts=2,
 subject_revision=1,binding_revision=0,policy_revision=1,started_at=1,next_attempt_at=9999999999999,error_code='PROVIDER_UNAVAILABLE'`)
}

func TestProviderSaveRequeuesOnlyAffectedLiveSubjects(t *testing.T) {
	s := openTestStore(t)
	a := settingsLibrary(t, s, "tmdb", "automatic.mp4", "manual-other.mp4", "deleted.mp4")
	b := settingsLibrary(t, s, "tvdb", "automatic.mp4", "manual-tmdb.mp4")
	tmdbTitle := manualBinding(t, s, b["manual-tmdb.mp4"], "tmdb", "tmdb")
	tvdbTitle := manualBinding(t, s, a["manual-other.mp4"], "tvdb", "tvdb")
	if err := s.MarkFileDeleted(context.Background(), a["deleted.mp4"].ID); err != nil {
		t.Fatal(err)
	}
	runningJobs(t, s)
	before := map[string]taskState{}
	for _, group := range []map[string]model.MediaFile{a, b} {
		for _, f := range group {
			before[f.SubjectID] = task(t, s, f.SubjectID)
		}
	}
	settingsWrite(t, s, "tmdb", "https://api.themoviedb.org/3", 1)
	for _, f := range []model.MediaFile{a["automatic.mp4"], b["manual-tmdb.mp4"]} {
		got := task(t, s, f.SubjectID)
		if got.Generation != before[f.SubjectID].Generation+1 || got.State != "queued" || got.Attempts != 0 || got.Started != nil || got.Finished != nil || got.Next != nil || got.Error != nil {
			t.Fatal("affected target did not get a fresh generation", got)
		}
	}
	for _, f := range []model.MediaFile{a["manual-other.mp4"], a["deleted.mp4"], b["automatic.mp4"]} {
		if got := task(t, s, f.SubjectID); !reflect.DeepEqual(got, before[f.SubjectID]) {
			t.Fatal("unrelated/deleted target requeued", f.Relative, got)
		}
	}
	for _, id := range []string{tmdbTitle, tvdbTitle} {
		var selection string
		var revision, expires int64
		if err := s.reader.QueryRow(`SELECT b.selection,b.revision,s.expires_at FROM metadata_bindings b JOIN metadata_snapshots s ON s.remote_title_id=b.remote_title_id WHERE b.remote_title_id=?`, id).Scan(&selection, &revision, &expires); err != nil || selection != "manual" || revision != 7 || expires <= time.Now().UnixMilli() {
			t.Fatal("save mutated a manual choice or its snapshot", err)
		}
	}
}

func TestMetaTubeCatalogChangePreservesOldBindingsAndSupersedesJobs(t *testing.T) {
	s := openTestStore(t)
	old := settingsWrite(t, s, "metatube", "http://first.invalid/v1", 1)
	a := settingsLibrary(t, s, "metatube", "automatic.mp4", "manual.mp4")
	title := manualBinding(t, s, a["manual.mp4"], old.CatalogID, "provider")
	runningJobs(t, s)
	before := task(t, s, a["manual.mp4"].SubjectID)
	next := settingsWrite(t, s, "metatube", "http://second.invalid/v1", 2)
	if next.CatalogID == old.CatalogID {
		t.Fatal("address reused old space")
	}
	got := task(t, s, a["manual.mp4"].SubjectID)
	if got.Generation != before.Generation+1 || got.State != "superseded" || got.Finished == nil || got.Next != nil || got.Error != nil {
		t.Fatal("old-space job still active", got)
	}
	if got := task(t, s, a["automatic.mp4"].SubjectID); got.State != "queued" || got.Generation != 2 {
		t.Fatal("automatic target not requeued", got)
	}
	var catalog, selected, remoteID string
	if err := s.reader.QueryRow(`SELECT r.catalog_id,b.remote_title_id,r.remote_id FROM metadata_bindings b JOIN remote_titles r ON r.id=b.remote_title_id WHERE b.subject_id=?`, a["manual.mp4"].SubjectID).Scan(&catalog, &selected, &remoteID); err != nil || catalog != old.CatalogID || selected != title || remoteID != "Opaque-ID-001" {
		t.Fatal("old selection was translated to new server", err)
	}
	// Rotating a token keeps catalog identity; revisiting the old address resumes
	// the original manual selection without silently changing its binding.
	rotated := settingsWrite(t, s, "metatube", "http://second.invalid/v1", 3)
	if rotated.CatalogID != next.CatalogID {
		t.Fatal("rotation changed catalog")
	}
	back := settingsWrite(t, s, "metatube", "http://first.invalid/v1", 4)
	if back.CatalogID != old.CatalogID || task(t, s, a["manual.mp4"].SubjectID).State != "queued" {
		t.Fatal("known catalog lost stable identity")
	}
}

func TestProviderSaveCASAndJobFailureAreAtomic(t *testing.T) {
	s := openTestStore(t)
	a := settingsLibrary(t, s, "metatube", "automatic.mp4")
	runningJobs(t, s)
	before := task(t, s, a["automatic.mp4"].SubjectID)
	execSQL(t, s, `CREATE TRIGGER refuse_requeue BEFORE UPDATE ON metadata_jobs BEGIN SELECT RAISE(ABORT,'test requeue failure'); END`)
	input := model.StoredProviderSettings{Source: "metatube", BaseURL: "http://first.invalid/v1", Revision: 2, Secrets: make([]byte, 28)}
	if _, err := s.SaveProviderSettings(context.Background(), input, 1); err == nil {
		t.Fatal("failed requeue did not roll back setting save")
	}
	if count(t, s, "provider_catalogs") != 2 || !reflect.DeepEqual(before, task(t, s, a["automatic.mp4"].SubjectID)) {
		t.Fatal("failed write leaked catalog or job state")
	}
	execSQL(t, s, "DROP TRIGGER refuse_requeue")
	settingsWrite(t, s, "metatube", input.BaseURL, 1)
	if _, err := s.SaveProviderSettings(context.Background(), input, 1); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale write accepted", err)
	}
}
