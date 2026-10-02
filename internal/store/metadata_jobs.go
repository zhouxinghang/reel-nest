package store

import (
	"context"
	"database/sql"
	"errors"
	"reelnest/internal/model"
	"strings"
	"time"
)

type queryReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

const targetSQL = `SELECT s.id,s.library_id,s.expected_kind,s.revision,l.policy_revision,
 coalesce(b.revision,0),coalesce(b.selection,''),coalesce(b.remote_title_id,''),
 coalesce(r.catalog_id,''),coalesce(c.source,''),coalesce(r.namespace,''),coalesce(r.remote_id,''),coalesce(p.revision,1),
 coalesce((SELECT local_title FROM catalog_entries WHERE subject_id=s.id ORDER BY series_id IS NULL LIMIT 1),''),
 coalesce((SELECT group_concat(source,',') FROM (SELECT source FROM library_sources WHERE library_id=s.library_id ORDER BY priority)),'')
 FROM metadata_subjects s JOIN libraries l ON l.id=s.library_id
 LEFT JOIN metadata_bindings b ON b.subject_id=s.id LEFT JOIN remote_titles r ON r.id=b.remote_title_id
 LEFT JOIN provider_catalogs c ON c.id=r.catalog_id LEFT JOIN provider_settings p ON p.source=c.source
 WHERE s.id=?`
const targetAvailable = ` AND (s.media_file_id IS NULL AND EXISTS(SELECT 1 FROM file_organization o JOIN media_files f ON f.id=o.media_file_id WHERE o.series_id=s.series_id AND f.state='available') OR EXISTS(SELECT 1 FROM media_files f WHERE f.id=s.media_file_id AND f.state='available'))`

