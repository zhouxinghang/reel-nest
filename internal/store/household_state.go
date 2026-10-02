package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"

	"reelnest/internal/model"
)

func (s *Store) SaveProgress(ctx context.Context, id string, p model.Progress) (model.Progress, error) {
	if math.IsNaN(p.Position) || math.IsNaN(p.Duration) || math.IsInf(p.Position, 0) || math.IsInf(p.Duration, 0) || p.Position < 0 || p.Duration <= 0 || p.Duration > 31622400 || p.Position > p.Duration || p.Completed && p.Position < p.Duration-0.5 {
		return p, model.ErrInvalid
	}
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return p, e
	}
	defer tx.Rollback()
	var state string
	if e = tx.QueryRowContext(ctx, "SELECT state FROM media_files WHERE id=?", id).Scan(&state); e != nil {
		return p, notFound(e)
	}
	if state != "available" {
		return p, model.ErrConflict
	}
	p.UpdatedAt = time.Now().UnixMilli()
	if _, e = tx.ExecContext(ctx, `INSERT INTO watch_progress VALUES(?,?,?,?,?) ON CONFLICT(media_file_id) DO UPDATE SET position=excluded.position,duration=excluded.duration,completed=excluded.completed,updated_at=excluded.updated_at`, id, p.Position, p.Duration, p.Completed, p.UpdatedAt); e != nil {
		return p, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE revisions SET progress=progress+1 WHERE singleton=1"); e != nil {
		return p, e
	}
	return p, tx.Commit()
}
func (s *Store) Progress(ctx context.Context, id string) (*model.Progress, error) {
	var exists bool
	if e := s.reader.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM media_files WHERE id=?)", id).Scan(&exists); e != nil {
		return nil, e
	}
	if !exists {
		return nil, model.ErrNotFound
	}
	var p model.Progress
	e := s.reader.QueryRowContext(ctx, "SELECT position,duration,completed,updated_at FROM watch_progress WHERE media_file_id=?", id).Scan(&p.Position, &p.Duration, &p.Completed, &p.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	return &p, e
}
func (s *Store) Favorite(ctx context.Context, kind, id string, value bool) (*int64, error) {
	table, key, target := "media_favorites", "media_file_id", "media_files"
	if kind == "series" {
		table, key, target = "series_favorites", "series_id", "series"
	} else if kind != "media" {
		return nil, model.ErrInvalid
	}
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var exists bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+target+" WHERE id=?)", id).Scan(&exists); e != nil {
		return nil, e
	}
	if !exists {
		return nil, model.ErrNotFound
	}
	var at *int64
	if value {
		_, e = tx.ExecContext(ctx, "INSERT INTO "+table+" VALUES(?,?) ON CONFLICT DO NOTHING", id, time.Now().UnixMilli())
		if e == nil {
			e = tx.QueryRowContext(ctx, "SELECT favorited_at FROM "+table+" WHERE "+key+"=?", id).Scan(&at)
		}
	} else {
		_, e = tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+key+"=?", id)
	}
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE revisions SET favorites=favorites+1 WHERE singleton=1"); e != nil {
		return nil, e
	}
	return at, tx.Commit()
}

// MarkFileDeleted retains the observable deleted record until a successful scan.
// The caller holds its library lease and uses a non-request cancellation context
// after unlink. Failure here never implies the physical deletion did not happen.
func (s *Store) MarkFileDeleted(ctx context.Context, id string) error {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var library string
	if e = tx.QueryRowContext(ctx, "UPDATE media_files SET state='deleted' WHERE id=? RETURNING library_id", id).Scan(&library); e != nil {
		return notFound(e)
	}
	if _, e = tx.ExecContext(ctx, "UPDATE metadata_subjects SET revision=revision+1 WHERE media_file_id=?", id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE metadata_jobs SET requested_generation=requested_generation+1,state='superseded',finished_at=?,next_attempt_at=NULL,error_code=NULL
 WHERE subject_id IN (SELECT sub.id FROM metadata_subjects sub WHERE sub.media_file_id=? OR sub.series_id IN (SELECT o.series_id FROM file_organization o WHERE o.media_file_id=?) AND NOT EXISTS(SELECT 1 FROM file_organization o JOIN media_files f ON f.id=o.media_file_id WHERE o.series_id=sub.series_id AND f.state='available'))`, time.Now().UnixMilli(), id, id); e != nil {
		return e
	}
	if e = updateDeletedProjection(ctx, tx, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE revisions SET catalog=catalog+1,metadata=metadata+1 WHERE singleton=1"); e != nil {
		return e
	}
	return tx.Commit()
}
