package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"reelnest/internal/model"
)

type rowScanner interface{ Scan(...any) error }

const libraryColumns = `l.id,l.name,l.root_path,l.root_identity,l.kind,l.revision,l.policy_revision,l.created_at,
 coalesce((SELECT group_concat(source,',') FROM (SELECT source FROM library_sources WHERE library_id=l.id ORDER BY priority)),''),
 (SELECT id FROM scan_runs WHERE library_id=l.id ORDER BY requested_at DESC,rowid DESC LIMIT 1)`

func libraryRow(row rowScanner) (model.Library, error) {
	var l model.Library
	var sources string
	e := row.Scan(&l.ID, &l.Name, &l.Path, &l.Identity, &l.Kind, &l.Revision, &l.PolicyRevision, &l.CreatedAt, &sources, &l.LatestScanID)
	l.Sources = []string{}
	if sources != "" {
		l.Sources = strings.Split(sources, ",")
	}
	return l, notFound(e)
}
func notFound(e error) error {
	if errors.Is(e, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	return e
}
func (s *Store) Library(ctx context.Context, id string) (model.Library, error) {
	return libraryRow(s.reader.QueryRowContext(ctx, "SELECT "+libraryColumns+" FROM libraries l WHERE l.id=?", id))
}

// Libraries is a revision-consistent bounded keyset page, including empty libraries.
func (s *Store) Libraries(ctx context.Context, after string, limit int, expected int64) ([]model.Library, int64, error) {
	if limit < 1 || limit > 101 {
		return nil, 0, model.ErrInvalid
	}
	tx, e := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback()
	var rev int64
	if e = tx.QueryRowContext(ctx, "SELECT catalog FROM revisions WHERE singleton=1").Scan(&rev); e != nil {
		return nil, 0, e
	}
	if expected != 0 && expected != rev {
		return nil, rev, model.ErrCursor
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+libraryColumns+" FROM libraries l WHERE l.id>? ORDER BY l.id LIMIT ?", after, limit)
	if e != nil {
		return nil, 0, e
	}
	list := []model.Library{}
	for rows.Next() {
		l, e := libraryRow(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		list = append(list, l)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, 0, e
	}
	return list, rev, tx.Commit()
}
func pathWithin(a, b string) bool {
	r, e := filepath.Rel(a, b)
	return e == nil && r != ".." && !strings.HasPrefix(r, "../")
}
func (s *Store) CreateLibrary(ctx context.Context, l model.Library) (model.Library, error) {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return l, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, "SELECT root_path,root_identity FROM libraries")
	if e != nil {
		return l, e
	}
	for rows.Next() {
		var path, identity string
		if e = rows.Scan(&path, &identity); e != nil {
			break
		}
		if identity == l.Identity || pathWithin(path, l.Path) || pathWithin(l.Path, path) {
			e = model.ErrOverlap
			break
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return l, e
	}
	now := time.Now().UnixMilli()
	l.CreatedAt = now
	l.Revision = 1
	l.PolicyRevision = 1
	if _, e = tx.ExecContext(ctx, `INSERT INTO libraries(id,name,root_path,root_identity,kind,created_at) VALUES(?,?,?,?,?,?)`, l.ID, l.Name, l.Path, l.Identity, l.Kind, now); e != nil {
		return l, e
	}
	if e = setSources(ctx, tx, l.ID, l.Sources); e != nil {
		return l, e
	}
	scanID := model.NewID()
	if _, e = tx.ExecContext(ctx, "INSERT INTO scan_runs(id,library_id,state,requested_at) VALUES(?,?,'queued',?)", scanID, l.ID, now); e != nil {
		return l, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE revisions SET catalog=catalog+1 WHERE singleton=1"); e != nil {
		return l, e
	}
	l.LatestScanID = &scanID
	return l, tx.Commit()
}
func setSources(ctx context.Context, tx *sql.Tx, id string, sources []string) error {
	if _, e := tx.ExecContext(ctx, "DELETE FROM library_sources WHERE library_id=?", id); e != nil {
		return e
	}
	for i, source := range sources {
		if _, e := tx.ExecContext(ctx, "INSERT INTO library_sources VALUES(?,?,?)", id, source, i); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) UpdateLibrary(ctx context.Context, id string, expected int64, name *string, sources *[]string) (model.Library, error) {
	tx, e := s.writer.BeginTx(ctx, nil)
	if e != nil {
		return model.Library{}, e
	}
	defer tx.Rollback()
	l, e := libraryRow(tx.QueryRowContext(ctx, "SELECT "+libraryColumns+" FROM libraries l WHERE l.id=?", id))
	if e != nil {
		return l, e
	}
	if l.Revision != expected {
		return l, model.ErrConflict
	}
	if name != nil {
		l.Name = *name
	}
	policyChanged := sources != nil && strings.Join(l.Sources, ",") != strings.Join(*sources, ",")
	if policyChanged {
		l.Sources = append([]string{}, (*sources)...)
		l.PolicyRevision++
		if e = setSources(ctx, tx, id, l.Sources); e != nil {
			return l, e
		}
	}
	l.Revision++
	if _, e = tx.ExecContext(ctx, "UPDATE libraries SET name=?,revision=?,policy_revision=? WHERE id=?", l.Name, l.Revision, l.PolicyRevision, id); e != nil {
		return l, e
	}
	if policyChanged {
		// A new generation invalidates any in-flight automatic evaluation. Manual
		// selections and their jobs are deliberately not replaced by policy changes.
		_, e = tx.ExecContext(ctx, `INSERT INTO metadata_jobs(subject_id,requested_generation,state,requested_at)
 SELECT s.id,1,'queued',? FROM metadata_subjects s LEFT JOIN metadata_bindings b ON b.subject_id=s.id
 WHERE s.library_id=? AND (b.selection IS NULL OR b.selection='automatic')
 ON CONFLICT(subject_id) DO UPDATE SET requested_generation=metadata_jobs.requested_generation+1,state='queued',requested_at=excluded.requested_at,attempts=0`, time.Now().UnixMilli(), id)
		if e != nil {
			return l, e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE revisions SET catalog=catalog+1 WHERE singleton=1"); e != nil {
		return l, e
	}
	return l, tx.Commit()
}

const fileColumns = `f.id,f.library_id,f.relative_path,f.fingerprint,f.state,f.organization_reason,l.root_path,l.root_identity,l.kind,f.size,f.modified_at,f.added_at,coalesce(o.series_id,''),coalesce(se.local_title,''),coalesce(o.season_number,0),coalesce(o.episode_number,0),coalesce(ms.id,'')`
const fileJoins = ` FROM media_files f JOIN libraries l ON l.id=f.library_id LEFT JOIN file_organization o ON o.media_file_id=f.id LEFT JOIN series se ON se.id=o.series_id LEFT JOIN metadata_subjects ms ON ms.media_file_id=f.id OR ms.series_id=o.series_id `

func fileRow(row rowScanner) (model.MediaFile, error) {
	var f model.MediaFile
	e := row.Scan(&f.ID, &f.LibraryID, &f.Relative, &f.Fingerprint, &f.State, &f.Reason, &f.RootPath, &f.RootIdentity, &f.Kind, &f.Size, &f.ModifiedAt, &f.AddedAt, &f.SeriesID, &f.SeriesTitle, &f.Season, &f.Episode, &f.SubjectID)
	return f, notFound(e)
}
func (s *Store) MediaFile(ctx context.Context, id string) (model.MediaFile, error) {
	return fileRow(s.reader.QueryRowContext(ctx, "SELECT "+fileColumns+fileJoins+" WHERE f.id=?", id))
}

// MediaFiles is a bounded internal inspection/read primitive, not the catalog
// HTTP projection (stage 04 supplies that with query-bound cursors).
func (s *Store) MediaFiles(ctx context.Context, library, after string, limit int) ([]model.MediaFile, error) {
	if limit < 1 || limit > 256 {
		return nil, model.ErrInvalid
	}
	rows, e := s.reader.QueryContext(ctx, "SELECT "+fileColumns+fileJoins+" WHERE f.library_id=? AND f.id>? ORDER BY f.id LIMIT ?", library, after, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	list := []model.MediaFile{}
	for rows.Next() {
		f, e := fileRow(rows)
		if e != nil {
			return nil, e
		}
		list = append(list, f)
	}
	return list, rows.Err()
}

func (s *Store) Ready(ctx context.Context) bool { return s.reader.PingContext(ctx) == nil }
func (s *Store) ConfiguredProviders(ctx context.Context) (map[string]bool, error) {
	rows, e := s.reader.QueryContext(ctx, "SELECT source,secrets IS NOT NULL FROM provider_settings")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	m := map[string]bool{}
	for rows.Next() {
		var key string
		var value bool
		if e = rows.Scan(&key, &value); e != nil {
			return nil, e
		}
		m[key] = value
	}
	return m, rows.Err()
}
