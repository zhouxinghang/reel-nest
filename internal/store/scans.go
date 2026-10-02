package store

import (
	"context"
	"database/sql"
	"time"

	"reelnest/internal/model"
)

const scanColumns = `id,library_id,state,requested_at,started_at,finished_at,discovered_count,error_code`

func scanRow(row rowScanner) (model.Scan, error) {
	var s model.Scan
	e := row.Scan(&s.ID, &s.LibraryID, &s.State, &s.RequestedAt, &s.StartedAt, &s.FinishedAt, &s.DiscoveredCount, &s.ErrorCode)
	return s, notFound(e)
}
func (s *Store) Scan(ctx context.Context, id string) (model.Scan, error) {
	return scanRow(s.reader.QueryRowContext(ctx, "SELECT "+scanColumns+" FROM scan_runs WHERE id=?", id))
}
func (s *Store) QueueScan(ctx context.Context, library string) (model.Scan, error) {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return model.Scan{}, e
	}
	defer tx.Rollback()
	var exists, active bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM libraries WHERE id=?),EXISTS(SELECT 1 FROM scan_runs WHERE library_id=? AND state IN ('queued','running'))", library, library).Scan(&exists, &active); e != nil {
		return model.Scan{}, e
	}
	if !exists {
		return model.Scan{}, model.ErrNotFound
	}
	if active {
		return model.Scan{}, model.ErrBusy
	}
	scan := model.Scan{ID: model.NewID(), LibraryID: library, State: "queued", RequestedAt: time.Now().UnixMilli()}
	if _, e = tx.ExecContext(ctx, "INSERT INTO scan_runs(id,library_id,state,requested_at) VALUES(?,?,'queued',?)", scan.ID, library, scan.RequestedAt); e != nil {
		return scan, e
	}
	return scan, tx.Commit()
}

// ClaimScan atomically claims a single persisted queued task. Two bounded workers
// may claim different libraries; the partial unique index rejects same-library work.
func (s *Store) ClaimScan(ctx context.Context) (model.Scan, error) {
	return scanRow(s.writer.QueryRowContext(ctx, `UPDATE scan_runs SET state='running',started_at=max(requested_at,?)
 WHERE id=(SELECT id FROM scan_runs WHERE state='queued' ORDER BY requested_at,id LIMIT 1) RETURNING `+scanColumns, time.Now().UnixMilli()))
}
func (s *Store) StageScan(ctx context.Context, id string, entries []model.ScanEntry) error {
	if len(entries) == 0 || len(entries) > 256 {
		return model.ErrInvalid
	}
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var library string
	if e = tx.QueryRowContext(ctx, "SELECT library_id FROM scan_runs WHERE id=? AND state='running'", id).Scan(&library); e != nil {
		return notFound(e)
	}
	for _, f := range entries {
		if _, e = tx.ExecContext(ctx, "INSERT INTO scan_entries VALUES(?,?,?,?,?,?)", id, library, f.Relative, f.Fingerprint, f.Size, f.ModifiedAt); e != nil {
			return e
		}
		o := f.Organization
		if _, e = tx.ExecContext(ctx, "INSERT INTO scan_organization VALUES(?,?,?,?,?,?,?)", id, f.Relative, o.Key, o.Title, o.Season, o.Episode, o.Reason); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE scan_runs SET discovered_count=discovered_count+? WHERE id=?", len(entries), id); e != nil {
		return e
	}
	return tx.Commit()
}

// InterruptScans is called at startup and after workers drain at shutdown. It
// never publishes or requeues staging from a previous lifetime.
func (s *Store) InterruptScans(ctx context.Context) error {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE scan_runs SET state='interrupted',finished_at=max(coalesce(started_at,requested_at),?),error_code='SCAN_INTERRUPTED' WHERE state IN ('queued','running')`, time.Now().UnixMilli()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM scan_entries WHERE scan_id IN (SELECT id FROM scan_runs WHERE state NOT IN ('queued','running'))"); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) FailScan(ctx context.Context, id string, interrupted bool) error {
	state, code := "failed", "SCAN_FAILED"
	if interrupted {
		state, code = "interrupted", "SCAN_INTERRUPTED"
	}
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE scan_runs SET state=?,error_code=?,finished_at=max(coalesce(started_at,requested_at),?) WHERE id=? AND state IN ('queued','running')`, state, code, time.Now().UnixMilli(), id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM scan_entries WHERE scan_id=?", id); e != nil {
		return e
	}
	return tx.Commit()
}

