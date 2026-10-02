package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reelnest/internal/model"
)

func writeTitle(ctx context.Context, tx *sql.Tx, t model.Title, now int64) (string, error) {
	ref := t.Ref
	id := model.NewID()
	if _, e := tx.ExecContext(ctx, `INSERT INTO remote_titles(id,catalog_id,namespace,kind,remote_id) VALUES(?,?,?,?,?) ON CONFLICT(catalog_id,namespace,kind,remote_id) DO NOTHING`, id, ref.CatalogID, ref.Namespace, ref.Kind, ref.RemoteID); e != nil {
		return "", e
	}
	if e := tx.QueryRowContext(ctx, `SELECT id FROM remote_titles WHERE catalog_id=? AND namespace=? AND kind=? AND remote_id=?`, ref.CatalogID, ref.Namespace, ref.Kind, ref.RemoteID).Scan(&id); e != nil {
		return "", e
	}
	var oldRevision, oldFetched, oldExpires int64
	e := tx.QueryRowContext(ctx, `SELECT revision,fetched_at,expires_at FROM metadata_snapshots WHERE remote_title_id=?`, id).Scan(&oldRevision, &oldFetched, &oldExpires)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	if t.Partial {
		if e != nil || oldRevision != t.Revision || oldExpires <= now {
			return "", model.ErrConflict
		}
		if _, e = tx.ExecContext(ctx, `UPDATE metadata_snapshots SET revision=revision+1,
 credits_state=CASE WHEN ? THEN ? ELSE credits_state END,credits_retry_at=CASE WHEN ? THEN ? ELSE credits_retry_at END,
 artwork_state=CASE WHEN ? THEN ? ELSE artwork_state END,artwork_retry_at=CASE WHEN ? THEN ? ELSE artwork_retry_at END WHERE remote_title_id=?`, t.UpdateCredits, t.CreditsState, t.UpdateCredits, t.CreditsRetryAt, t.UpdateArtwork, t.ArtworkState, t.UpdateArtwork, t.ArtworkRetryAt, id); e != nil {
			return "", e
		}
	} else {
		if t.Title == "" || t.ExpiresAt <= now || t.ExpiresAt <= t.FetchedAt {
			return "", model.ErrMetadata
		}
		// A different subject may have fetched the same title more recently.
		if oldFetched > t.FetchedAt {
			return id, nil
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO metadata_snapshots VALUES(?,?,?,?,?,?,?,?,?,?,?, ?,?,?) ON CONFLICT(remote_title_id) DO UPDATE SET title=excluded.title,original_title=excluded.original_title,identifier=excluded.identifier,year=excluded.year,overview=excluded.overview,source_url=excluded.source_url,fetched_at=excluded.fetched_at,expires_at=excluded.expires_at,revision=metadata_snapshots.revision+1,credits_state=excluded.credits_state,artwork_state=excluded.artwork_state,credits_retry_at=excluded.credits_retry_at,artwork_retry_at=excluded.artwork_retry_at`, id, t.Title, t.OriginalTitle, t.Identifier, t.Year, t.Overview, t.SourceURL, t.FetchedAt, t.ExpiresAt, 1, t.CreditsState, t.ArtworkState, t.CreditsRetryAt, t.ArtworkRetryAt); e != nil {
			return "", e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM assets WHERE remote_title_id=?`, id); e != nil {
			return "", e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM title_credits WHERE remote_title_id=?`, id); e != nil {
			return "", e
		}
	}
	if !t.Partial || t.UpdateCredits {
		if _, e = tx.ExecContext(ctx, `DELETE FROM title_credits WHERE remote_title_id=?`, id); e != nil {
			return "", e
		}
		ordinals := map[string]int{}
		for _, credit := range t.Credits {
			ordinal := ordinals[credit.Role]
			ordinals[credit.Role]++
			if (credit.Role == "actor" && ordinal >= 100) || (credit.Role == "director" && ordinal >= 50) {
				continue
			}
			key := "id:" + credit.Key
			if credit.Key == "" {
				key = fmt.Sprintf("title:%s:%s:%d", id, credit.Role, ordinal)
			}
			person := model.NewID()
			if _, e = tx.ExecContext(ctx, `INSERT INTO people VALUES(?,?,?) ON CONFLICT(catalog_id,person_key) DO NOTHING`, person, ref.CatalogID, key); e != nil {
				return "", e
			}
			if e = tx.QueryRowContext(ctx, `SELECT id FROM people WHERE catalog_id=? AND person_key=?`, ref.CatalogID, key).Scan(&person); e != nil {
				return "", e
			}
			characters := credit.Characters
			if characters == nil {
				characters = []string{}
			}
			body, e := json.Marshal(characters)
			if e != nil {
				return "", e
			}
			creditID := model.NewID()
			if _, e = tx.ExecContext(ctx, `INSERT INTO title_credits VALUES(?,?,?,?,?,?,?,?)`, creditID, id, ref.CatalogID, person, credit.Role, ordinal, credit.Name, string(body)); e != nil {
				return "", e
			}
			if credit.Photo != "" {
				if e = writeAsset(ctx, tx, id, ref.CatalogID, creditID, "photo", credit.Photo, ref.ProviderRevision); e != nil {
					return "", e
				}
			}
		}
	}
	if !t.Partial || t.UpdateArtwork {
		// Missing references replace old ones; primary may be kept on an optional
		// artwork-only retry because TMDB's image endpoint does not return a poster.
		for _, kind := range []string{"primary", "thumb", "backdrop"} {
			reference := t.Artwork[kind]
			if reference == "" {
				if kind != "primary" || !t.Partial {
					if _, e = tx.ExecContext(ctx, `DELETE FROM assets WHERE remote_title_id=? AND kind=? AND credit_id IS NULL`, id, kind); e != nil {
						return "", e
					}
				}
				continue
			}
			if e = writeAsset(ctx, tx, id, ref.CatalogID, "", kind, reference, ref.ProviderRevision); e != nil {
				return "", e
			}
		}
	}
	// Existing references may be downloaded under the newly validated config,
	// never under the stale configuration captured by an in-flight download.
	if _, e = tx.ExecContext(ctx, `UPDATE assets SET provider_revision=?,revision=revision+1 WHERE remote_title_id=? AND provider_revision<>?`, ref.ProviderRevision, id, ref.ProviderRevision); e != nil {
		return "", e
	}
	return id, nil
}
func writeAsset(ctx context.Context, tx *sql.Tx, title, catalog, credit, kind, reference string, revision int64) error {
	// Change the entire identity when a reference changes; old asset URLs disappear.
	_, e := tx.ExecContext(ctx, `DELETE FROM assets WHERE remote_title_id=? AND kind=? AND coalesce(credit_id,'')=? AND (reference<>? OR provider_revision<>?)`, title, kind, credit, reference, revision)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO assets(id,remote_title_id,catalog_id,credit_id,kind,reference,revision,provider_revision) VALUES(?,?,?,?,?,?,1,?) ON CONFLICT DO NOTHING`, model.NewID(), title, catalog, nullable(credit), kind, reference, revision)
	return e
}
func readMatch(ctx context.Context, q queryReader, id string, now int64) (model.Match, error) {
	m := model.Match{CacheState: "absent", AttemptState: "never", Credits: []model.StoredCredit{}, Artwork: map[string]string{}}
	t, e := readTarget(ctx, q, id, false)
	if e != nil {
		return m, e
	}
	m.Target = t
	if t.RemoteTitleID == "" {
		var state string
		var finished *int64
		var code *string
		e = q.QueryRowContext(ctx, `SELECT state,finished_at,error_code FROM metadata_jobs WHERE subject_id=?`, id).Scan(&state, &finished, &code)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return m, e
		}
		if state == "failed" {
			m.AttemptState = "failed"
			m.AttemptedAt = finished
			m.ErrorCode = code
		}
		return m, nil
	}
	e = q.QueryRowContext(ctx, `SELECT attempt_state,attempted_at,error_code FROM metadata_bindings WHERE subject_id=?`, id).Scan(&m.AttemptState, &m.AttemptedAt, &m.ErrorCode)
	if e != nil {
		return m, e
	}
	var title model.Title
	title.Ref = t.Ref
	e = q.QueryRowContext(ctx, `SELECT title,original_title,identifier,year,overview,source_url,fetched_at,expires_at,revision,credits_state,artwork_state,credits_retry_at,artwork_retry_at FROM metadata_snapshots WHERE remote_title_id=?`, t.RemoteTitleID).Scan(&title.Title, &title.OriginalTitle, &title.Identifier, &title.Year, &title.Overview, &title.SourceURL, &title.FetchedAt, &title.ExpiresAt, &title.Revision, &title.CreditsState, &title.ArtworkState, &title.CreditsRetryAt, &title.ArtworkRetryAt)
	if errors.Is(e, sql.ErrNoRows) {
		return m, nil
	}
	if e != nil {
		return m, e
	}
	if title.ExpiresAt <= now {
		m.CacheState = "expired"
		return m, nil
	}
	m.CacheState = "fresh"
	m.Title = &title
	rows, e := q.QueryContext(ctx, `SELECT c.id,c.person_id,c.name,c.role,c.characters,(SELECT a.id FROM assets a WHERE a.credit_id=c.id) FROM title_credits c WHERE c.remote_title_id=? ORDER BY c.role,c.ordinal LIMIT 150`, t.RemoteTitleID)
	if e != nil {
		return m, e
	}
	for rows.Next() {
		var c model.StoredCredit
		var chars string
		if e = rows.Scan(&c.ID, &c.PersonID, &c.Name, &c.Role, &chars, &c.PhotoAssetID); e != nil {
			break
		}
		if e = json.Unmarshal([]byte(chars), &c.Characters); e != nil {
			break
		}
		m.Credits = append(m.Credits, c)
	}
	e = errors.Join(e, rows.Err())
	rows.Close()
	if e != nil {
		return m, e
	}
	rows, e = q.QueryContext(ctx, `SELECT kind,id FROM assets WHERE remote_title_id=? AND credit_id IS NULL`, t.RemoteTitleID)
	if e != nil {
		return m, e
	}
	for rows.Next() {
		var kind, id string
		if e = rows.Scan(&kind, &id); e != nil {
			break
		}
		m.Artwork[kind] = id
	}
	e = errors.Join(e, rows.Err())
	rows.Close()
	return m, e
}
func (s *Store) MetadataMatch(ctx context.Context, id string, now int64) (model.Match, error) {
	tx, e := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return model.Match{}, e
	}
	defer tx.Rollback()
	m, e := readMatch(ctx, tx, id, now)
	if e != nil {
		return m, e
	}
	return m, tx.Commit()
}