func readTarget(ctx context.Context, q queryReader, id string, available bool) (model.MetadataTarget, error) {
	var t model.MetadataTarget
	var sources string
	query := targetSQL
	if available {
		query += targetAvailable
	}
	e := q.QueryRowContext(ctx, query, id).Scan(&t.ID, &t.LibraryID, &t.Kind, &t.SubjectRevision, &t.PolicyRevision, &t.BindingRevision, &t.Selection, &t.RemoteTitleID, &t.Ref.CatalogID, &t.Ref.Source, &t.Ref.Namespace, &t.Ref.RemoteID, &t.Ref.ProviderRevision, &t.Query, &sources)
	t.Ref.Kind = t.Kind
	t.Sources = []string{}
	if sources != "" {
		t.Sources = strings.Split(sources, ",")
	}
	return t, notFound(e)
}
func (s *Store) MetadataTarget(ctx context.Context, id string) (model.MetadataTarget, error) {
	return readTarget(ctx, s.reader, id, true)
}
func versionsValid(ctx context.Context, tx *sql.Tx, versions []model.ProviderVersion) (bool, error) {
	for _, v := range versions {
		var rev int64
		var catalog string
		e := tx.QueryRowContext(ctx, `SELECT revision,coalesce(catalog_id,'') FROM provider_settings WHERE source=?`, v.Source).Scan(&rev, &catalog)
		if e != nil {
			return false, e
		}
		if rev != v.Revision || catalog != v.CatalogID {
			return false, nil
		}
	}
	return true, nil
}
func targetVersions(t model.MetadataTarget, versions []model.ProviderVersion) []model.ProviderVersion {
	out := []model.ProviderVersion{}
	for _, v := range versions {
		use := false
		if t.Selection == "manual" {
			use = t.Ref.Source == v.Source
		} else {
			for _, source := range t.Sources {
				use = use || source == v.Source
			}
		}
		if use {
			out = append(out, v)
		}
	}
	return out
}
func recordClaim(ctx context.Context, tx *sql.Tx, t model.MetadataTarget, now int64) error {
	_, e := tx.ExecContext(ctx, `UPDATE metadata_jobs SET state='running',claimed_generation=requested_generation,attempts=attempts+1,subject_revision=?,binding_revision=?,policy_revision=?,catalog_id=?,started_at=?,finished_at=NULL,next_attempt_at=NULL,error_code=NULL WHERE subject_id=?`, t.SubjectRevision, t.BindingRevision, t.PolicyRevision, nullable(t.Ref.CatalogID), now, t.ID)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM metadata_job_providers WHERE subject_id=?`, t.ID); e != nil {
		return e
	}
	for _, v := range t.Providers {
		if _, e = tx.ExecContext(ctx, `INSERT INTO metadata_job_providers VALUES(?,?,?,?)`, t.ID, v.Source, v.Revision, nullable(v.CatalogID)); e != nil {
			return e
		}
	}
	return nil
}
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Recovery is non-destructive. A consumed attempt is not refunded on restart.
func (s *Store) RecoverMetadata(ctx context.Context) error {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE metadata_jobs SET state=CASE WHEN attempts<3 THEN 'queued' ELSE 'failed' END,finished_at=CASE WHEN attempts<3 THEN NULL ELSE ? END,next_attempt_at=NULL WHERE state='running'`, time.Now().UnixMilli()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE revisions SET metadata=metadata+1 WHERE singleton=1`); e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) ClaimMetadata(ctx context.Context, versions []model.ProviderVersion, now int64) (model.MetadataTarget, error) {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return model.MetadataTarget{}, e
	}
	defer tx.Rollback()
	var id string
	var generation int64
	e = tx.QueryRowContext(ctx, `SELECT j.subject_id,j.requested_generation FROM metadata_jobs j JOIN metadata_subjects s ON s.id=j.subject_id WHERE (j.state='queued' OR j.state IN ('failed','succeeded') AND j.attempts<3 AND j.next_attempt_at IS NOT NULL AND j.next_attempt_at<=?) AND j.attempts<3 `+targetAvailable+` ORDER BY j.requested_at,j.subject_id LIMIT 1`, now).Scan(&id, &generation)
	if e != nil {
		return model.MetadataTarget{}, notFound(e)
	}
	t, e := readTarget(ctx, tx, id, true)
	if e != nil {
		return t, e
	}
	t.Generation = generation
	t.Providers = targetVersions(t, versions)
	valid, e := versionsValid(ctx, tx, t.Providers)
	if e != nil {
		return t, e
	}
	if !valid {
		return t, model.ErrConflict
	}
	if e = recordClaim(ctx, tx, t, now); e != nil {
		return t, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE revisions SET metadata=metadata+1 WHERE singleton=1`); e != nil {
		return t, e
	}
	return t, tx.Commit()
}

