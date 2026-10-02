package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reelnest/internal/model"
)

const localPerson = ` FROM title_credits c JOIN people p ON p.id=c.person_id JOIN provider_catalogs pc ON pc.id=p.catalog_id
 JOIN metadata_snapshots sn ON sn.remote_title_id=c.remote_title_id AND sn.expires_at>?
 JOIN metadata_bindings b ON b.remote_title_id=c.remote_title_id JOIN metadata_subjects sub ON sub.id=b.subject_id
 JOIN catalog_entries e ON e.subject_id=sub.id AND (e.series_id IS NOT NULL OR NOT EXISTS(SELECT 1 FROM file_organization o WHERE o.media_file_id=e.media_file_id)) WHERE p.id=?`

func (s *Store) MetadataPerson(ctx context.Context, id string, now int64) (model.Person, error) {
	var p model.Person
	e := s.reader.QueryRowContext(ctx, `SELECT p.id,c.name,pc.source,p.catalog_id,(SELECT a.id FROM assets a WHERE a.credit_id=c.id)`+localPerson+` ORDER BY e.available_file_count>0 DESC,c.remote_title_id,c.role,c.ordinal,e.entry_key LIMIT 1`, now, id).Scan(&p.ID, &p.Name, &p.Source, &p.CatalogID, &p.PhotoAssetID)
	return p, notFound(e)
}

// PersonWorks is the bounded relationship foundation. Stage 04 can optimize its
// page assembly without changing current-binding/TTL/representative semantics.
func (s *Store) MetadataPersonWorks(ctx context.Context, id, after string, limit int, expected, now int64) ([]model.PersonWork, int64, int64, error) {
	works := []model.PersonWork{}
	if limit < 1 || limit > 101 {
		return works, 0, 0, model.ErrInvalid
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return works, 0, 0, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT catalog+favorites FROM revisions WHERE singleton=1`).Scan(&revision); err != nil {
		return works, 0, 0, err
	}
	if expected != 0 && expected != revision {
		return works, revision, 0, model.ErrCursor
	}
	var expiry *int64
	if err = tx.QueryRowContext(ctx, `SELECT min(sn.expires_at)`+localPerson, now, id).Scan(&expiry); err != nil {
		return works, 0, 0, err
	}
	if expiry == nil {
		return works, revision, 0, model.ErrNotFound
	}
	rows, err := tx.QueryContext(ctx, `WITH candidates AS (SELECT DISTINCT c.remote_title_id,e.entry_key,e.media_file_id,e.series_id,e.library_id,e.kind,e.added_at,e.season_count,e.episode_count,e.file_count,e.available_file_count,e.subject_id`+localPerson+`), ranked AS (SELECT *,row_number() OVER(PARTITION BY remote_title_id ORDER BY available_file_count>0 DESC,entry_key) AS n FROM candidates)
 SELECT r.remote_title_id,r.entry_key,coalesce(r.media_file_id,''),coalesce(r.series_id,''),r.library_id,r.kind,r.added_at,r.season_count,r.episode_count,r.file_count,r.available_file_count,r.subject_id,
 EXISTS(SELECT 1 FROM media_favorites f WHERE f.media_file_id=r.media_file_id) OR EXISTS(SELECT 1 FROM series_favorites f WHERE f.series_id=r.series_id),
 sn.title,sn.original_title,sn.year,sn.expires_at,pc.source FROM ranked r JOIN metadata_snapshots sn ON sn.remote_title_id=r.remote_title_id JOIN remote_titles rt ON rt.id=r.remote_title_id JOIN provider_catalogs pc ON pc.id=rt.catalog_id WHERE n=1 AND r.remote_title_id>? ORDER BY r.remote_title_id LIMIT ?`, now, id, after, limit)
	if err != nil {
		return works, 0, 0, err
	}
	for rows.Next() {
		var w model.PersonWork
		w.Match.Title = &model.Title{}
		w.Match.Artwork = map[string]string{}
		t := w.Match.Title
		if err = rows.Scan(&w.RemoteTitleID, &w.EntryKey, &w.MediaFileID, &w.SeriesID, &w.LibraryID, &w.Kind, &w.AddedAt, &w.SeasonCount, &w.EpisodeCount, &w.FileCount, &w.AvailableFileCount, &w.Match.Target.ID, &w.Favorite, &t.Title, &t.OriginalTitle, &t.Year, &t.ExpiresAt, &t.Ref.Source); err != nil {
			break
		}
		w.Title = t.Title
		w.State = "available"
		if w.AvailableFileCount == 0 {
			w.State = "deleted"
		}
		w.Roles = []string{}
		w.Characters = []string{}
		works = append(works, w)
	}
	err = errors.Join(err, rows.Err())
	rows.Close()
	if err != nil {
		return works, 0, 0, err
	}
	for i := range works {
		w := &works[i]
		if w.MediaFileID != "" {
			var relative string
			if err = tx.QueryRowContext(ctx, `SELECT relative_path FROM media_files WHERE id=?`, w.MediaFileID).Scan(&relative); err != nil {
				return works, 0, 0, err
			}
			w.Filename = filepath.Base(relative)
			var p model.Progress
			err = tx.QueryRowContext(ctx, `SELECT position,duration,completed,updated_at FROM watch_progress WHERE media_file_id=?`, w.MediaFileID).Scan(&p.Position, &p.Duration, &p.Completed, &p.UpdatedAt)
			if err == nil {
				w.Progress = &p
			} else if !errors.Is(err, sql.ErrNoRows) {
				return works, 0, 0, err
			}
			err = tx.QueryRowContext(ctx, `SELECT favorited_at FROM media_favorites WHERE media_file_id=?`, w.MediaFileID).Scan(&w.FavoritedAt)
		} else {
			err = tx.QueryRowContext(ctx, `SELECT favorited_at FROM series_favorites WHERE series_id=?`, w.SeriesID).Scan(&w.FavoritedAt)
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return works, 0, 0, err
		}
		err = nil

		rows, err = tx.QueryContext(ctx, `SELECT role,characters FROM title_credits WHERE person_id=? AND remote_title_id=? ORDER BY role,ordinal LIMIT 150`, id, w.RemoteTitleID)
		if err != nil {
			return works, 0, 0, err
		}
		seenRole, seenChar := map[string]bool{}, map[string]bool{}
		for rows.Next() {
			var role, body string
			if err = rows.Scan(&role, &body); err != nil {
				break
			}
			if !seenRole[role] {
				w.Roles = append(w.Roles, role)
				seenRole[role] = true
			}
			var chars []string
			if err = json.Unmarshal([]byte(body), &chars); err != nil {
				break
			}
			for _, v := range chars {
				if !seenChar[v] && len(w.Characters) < 50 {
					seenChar[v] = true
					w.Characters = append(w.Characters, v)
				}
			}
		}
		err = errors.Join(err, rows.Err())
		rows.Close()
		if err != nil {
			return works, 0, 0, err
		}
		rows, err = tx.QueryContext(ctx, `SELECT kind,id FROM assets WHERE remote_title_id=? AND credit_id IS NULL`, w.RemoteTitleID)
		if err != nil {
			return works, 0, 0, err
		}
		for rows.Next() {
			var k, a string
			if err = rows.Scan(&k, &a); err != nil {
				break
			}
			w.Match.Artwork[k] = a
		}
		err = errors.Join(err, rows.Err())
		rows.Close()
		if err != nil {
			return works, 0, 0, err
		}
	}
	return works, revision, *expiry, tx.Commit()
}
