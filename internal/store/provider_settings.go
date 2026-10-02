package store

import (
	"context"
	"time"

	"reelnest/internal/model"
)

// ProviderSettings returns the three persisted configurations in one snapshot.
// Decryption, validation and registry publication belong to metadata.
func (s *Store) ProviderSettings(ctx context.Context) ([]model.StoredProviderSettings, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT p.source,coalesce(p.catalog_id,''),coalesce(c.base_url,''),p.secrets,p.revision
 FROM provider_settings p LEFT JOIN provider_catalogs c ON c.id=p.catalog_id ORDER BY p.source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.StoredProviderSettings{}
	for rows.Next() {
		var p model.StoredProviderSettings
		if err = rows.Scan(&p.Source, &p.CatalogID, &p.BaseURL, &p.Secrets, &p.Revision); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// SaveProviderSettings atomically switches directory space, persists ciphertext,
// and supersedes affected task generations. No network or decryption runs here.
// The metadata owner serializes this commit with publication of its new registry.
func (s *Store) SaveProviderSettings(ctx context.Context, next model.StoredProviderSettings, expected int64) (model.StoredProviderSettings, error) {
	if expected < 1 || expected >= 9007199254740991 || next.Revision != expected+1 || len(next.Secrets) < 28 || len(next.Secrets) > 64<<10 {
		return next, model.ErrInvalid
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return next, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM provider_settings WHERE source=?", next.Source).Scan(&revision); err != nil {
		return next, notFound(err)
	}
	if revision != expected {
		return next, model.ErrConflict
	}
	var catalog any
	switch next.Source {
	case "tmdb", "tvdb":
		var base string
		if err = tx.QueryRowContext(ctx, "SELECT base_url FROM provider_catalogs WHERE id=?", next.Source).Scan(&base); err != nil {
			return next, err
		}
		if base != next.BaseURL {
			return next, model.ErrInvalid
		}
		next.CatalogID = next.Source
		catalog = next.CatalogID
	case "metatube":
		next.CatalogID = ""
		if next.BaseURL != "" {
			if _, err = tx.ExecContext(ctx, `INSERT INTO provider_catalogs(id,source,base_url) VALUES(?,'metatube',?) ON CONFLICT(source,base_url) DO NOTHING`, model.NewID(), next.BaseURL); err != nil {
				return next, err
			}
			if err = tx.QueryRowContext(ctx, "SELECT id FROM provider_catalogs WHERE source='metatube' AND base_url=?", next.BaseURL).Scan(&next.CatalogID); err != nil {
				return next, err
			}
			catalog = next.CatalogID
		}
	default:
		return next, model.ErrInvalid
	}
	if _, err = tx.ExecContext(ctx, "UPDATE provider_settings SET catalog_id=?,secrets=?,revision=? WHERE source=? AND revision=?", catalog, next.Secrets, next.Revision, next.Source, expected); err != nil {
		return next, err
	}
	// An old manual choice remains in its original catalog, with its snapshot's
	// original TTL. Never reinterpret that remote ID on the newly configured server.
	if _, err = tx.ExecContext(ctx, `UPDATE metadata_jobs SET requested_generation=requested_generation+1,
 state='superseded',finished_at=?,next_attempt_at=NULL,error_code=NULL
 WHERE subject_id IN (SELECT b.subject_id FROM metadata_bindings b JOIN remote_titles r ON r.id=b.remote_title_id
 JOIN provider_catalogs c ON c.id=r.catalog_id WHERE b.selection='manual' AND c.source=? AND r.catalog_id<>?)`, time.Now().UnixMilli(), next.Source, next.CatalogID); err != nil {
		return next, err
	}
	// Only automatic targets whose policy uses this source, or manual choices in
	// the active catalog, need new work. Binding rows themselves are never changed.
	if _, err = tx.ExecContext(ctx, `INSERT INTO metadata_jobs(subject_id,requested_generation,state,requested_at)
 SELECT s.id,1,'queued',? FROM metadata_subjects s
 LEFT JOIN metadata_bindings b ON b.subject_id=s.id
 LEFT JOIN remote_titles r ON r.id=b.remote_title_id
 WHERE (s.media_file_id IS NULL OR EXISTS(SELECT 1 FROM media_files f WHERE f.id=s.media_file_id AND f.state='available'))
 AND (((b.selection IS NULL OR b.selection='automatic') AND EXISTS(SELECT 1 FROM library_sources l WHERE l.library_id=s.library_id AND l.source=?))
 OR (b.selection='manual' AND r.catalog_id=?))
 ON CONFLICT(subject_id) DO UPDATE SET requested_generation=metadata_jobs.requested_generation+1,
 state='queued',attempts=0,requested_at=excluded.requested_at,started_at=NULL,finished_at=NULL,next_attempt_at=NULL,error_code=NULL`, time.Now().UnixMilli(), next.Source, next.CatalogID); err != nil {
		return next, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE revisions SET catalog=catalog+1 WHERE singleton=1"); err != nil {
		return next, err
	}
	return next, tx.Commit()
}