// BeginSelection supersedes any automatic/in-flight generation, but does not
// change the binding until the verified remote title is committed successfully.
func (s *Store) BeginSelection(ctx context.Context, id string, subject, binding int64, versions []model.ProviderVersion, ref model.RemoteRef) (model.MetadataTarget, error) {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return model.MetadataTarget{}, e
	}
	defer tx.Rollback()
	t, e := readTarget(ctx, tx, id, true)
	if e != nil {
		return t, e
	}
	if t.SubjectRevision != subject || t.BindingRevision != binding || t.Kind != ref.Kind {
		return t, model.ErrConflict
	}
	t.Providers = versions
	valid, e := versionsValid(ctx, tx, versions)
	if e != nil {
		return t, e
	}
	if !valid {
		return t, model.ErrCandidate
	}
	now := time.Now().UnixMilli()
	if _, e = tx.ExecContext(ctx, `INSERT INTO metadata_jobs(subject_id,requested_generation,state,requested_at) VALUES(?,1,'queued',?) ON CONFLICT(subject_id) DO UPDATE SET requested_generation=requested_generation+1,state='queued',attempts=0,requested_at=excluded.requested_at`, id, now); e != nil {
		return t, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT requested_generation FROM metadata_jobs WHERE subject_id=?`, id).Scan(&t.Generation); e != nil {
		return t, e
	}
	if e = recordClaim(ctx, tx, t, now); e != nil {
		return t, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE revisions SET metadata=metadata+1 WHERE singleton=1`); e != nil {
		return t, e
	}
	return t, tx.Commit()
}
func (s *Store) RefreshMetadata(ctx context.Context, library string, now int64) (int64, error) {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	if library != "" {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT 1 FROM libraries WHERE id=?`, library).Scan(&n); e != nil {
			return 0, notFound(e)
		}
	}
	res, e := tx.ExecContext(ctx, `INSERT INTO metadata_jobs(subject_id,requested_generation,state,requested_at)
 SELECT s.id,1,'queued',? FROM metadata_subjects s WHERE (?='' OR s.library_id=?) `+targetAvailable+`
 ON CONFLICT(subject_id) DO UPDATE SET requested_generation=requested_generation+1,state='queued',attempts=0,requested_at=excluded.requested_at,started_at=NULL,finished_at=NULL,next_attempt_at=NULL,error_code=NULL`, now, library, library)
	if e != nil {
		return 0, e
	}
	n, e := res.RowsAffected()
	if e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE revisions SET metadata=metadata+1 WHERE singleton=1`); e != nil {
		return 0, e
	}
	return n, tx.Commit()
}
func validateResult(ctx context.Context, tx *sql.Tx, t model.MetadataTarget) (bool, error) {
	cur, e := readTarget(ctx, tx, t.ID, true)
	if errors.Is(e, model.ErrNotFound) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if cur.SubjectRevision != t.SubjectRevision || cur.BindingRevision != t.BindingRevision || cur.PolicyRevision != t.PolicyRevision || cur.Kind != t.Kind {
		return false, nil
	}
	var gen, claimed int64
	var state string
	e = tx.QueryRowContext(ctx, `SELECT requested_generation,coalesce(claimed_generation,0),state FROM metadata_jobs WHERE subject_id=?`, t.ID).Scan(&gen, &claimed, &state)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if gen != t.Generation || claimed != t.Generation || state != "running" {
		return false, nil
	}
	return versionsValid(ctx, tx, t.Providers)
}