// CachedTitle supplies only the structured cache header, so optional retries do
// not round-trip entire credits through another truth or extend the original TTL.
func (s *Store) CachedTitle(ctx context.Context, ref model.RemoteRef, now int64) (model.Title, error) {
	var t model.Title
	t.Ref = ref
	e := s.reader.QueryRowContext(ctx, `SELECT s.title,s.original_title,s.identifier,s.year,s.overview,s.source_url,s.fetched_at,s.expires_at,s.revision,s.credits_state,s.artwork_state,s.credits_retry_at,s.artwork_retry_at FROM metadata_snapshots s JOIN remote_titles r ON r.id=s.remote_title_id WHERE r.catalog_id=? AND r.namespace=? AND r.kind=? AND r.remote_id=? AND s.expires_at>?`, ref.CatalogID, ref.Namespace, ref.Kind, ref.RemoteID, now).Scan(&t.Title, &t.OriginalTitle, &t.Identifier, &t.Year, &t.Overview, &t.SourceURL, &t.FetchedAt, &t.ExpiresAt, &t.Revision, &t.CreditsState, &t.ArtworkState, &t.CreditsRetryAt, &t.ArtworkRetryAt)
	t.Partial = true
	return t, notFound(e)
}
func (s *Store) MetadataAsset(ctx context.Context, id string, now int64) (model.Asset, error) {
	var a model.Asset
	e := s.reader.QueryRowContext(ctx, `SELECT a.id,a.remote_title_id,a.catalog_id,c.source,a.kind,a.reference,a.revision,a.provider_revision,a.state,a.bytes,coalesce(a.mime,''),a.expires_at,a.retry_at,s.revision,s.expires_at
 FROM assets a JOIN metadata_snapshots s ON s.remote_title_id=a.remote_title_id JOIN provider_catalogs c ON c.id=a.catalog_id WHERE a.id=? AND s.expires_at>? AND EXISTS(SELECT 1 FROM metadata_bindings b JOIN metadata_subjects sub ON sub.id=b.subject_id WHERE b.remote_title_id=a.remote_title_id AND (EXISTS(SELECT 1 FROM media_files f WHERE f.id=sub.media_file_id AND f.state='available') OR EXISTS(SELECT 1 FROM file_organization o JOIN media_files f ON f.id=o.media_file_id WHERE o.series_id=sub.series_id AND f.state='available')))`, id, now).Scan(&a.ID, &a.RemoteTitleID, &a.CatalogID, &a.Source, &a.Kind, &a.Reference, &a.Revision, &a.ProviderRevision, &a.State, &a.Bytes, &a.MIME, &a.ExpiresAt, &a.RetryAt, &a.SnapshotRevision, &a.SnapshotExpiry)
	return a, notFound(e)
}
func (s *Store) CompleteAsset(ctx context.Context, a model.Asset, body []byte, mime string, failed bool, now int64) error {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	state := "ready"
	var expiry, retry any
	var bytes, mimeValue any = body, mime
	if failed {
		state = "cooling"
		bytes = nil
		mimeValue = nil
		retry = now + 86400000
	} else {
		expiry = min(a.SnapshotExpiry, now+30*86400000)
	}
	res, e := tx.ExecContext(ctx, `UPDATE assets SET state=?,bytes=?,mime=?,expires_at=?,retry_at=? WHERE id=? AND revision=? AND reference=? AND provider_revision=?
 AND EXISTS(SELECT 1 FROM provider_settings p WHERE p.source=? AND p.catalog_id=assets.catalog_id AND p.revision=assets.provider_revision)
 AND EXISTS(SELECT 1 FROM metadata_snapshots s WHERE s.remote_title_id=assets.remote_title_id AND s.revision=? AND s.expires_at>?)
 AND EXISTS(SELECT 1 FROM metadata_bindings b JOIN metadata_subjects sub ON sub.id=b.subject_id WHERE b.remote_title_id=assets.remote_title_id AND (EXISTS(SELECT 1 FROM media_files f WHERE f.id=sub.media_file_id AND f.state='available') OR EXISTS(SELECT 1 FROM file_organization o JOIN media_files f ON f.id=o.media_file_id WHERE o.series_id=sub.series_id AND f.state='available')))`, state, bytes, mimeValue, expiry, retry, a.ID, a.Revision, a.Reference, a.ProviderRevision, a.Source, a.SnapshotRevision, now)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return model.ErrConflict
	}
	return tx.Commit()
}
