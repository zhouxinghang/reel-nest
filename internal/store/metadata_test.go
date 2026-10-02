package store

import (
	"context"
	"errors"
	"fmt"
	"reelnest/internal/model"
	"testing"
	"time"
)

func metadataFixture(t *testing.T) (*Store, model.Library, model.MetadataTarget, model.Title) {
	t.Helper()
	s := openTestStore(t)
	l := newScanLibrary(t, s, "movie")
	if _, e := s.writer.Exec(`INSERT INTO library_sources VALUES(?,'tmdb',0)`, l.ID); e != nil {
		t.Fatal(e)
	}
	scan := beginScan(t, s, l, true)
	stage(t, s, scan, entry("Movie (2024).mp4", "one"))
	publish(t, s, scan)
	target, e := s.ClaimMetadata(context.Background(), []model.ProviderVersion{{Source: "tmdb", CatalogID: "tmdb", Revision: 1}}, time.Now().UnixMilli())
	if e != nil {
		t.Fatal(e)
	}
	title := model.Title{Ref: model.RemoteRef{Source: "tmdb", CatalogID: "tmdb", Namespace: "tmdb", Kind: "movie", RemoteID: "42", ProviderRevision: 1}, Title: "电影", OriginalTitle: "Movie", Overview: "简介", FetchedAt: time.Now().UnixMilli(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), CreditsState: "ready", ArtworkState: "ready", Credits: []model.Credit{{Key: "7", Name: "同名演员", Role: "actor", Characters: []string{"角色"}, Photo: "/person.jpg"}, {Name: "无ID演员", Role: "actor"}}, Artwork: map[string]string{"primary": "/poster.jpg", "thumb": "/thumb.jpg", "backdrop": "/back.jpg"}}
	return s, l, target, title
}
func finishTitle(t *testing.T, s *Store, target model.MetadataTarget, title model.Title) {
	t.Helper()
	if e := s.CompleteMetadata(context.Background(), model.MetadataResult{Target: target, Title: &title, Selection: "manual"}, time.Now().UnixMilli()); e != nil {
		t.Fatal(e)
	}
}
func TestMetadataCASRejectsAllStaleResults(t *testing.T) {
	for _, failure := range []bool{false, true} {
		for _, change := range []string{"subject", "binding", "policy", "provider", "catalog", "generation", "remove", "unavailable"} {
			t.Run(fmt.Sprintf("%s/failure=%v", change, failure), func(t *testing.T) {
				s, _, target, title := metadataFixture(t)
				ctx := context.Background()
				var q string
				var args []any
				switch change {
				case "subject":
					q = `UPDATE metadata_subjects SET revision=revision+1 WHERE id=?`
					args = []any{target.ID}
				case "binding":
					if _, e := s.writer.Exec(`INSERT INTO remote_titles VALUES('other','tmdb','tmdb','movie','9')`); e != nil {
						t.Fatal(e)
					}
					q = `INSERT INTO metadata_bindings(subject_id,expected_kind,remote_title_id,selection,revision) VALUES(?,'movie','other','manual',1)`
					args = []any{target.ID}
				case "policy":
					q = `UPDATE libraries SET policy_revision=policy_revision+1 WHERE id=?`
					args = []any{target.LibraryID}
				case "provider":
					q = `UPDATE provider_settings SET revision=revision+1 WHERE source='tmdb'`
				case "catalog":
					target.Providers[0].CatalogID = "other"
				case "generation":
					q = `UPDATE metadata_jobs SET requested_generation=requested_generation+1,state='queued',attempts=0 WHERE subject_id=?`
					args = []any{target.ID}
				case "remove":
					q = `DELETE FROM metadata_subjects WHERE id=?`
					args = []any{target.ID}
				case "unavailable":
					q = `UPDATE media_files SET state='deleted' WHERE id=(SELECT media_file_id FROM metadata_subjects WHERE id=?)`
					args = []any{target.ID}
				}
				if q != "" {
					if _, e := s.writer.Exec(q, args...); e != nil {
						t.Fatal(e)
					}
				}
				result := model.MetadataResult{Target: target, Title: &title}
				if failure {
					result.Title = nil
					result.ErrorCode = "PROVIDER_UNAVAILABLE"
				}
				if e := s.CompleteMetadata(ctx, result, time.Now().UnixMilli()); !errors.Is(e, model.ErrConflict) {
					t.Fatal(e)
				}
				if count(t, s, "metadata_snapshots") != 0 {
					t.Fatal("stale content committed")
				}
				if change == "generation" {
					var state string
					if e := s.reader.QueryRow(`SELECT state FROM metadata_jobs WHERE subject_id=?`, target.ID).Scan(&state); e != nil || state != "queued" {
						t.Fatal("old result cleared queued work", state, e)
					}
				}
				if change == "binding" {
					var state string
					s.reader.QueryRow(`SELECT attempt_state FROM metadata_bindings WHERE subject_id=?`, target.ID).Scan(&state)
					if state != "never" {
						t.Fatal("old failure overwrote new binding", state)
					}
				}
			})
		}
	}
}
func TestMetadataAtomicCreditsFailureAndOptionalTTL(t *testing.T) {
	s, _, target, title := metadataFixture(t)
	ctx := context.Background()
	if _, e := s.writer.Exec(`CREATE TRIGGER reject_credit AFTER INSERT ON title_credits BEGIN UPDATE title_credits SET person_id='nonexistent-fixture-person' WHERE id=NEW.id; END`); e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteMetadata(ctx, model.MetadataResult{Target: target, Title: &title}, time.Now().UnixMilli()); e == nil {
		t.Fatal("injection did not fail")
	}
	for _, table := range []string{"metadata_snapshots", "remote_titles", "metadata_bindings", "people", "assets"} {
		if count(t, s, table) != 0 {
			t.Fatal("partial write", table)
		}
	}
	s.writer.Exec(`DROP TRIGGER reject_credit`)
	finishTitle(t, s, target, title)
	m, e := s.MetadataMatch(ctx, target.ID, time.Now().UnixMilli())
	if e != nil || len(m.Credits) != 2 || len(m.Artwork) != 3 {
		t.Fatal(m, e)
	}
	if _, e = s.RefreshMetadata(ctx, "", time.Now().UnixMilli()); e != nil {
		t.Fatal(e)
	}
	next, e := s.ClaimMetadata(ctx, target.Providers, time.Now().UnixMilli())
	if e != nil {
		t.Fatal(e)
	}
	supplement := *m.Title
	supplement.Partial = true
	supplement.UpdateArtwork = true
	supplement.ArtworkState = "ready"
	supplement.Artwork = map[string]string{"thumb": "/new.jpg"}
	supplement.FetchedAt++
	supplement.ExpiresAt += 10000
	finishTitle(t, s, next, supplement)
	after, e := s.MetadataMatch(ctx, target.ID, time.Now().UnixMilli())
	if e != nil || after.Title.FetchedAt != title.FetchedAt || after.Title.ExpiresAt != title.ExpiresAt || len(after.Credits) != 2 || after.Artwork["primary"] != m.Artwork["primary"] || after.Artwork["thumb"] == m.Artwork["thumb"] {
		t.Fatal("optional retry changed original TTL/credits", after, e)
	}
	// An optional supplement must also reject an old snapshot revision.
	s.RefreshMetadata(ctx, "", time.Now().UnixMilli())
	next, e = s.ClaimMetadata(ctx, target.Providers, time.Now().UnixMilli())
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteMetadata(ctx, model.MetadataResult{Target: next, Title: &supplement, Selection: "manual"}, time.Now().UnixMilli()); !errors.Is(e, model.ErrConflict) {
		t.Fatal(e)
	}
	expired, e := s.MetadataMatch(ctx, target.ID, title.ExpiresAt)
	if e != nil || expired.CacheState != "expired" || expired.Title != nil || expired.Target.Selection != "manual" || len(expired.Credits) != 0 {
		t.Fatal(expired, e)
	}
	if _, e = s.MetadataPerson(ctx, m.Credits[0].PersonID, title.ExpiresAt); !errors.Is(e, model.ErrNotFound) {
		t.Fatal("expired person", e)
	}
	if _, e = s.MetadataAsset(ctx, m.Artwork["primary"], title.ExpiresAt); !errors.Is(e, model.ErrNotFound) {
		t.Fatal("expired image", e)
	}
}
func TestMetadataRetriesGenerationAndRecovery(t *testing.T) {
	s, _, target, _ := metadataFixture(t)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	for attempt := 1; attempt <= 3; attempt++ {
		if e := s.CompleteMetadata(ctx, model.MetadataResult{Target: target, ErrorCode: "PROVIDER_UNAVAILABLE", RetryAt: now + 10}, now); e != nil {
			t.Fatal(e)
		}
		if _, e := s.ClaimMetadata(ctx, target.Providers, now); !errors.Is(e, model.ErrNotFound) {
			t.Fatal("retry before due", e)
		}
		if attempt < 3 {
			var e error
			target, e = s.ClaimMetadata(ctx, target.Providers, now+11)
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	if _, e := s.ClaimMetadata(ctx, target.Providers, now+100); !errors.Is(e, model.ErrNotFound) {
		t.Fatal("unbounded retry", e)
	}
	s.RefreshMetadata(ctx, "", now+200)
	next, e := s.ClaimMetadata(ctx, target.Providers, now+200)
	if e != nil || next.Generation <= target.Generation {
		t.Fatal(next, e)
	}
	if e = s.RecoverMetadata(ctx); e != nil {
		t.Fatal(e)
	}
	recovered, e := s.ClaimMetadata(ctx, target.Providers, now+201)
	if e != nil || recovered.Generation != next.Generation {
		t.Fatal(recovered, e)
	}
	var attempts int
	s.reader.QueryRow(`SELECT attempts FROM metadata_jobs WHERE subject_id=?`, target.ID).Scan(&attempts)
	if attempts != 2 {
		t.Fatal("restart refunded attempt", attempts)
	}
	status, e := s.MetadataStatus(ctx, "", "", 100, 0)
	if e != nil || status.Counts["running"] != 1 {
		t.Fatal(status, e)
	}
}
func TestMetadataAssetCASAndPeopleLocalScope(t *testing.T) {
	s, l, target, title := metadataFixture(t)
	ctx := context.Background()
	finishTitle(t, s, target, title)
	now := time.Now().UnixMilli()
	m, e := s.MetadataMatch(ctx, target.ID, now)
	if e != nil {
		t.Fatal(e)
	}
	person := m.Credits[0].PersonID
	works, _, _, e := s.MetadataPersonWorks(ctx, person, "", 100, 0, now)
	if e != nil || len(works) != 1 {
		t.Fatal(works, e)
	}
	a, e := s.MetadataAsset(ctx, m.Artwork["primary"], now)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteAsset(ctx, a, []byte{1, 2, 3}, "image/png", false, now); e != nil {
		t.Fatal(e)
	}
	a, e = s.MetadataAsset(ctx, a.ID, now)
	if e != nil || a.State != "ready" || *a.ExpiresAt != title.ExpiresAt {
		t.Fatal(a, e)
	}
	for _, mutate := range []string{`UPDATE assets SET revision=revision+1`, `UPDATE provider_settings SET revision=revision+1 WHERE source='tmdb'`, `UPDATE metadata_snapshots SET revision=revision+1`} {
		tx, e := s.writer.Begin()
		if e != nil {
			t.Fatal(e)
		}
		_, e = tx.Exec(mutate)
		if e != nil {
			t.Fatal(e)
		}
		tx.Commit()
		if e = s.CompleteAsset(ctx, a, []byte{9}, "image/png", false, now); !errors.Is(e, model.ErrConflict) {
			t.Fatal("stale image", e)
		}
		if e = s.CompleteAsset(ctx, a, nil, "", true, now); !errors.Is(e, model.ErrConflict) {
			t.Fatal("stale negative", e)
		}
	}
	// A second local copy shares reliable people and collapses to one work.
	scan := beginScan(t, s, l, false)
	stage(t, s, scan, entry("Movie (2024).mp4", "one"), entry("Other.mp4", "two"))
	publish(t, s, scan)
	var second string
	s.reader.QueryRow(`SELECT id FROM metadata_subjects WHERE id<>?`, target.ID).Scan(&second)
	if _, e = s.writer.Exec(`INSERT INTO metadata_bindings(subject_id,expected_kind,remote_title_id,selection,revision) VALUES(?,'movie',?,'manual',1)`, second, m.Target.RemoteTitleID); e != nil {
		t.Fatal(e)
	}
	var deletedID string
	if e = s.reader.QueryRow(`SELECT media_file_id FROM metadata_subjects WHERE id=?`, target.ID).Scan(&deletedID); e != nil {
		t.Fatal(e)
	}
	if e = s.MarkFileDeleted(ctx, deletedID); e != nil {
		t.Fatal(e)
	}
	works, _, _, e = s.MetadataPersonWorks(ctx, person, "", 100, 0, now)
	if e != nil || len(works) != 1 || works[0].Match.Target.ID != second {
		t.Fatal("representative must prefer available", works, e)
	}
	s.writer.Exec(`DELETE FROM metadata_bindings`)
	if _, e = s.MetadataPerson(ctx, person, now); !errors.Is(e, model.ErrNotFound) {
		t.Fatal("orphan person exposed", e)
	}
	if _, e = s.MetadataAsset(ctx, a.ID, now); !errors.Is(e, model.ErrNotFound) {
		t.Fatal("orphan asset exposed", e)
	}
}

func TestMetadataPeopleSeriesDoNotRepeatEpisodes(t *testing.T) {
	s := openTestStore(t)
	populateSynthetic(t, s, 100, 20, 3)
	works, _, _, e := s.MetadataPersonWorks(context.Background(), "person-000", "", 100, 0, 1790800001000)
	if e != nil || len(works) != 20 {
		t.Fatal(len(works), e)
	}
	for _, w := range works {
		if w.SeriesID == "" || w.MediaFileID != "" || w.FileCount != 5 {
			t.Fatal(w)
		}
	}
}