// CommitScan is the only publication path: media, organization, subjects,
// dependent cleanup, projections, metadata work and scan status share one tx.
// Set-based SQL consumes persisted staging without loading a Go file manifest or
// issuing a per-file chain of statements while holding the SQLite writer.
func (s *Store) CommitScan(ctx context.Context, id string) error {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var library, kind string
	var expected int64
	if e = tx.QueryRowContext(ctx, `SELECT r.library_id,l.kind,r.discovered_count FROM scan_runs r JOIN libraries l ON l.id=r.library_id WHERE r.id=? AND r.state='running'`, id).Scan(&library, &kind, &expected); e != nil {
		return notFound(e)
	}
	var staged, organized int64
	if e = tx.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM scan_entries WHERE scan_id=?),(SELECT count(*) FROM scan_organization WHERE scan_id=?)", id, id).Scan(&staged, &organized); e != nil {
		return e
	}
	if staged != organized || staged != expected {
		return model.ErrInvalid
	}
	now := time.Now().UnixMilli()
	// SQLite-generated random IDs are identifiers, not authorization credentials.
	// Every newly observed version/subject gets a fresh ID, never a path-derived ID.
	steps := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO media_files(id,library_id,relative_path,fingerprint,size,modified_at,added_at,organization_reason)
   SELECT lower(hex(randomblob(16))),e.library_id,e.relative_path,e.fingerprint,e.size,e.modified_at,?,o.reason
   FROM scan_entries e JOIN scan_organization o ON o.scan_id=e.scan_id AND o.relative_path=e.relative_path WHERE e.scan_id=?
   ON CONFLICT(library_id,relative_path,fingerprint) DO UPDATE SET state='available',organization_reason=excluded.organization_reason`, []any{now, id}},
		{`DELETE FROM file_organization WHERE library_id=?`, []any{library}},
		{`INSERT INTO series(id,library_id,title_key,local_title)
   SELECT lower(hex(randomblob(16))),?,o.title_key,min(o.local_title) FROM scan_organization o
   WHERE o.scan_id=? AND ?='series' AND o.local_title<>'' AND o.reason='' GROUP BY o.title_key
   ON CONFLICT(library_id,title_key) DO NOTHING`, []any{library, id, kind}},
		{`DELETE FROM metadata_subjects WHERE media_file_id IN (
   SELECT f.id FROM scan_entries e JOIN scan_organization o ON o.scan_id=e.scan_id AND o.relative_path=e.relative_path
   JOIN media_files f ON f.library_id=e.library_id AND f.relative_path=e.relative_path AND f.fingerprint=e.fingerprint
   WHERE e.scan_id=? AND ?='series' AND o.local_title<>'' AND o.reason='')`, []any{id, kind}},
		{`INSERT INTO file_organization(media_file_id,library_id,series_id,season_number,episode_number)
   SELECT f.id,e.library_id,se.id,o.season_number,o.episode_number FROM scan_entries e
   JOIN scan_organization o ON o.scan_id=e.scan_id AND o.relative_path=e.relative_path
   JOIN media_files f ON f.library_id=e.library_id AND f.relative_path=e.relative_path AND f.fingerprint=e.fingerprint
   JOIN series se ON se.library_id=e.library_id AND se.title_key=o.title_key
   WHERE e.scan_id=? AND ?='series' AND o.local_title<>'' AND o.reason=''`, []any{id, kind}},
		{`DELETE FROM media_files WHERE library_id=? AND NOT EXISTS
   (SELECT 1 FROM scan_entries e WHERE e.scan_id=? AND e.relative_path=media_files.relative_path AND e.fingerprint=media_files.fingerprint)`, []any{library, id}},
		{`DELETE FROM series WHERE library_id=? AND NOT EXISTS (SELECT 1 FROM file_organization o WHERE o.series_id=series.id)`, []any{library}},
		// This transaction-local table captures ONLY new subjects, so a rescan does
		// not requeue unchanged subjects whose previous metadata job was retired.
		{`CREATE TEMP TABLE scan_new_subjects(id TEXT PRIMARY KEY,library_id TEXT NOT NULL,expected_kind TEXT NOT NULL,media_file_id TEXT,series_id TEXT) STRICT`, nil},
		{`INSERT INTO scan_new_subjects
   SELECT lower(hex(randomblob(16))),f.library_id,?,f.id,NULL FROM media_files f
   WHERE f.library_id=? AND NOT EXISTS (SELECT 1 FROM file_organization o WHERE o.media_file_id=f.id)
   AND NOT EXISTS (SELECT 1 FROM metadata_subjects s WHERE s.media_file_id=f.id)`, []any{kind, library}},
		{`INSERT INTO scan_new_subjects
   SELECT lower(hex(randomblob(16))),se.library_id,'series',NULL,se.id FROM series se WHERE se.library_id=?
   AND NOT EXISTS (SELECT 1 FROM metadata_subjects s WHERE s.series_id=se.id)`, []any{library}},
		{`INSERT INTO metadata_subjects(id,library_id,expected_kind,media_file_id,series_id) SELECT * FROM scan_new_subjects`, nil},
		{`INSERT INTO metadata_jobs(subject_id,requested_generation,state,requested_at) SELECT id,1,'queued',? FROM scan_new_subjects`, []any{now}},
		{`DROP TABLE scan_new_subjects`, nil},
	}
	for _, step := range steps {
		if _, e = tx.ExecContext(ctx, step.query, step.args...); e != nil {
			return e
		}
	}
	if e = rebuildLocalProjection(ctx, tx, library); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE revisions SET catalog=catalog+1 WHERE singleton=1"); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE scan_runs SET state='completed',finished_at=max(started_at,?),error_code=NULL WHERE id=?", now, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM scan_entries WHERE scan_id=?", id); e != nil {
		return e
	}
	return tx.Commit()
}
func rebuildLocalProjection(ctx context.Context, tx *sql.Tx, library string) error {
	queries := []string{
		`DELETE FROM catalog_entries WHERE library_id=?`,
		`INSERT INTO catalog_entries(entry_key,library_id,media_file_id,subject_id,kind,local_title,local_search_key,title_sort_key,added_at,file_count,available_file_count)
 SELECT 'media:'||f.id,f.library_id,f.id,s.id,l.kind,f.relative_path,lower(f.relative_path),lower(f.relative_path),f.added_at,1,(f.state='available')
 FROM media_files f JOIN libraries l ON l.id=f.library_id JOIN metadata_subjects s ON s.media_file_id=f.id WHERE f.library_id=?`,
		`INSERT INTO catalog_entries(entry_key,library_id,series_id,subject_id,kind,local_title,local_search_key,title_sort_key,added_at,season_count,episode_count,file_count,available_file_count)
 SELECT 'series:'||se.id,se.library_id,se.id,s.id,'series',se.local_title,lower(se.local_title),lower(se.local_title),max(f.added_at),count(DISTINCT o.season_number),count(DISTINCT o.season_number||':'||o.episode_number),count(*),sum(f.state='available')
 FROM series se JOIN file_organization o ON o.series_id=se.id JOIN media_files f ON f.id=o.media_file_id JOIN metadata_subjects s ON s.series_id=se.id WHERE se.library_id=? GROUP BY se.id`,
	}
	for _, q := range queries {
		if _, e := tx.ExecContext(ctx, q, library); e != nil {
			return e
		}
	}
	return nil
}