// CompleteMetadata is the only matching-result writer, including failures and
// optional supplements. All checks and mutations share one short transaction.
func (s *Store) CompleteMetadata(ctx context.Context, r model.MetadataResult, now int64) error {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	valid, e := validateResult(ctx, tx, r.Target)
	if e != nil {
		return e
	}
	if !valid {
		if e = tx.Rollback(); e != nil {
			return e
		}
		return s.supersedeMetadata(ctx, r.Target, now)
	}

	t := r.Target
	if r.ErrorCode != "" {
		if _, e = tx.ExecContext(ctx, `UPDATE metadata_bindings SET attempted_at=?,attempt_state='failed',error_code=? WHERE subject_id=?`, now, r.ErrorCode, t.ID); e != nil {
			return e
		}
		// Manual failed selections are never retried as automatic selections.
		retry := any(nil)
		if r.RetryAt > now {
			retry = r.RetryAt
		}
		if _, e = tx.ExecContext(ctx, `UPDATE metadata_jobs SET state='failed',finished_at=?,error_code=?,next_attempt_at=CASE WHEN attempts<3 THEN ? ELSE NULL END WHERE subject_id=?`, now, r.ErrorCode, retry, t.ID); e != nil {
			return e
		}
	} else {
		titleID := t.RemoteTitleID
		if r.Title != nil {
			ref := r.Title.Ref
			authorized := false
			for _, v := range t.Providers {
				authorized = authorized || (v.Source == ref.Source && v.CatalogID == ref.CatalogID && v.Revision == ref.ProviderRevision)
			}
			if !authorized || ref.Kind != t.Kind {
				return model.ErrConflict
			}
			titleID, e = writeTitle(ctx, tx, *r.Title, now)
			if errors.Is(e, model.ErrConflict) {
				if err := tx.Rollback(); err != nil {
					return err
				}
				return s.supersedeMetadata(ctx, t, now)
			}
			if e != nil {
				return e
			}
		}
		if titleID == "" {
			return model.ErrMetadata
		}
		selection := r.Selection
		if selection == "" {
			selection = t.Selection
		}
		if selection == "" {
			selection = "automatic"
		}
		revision := t.BindingRevision
		if r.Rebind || titleID != t.RemoteTitleID || selection != t.Selection {
			revision++
		}
		if revision == 0 {
			revision = 1
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO metadata_bindings(subject_id,expected_kind,remote_title_id,selection,revision,attempted_at,attempt_state) VALUES(?,?,?,?,?,?,'succeeded') ON CONFLICT(subject_id) DO UPDATE SET remote_title_id=excluded.remote_title_id,selection=excluded.selection,revision=excluded.revision,attempted_at=excluded.attempted_at,attempt_state='succeeded',error_code=NULL`, t.ID, t.Kind, titleID, selection, revision, now); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE metadata_jobs SET state='succeeded',finished_at=?,error_code=NULL,next_attempt_at=CASE WHEN attempts<3 AND ?>? THEN ? ELSE NULL END WHERE subject_id=?`, now, r.RetryAt, now, r.RetryAt, t.ID); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE revisions SET catalog=catalog+1,metadata=metadata+1 WHERE singleton=1`); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) MetadataStatus(ctx context.Context, library, after string, limit int, expected int64) (model.MetadataStatus, error) {
	result := model.MetadataStatus{Counts: map[string]int64{}, Failures: []model.MetadataJob{}}
	if limit < 1 || limit > 101 {
		return result, model.ErrInvalid
	}
	tx, e := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	e = tx.QueryRowContext(ctx, `SELECT metadata FROM revisions WHERE singleton=1`).Scan(&result.Revision)
	if e != nil {
		return result, e
	}
	if expected != 0 && expected != result.Revision {
		return result, model.ErrCursor
	}
	rows, e := tx.QueryContext(ctx, `SELECT j.state,count(*) FROM metadata_jobs j JOIN metadata_subjects s ON s.id=j.subject_id WHERE (?='' OR s.library_id=?) GROUP BY j.state`, library, library)
	if e != nil {
		return result, e
	}
	for rows.Next() {
		var state string
		var n int64
		if e = rows.Scan(&state, &n); e != nil {
			break
		}
		result.Counts[state] = n
	}
	e = errors.Join(e, rows.Err())
	rows.Close()
	if e != nil {
		return result, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT j.subject_id,j.state,j.requested_generation,j.claimed_generation,j.attempts,j.requested_at,j.started_at,j.finished_at,j.next_attempt_at,j.error_code FROM metadata_jobs j JOIN metadata_subjects s ON s.id=j.subject_id WHERE (?='' OR s.library_id=?) AND j.state='failed' AND j.subject_id>? ORDER BY j.subject_id LIMIT ?`, library, library, after, limit)
	if e != nil {
		return result, e
	}
	defer rows.Close()
	for rows.Next() {
		var j model.MetadataJob
		if e = rows.Scan(&j.SubjectID, &j.State, &j.RequestedGeneration, &j.ClaimedGeneration, &j.Attempts, &j.RequestedAt, &j.StartedAt, &j.FinishedAt, &j.NextAttemptAt, &j.ErrorCode); e != nil {
			return result, e
		}
		result.Failures = append(result.Failures, j)
	}
	if e = rows.Err(); e != nil {
		return result, e
	}
	return result, tx.Commit()
}

func (s *Store) supersedeMetadata(ctx context.Context, t model.MetadataTarget, now int64) error {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, available := readTarget(ctx, tx, t.ID, true)
	if available != nil && !errors.Is(available, model.ErrNotFound) {
		return available
	}
	if available == nil {
		_, e = tx.ExecContext(ctx, `UPDATE metadata_jobs SET requested_generation=requested_generation+1,state='queued',attempts=0,requested_at=?,started_at=NULL,finished_at=NULL,next_attempt_at=NULL,error_code=NULL WHERE subject_id=? AND requested_generation=? AND state='running'`, now, t.ID, t.Generation)
	} else {
		_, e = tx.ExecContext(ctx, `UPDATE metadata_jobs SET state='superseded',finished_at=?,next_attempt_at=NULL WHERE subject_id=? AND requested_generation=? AND state='running'`, now, t.ID, t.Generation)
	}
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE revisions SET metadata=metadata+1 WHERE singleton=1`); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return model.ErrConflict
}
