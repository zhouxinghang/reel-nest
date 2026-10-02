package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"reelnest/internal/model"
)

// All page predicates and keys are computed by SQL over light columns, before
// reading one bounded page of cards. No credits, artwork bytes or full-library
// Go slices are involved. The substring predicate deliberately is NOT FTS.
func catalogSelection(q model.CatalogQuery, now int64) (string, []any) {
	args := []any{}
	where := []string{"1=1"}
	joins := ""
	// Do not rely on SQLite eliminating unused LEFT JOINs. In particular an
	// expiry-index nested loop can make a global event-time page quadratic after
	// a metadata refresh. Only include relations needed for membership or order;
	// readCards joins household state/metadata for the bounded page afterwards.
	if q.View == "browse" || q.View == "recent" || q.View == "" {
		joins += " LEFT JOIN file_organization o ON o.media_file_id=e.media_file_id"
	}
	if q.Sort == "title" || q.Sort == "" || q.Query != "" || q.View == "unmatched" {
		joins += " LEFT JOIN metadata_bindings b ON b.subject_id=e.subject_id LEFT JOIN metadata_snapshots sn ON sn.remote_title_id=b.remote_title_id AND sn.expires_at>?"
		args = append(args, now)
	}
	if q.View == "favorites" {
		joins += " LEFT JOIN media_favorites mf ON mf.media_file_id=e.media_file_id LEFT JOIN series_favorites sf ON sf.series_id=e.series_id"
	}
	if q.View == "continue" {
		joins += " JOIN watch_progress p ON p.media_file_id=e.media_file_id"
	}

	if q.LibraryID != "" {
		where = append(where, "e.library_id=?")
		args = append(args, q.LibraryID)
	}
	if q.Kind != "" {
		where = append(where, "e.kind=?")
		args = append(args, q.Kind)
	}
	switch q.View {
	case "unmatched":
		where = append(where, "e.media_file_id IS NOT NULL AND sn.remote_title_id IS NULL")
	case "favorites":
		where = append(where, "coalesce(mf.favorited_at,sf.favorited_at) IS NOT NULL")
	case "continue":
		where = append(where, "p.completed=0 AND p.position>0 AND e.available_file_count>0")
	default:
		where = append(where, "(e.series_id IS NOT NULL OR o.media_file_id IS NULL)")
	}
	if q.Query != "" {
		where = append(where, `(instr(e.local_search_key,?)>0 OR instr(sn.search_key,?)>0 OR (e.series_id IS NOT NULL AND EXISTS(SELECT 1 FROM catalog_entries child WHERE child.subject_id=e.subject_id AND child.media_file_id IS NOT NULL AND instr(child.local_search_key,?)>0)))`)
		term := searchKey(q.Query)
		args = append(args, term, term, term)
	}
	key := "coalesce(sn.title_sort_key,e.title_sort_key)"
	switch q.Sort {
	case "recent":
		key = "printf('%020d',9223372036854775807-e.added_at)"
	case "favorite":
		key = "printf('%020d',9223372036854775807-coalesce(mf.favorited_at,sf.favorited_at))"
	case "progress":
		key = "printf('%020d',9223372036854775807-p.updated_at)"
	}
	return `SELECT e.entry_key,e.library_id,` + key + ` AS sort_key FROM catalog_entries e` + joins + ` WHERE ` + strings.Join(where, " AND "), args
}

// Revisions and expiry are read from the same SQLite snapshot as the page. A
// library's nearest fresh expiry is conservative (including currently excluded
// rows, which can enter unmatched/search when they expire). No timer repairs.
func readCatalogVersion(ctx context.Context, q queryReader, view, library string, expected, now int64) (int64, int64, error) {
	column := "catalog"
	if view == "favorites" {
		column += "+favorites"
	}
	if view == "continue" {
		column += "+progress"
	}
	var rev int64
	if err := q.QueryRowContext(ctx, "SELECT "+column+" FROM revisions WHERE singleton=1").Scan(&rev); err != nil {
		return 0, 0, err
	}
	if expected != 0 && expected != rev {
		return rev, 0, model.ErrCursor
	}
	var expiry *int64
	query := `SELECT min(sn.expires_at) FROM metadata_snapshots sn WHERE sn.expires_at>? AND EXISTS(SELECT 1 FROM metadata_bindings b JOIN metadata_subjects sub ON sub.id=b.subject_id WHERE b.remote_title_id=sn.remote_title_id`
	args := []any{now}
	if library != "" {
		query += " AND sub.library_id=?"
		args = append(args, library)
	}
	query += ")"
	if err := q.QueryRowContext(ctx, query, args...).Scan(&expiry); err != nil {
		return rev, 0, err
	}
	expires := now + 300000
	if expiry != nil {
		expires = min(expires, *expiry)
	}
	return rev, expires, nil
}

func readCards(ctx context.Context, q queryReader, keys []string, now int64) ([]model.CatalogCard, error) {
	cards := []model.CatalogCard{}
	if len(keys) == 0 {
		return cards, nil
	}
	body, _ := json.Marshal(keys)
	rows, err := q.QueryContext(ctx, `SELECT e.entry_key,e.library_id,coalesce(e.media_file_id,''),coalesce(e.series_id,''),coalesce(e.subject_id,''),e.kind,coalesce(sn.title,e.local_title),e.added_at,e.season_count,e.episode_count,e.file_count,e.available_file_count,
 coalesce(f.relative_path,''),coalesce(f.state,''),coalesce(o.series_id,''),coalesce(se.local_title,''),coalesce(o.season_number,0),coalesce(o.episode_number,0),coalesce(mf.favorited_at,sf.favorited_at),p.position,p.duration,p.completed,p.updated_at,
 coalesce(sn.remote_title_id,''),coalesce(sn.title,''),coalesce(sn.original_title,''),sn.year,coalesce(sn.expires_at,0),coalesce(pc.source,''),
 (SELECT id FROM assets a WHERE a.remote_title_id=sn.remote_title_id AND a.credit_id IS NULL AND kind='primary'),
 (SELECT id FROM assets a WHERE a.remote_title_id=sn.remote_title_id AND a.credit_id IS NULL AND kind='thumb'),
 (SELECT id FROM assets a WHERE a.remote_title_id=sn.remote_title_id AND a.credit_id IS NULL AND kind='backdrop')
 FROM catalog_entries e LEFT JOIN media_files f ON f.id=e.media_file_id LEFT JOIN file_organization o ON o.media_file_id=f.id LEFT JOIN series se ON se.id=o.series_id
 LEFT JOIN media_favorites mf ON mf.media_file_id=e.media_file_id LEFT JOIN series_favorites sf ON sf.series_id=e.series_id LEFT JOIN watch_progress p ON p.media_file_id=e.media_file_id
 LEFT JOIN metadata_bindings b ON b.subject_id=e.subject_id LEFT JOIN metadata_snapshots sn ON sn.remote_title_id=b.remote_title_id AND sn.expires_at>?
 LEFT JOIN remote_titles rt ON rt.id=sn.remote_title_id LEFT JOIN provider_catalogs pc ON pc.id=rt.catalog_id
 WHERE e.entry_key IN(SELECT value FROM json_each(?))`, now, string(body))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := map[string]model.CatalogCard{}
	for rows.Next() {
		var c model.CatalogCard
		var t model.Title
		var position, duration *float64
		var completed *bool
		var updated *int64
		var primary, thumb, backdrop *string
		if err = rows.Scan(&c.EntryKey, &c.LibraryID, &c.MediaFileID, &c.SeriesID, &c.SubjectID, &c.Kind, &c.Title, &c.AddedAt, &c.SeasonCount, &c.EpisodeCount, &c.FileCount, &c.AvailableFileCount, &c.Filename, &c.State, &c.ParentSeriesID, &c.ParentSeriesTitle, &c.Season, &c.Episode, &c.FavoritedAt, &position, &duration, &completed, &updated, &c.RemoteTitleID, &t.Title, &t.OriginalTitle, &t.Year, &t.ExpiresAt, &t.Ref.Source, &primary, &thumb, &backdrop); err != nil {
			return nil, err
		}
		c.Title = displayTitle(c.Title)
		if i := strings.LastIndex(c.Filename, "/"); i >= 0 {
			c.Filename = c.Filename[i+1:]
		}
		c.Favorite = c.FavoritedAt != nil
		if position != nil {
			c.Progress = &model.Progress{Position: *position, Duration: *duration, Completed: *completed, UpdatedAt: *updated}
		}
		c.Match.Artwork = map[string]string{}
		c.Match.Target.ID = c.SubjectID
		if t.ExpiresAt > now {
			c.Match.Title = &t
			c.Match.CacheState = "fresh"
		}
		for k, v := range map[string]*string{"primary": primary, "thumb": thumb, "backdrop": backdrop} {
			if v != nil {
				c.Match.Artwork[k] = *v
			}
		}
		byKey[c.EntryKey] = c
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, key := range keys {
		if c, ok := byKey[key]; ok {
			cards = append(cards, c)
		}
	}
	return cards, nil
}

func readCatalog(ctx context.Context, tx queryReader, q model.CatalogQuery, after model.CatalogPosition, limit int, now int64) (model.CatalogPage, error) {
	page := model.CatalogPage{Items: []model.CatalogCard{}}
	query, args := catalogSelection(q, now)
	query = `WITH matches AS (` + query + `) SELECT entry_key,sort_key FROM matches WHERE (sort_key,entry_key)>(?,?) ORDER BY sort_key,entry_key LIMIT ?`
	args = append(args, after.Key, after.ID, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	keys := []string{}
	for rows.Next() {
		var pos model.CatalogPosition
		if err = rows.Scan(&pos.ID, &pos.Key); err != nil {
			break
		}
		if len(keys) == limit {
			page.More = true
			break
		}
		keys = append(keys, pos.ID)
		page.Last = pos
	}
	err = errors.Join(err, rows.Err())
	rows.Close()
	if err != nil {
		return page, err
	}
	page.Items, err = readCards(ctx, tx, keys, now)
	return page, err
}
func (s *Store) Catalog(ctx context.Context, q model.CatalogQuery, after model.CatalogPosition, limit int, expected, now int64) (model.CatalogPage, error) {
	var page model.CatalogPage
	if limit < 1 || limit > 100 {
		return page, model.ErrInvalid
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	rev, expiry, err := readCatalogVersion(ctx, tx, q.View, q.LibraryID, expected, now)
	if err != nil {
		return page, err
	}
	if q.LibraryID != "" {
		var id string
		if err = tx.QueryRowContext(ctx, `SELECT id FROM libraries WHERE id=?`, q.LibraryID).Scan(&id); err != nil {
			return page, notFound(err)
		}
	}
	page, err = readCatalog(ctx, tx, q, after, limit, now)
	if err != nil {
		return page, err
	}
	page.Revision = rev
	page.Expires = expiry
	return page, tx.Commit()
}
func (s *Store) CatalogGroups(ctx context.Context, q model.CatalogQuery, after model.CatalogPosition, limit int, expected, now int64) (model.CatalogGroups, error) {
	page := model.CatalogGroups{Items: []model.CatalogGroup{}}
	if limit < 1 || limit > 100 {
		return page, model.ErrInvalid
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	page.Revision, page.Expires, err = readCatalogVersion(ctx, tx, q.View, "", expected, now)
	if err != nil {
		return page, err
	}
	query, args := catalogSelection(q, now)
	query = `WITH matches AS (` + query + `), ranked AS (SELECT *,count(*) OVER(PARTITION BY library_id) AS total,row_number() OVER(PARTITION BY library_id ORDER BY sort_key,entry_key) AS n FROM matches)
 SELECT l.id,l.name,l.kind,r.total,r.sort_key FROM ranked r JOIN libraries l ON l.id=r.library_id WHERE n=1 AND (r.sort_key,l.id)>(?,?) ORDER BY r.sort_key,l.id LIMIT ?`
	args = append(args, after.Key, after.ID, limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var g model.CatalogGroup
		if err = rows.Scan(&g.Library.ID, &g.Library.Name, &g.Library.Kind, &g.Total, &g.Position.Key); err != nil {
			break
		}
		g.Position.ID = g.Library.ID
		if len(page.Items) == limit {
			page.More = true
			break
		}
		page.Items = append(page.Items, g)
	}
	err = errors.Join(err, rows.Err())
	rows.Close()
	if err != nil {
		return page, err
	}
	return page, tx.Commit()
}

// A file detail executes 7 target-bound SELECTs at most (including full fresh
// credits), independent of unrelated titles. The HTTP identity check is separate.
func readMediaDetail(ctx context.Context, q queryReader, id string, now int64) (model.MediaDetail, error) {
	var d model.MediaDetail
	cards, err := readCards(ctx, q, []string{"media:" + id}, now)
	if err != nil {
		return d, err
	}
	if len(cards) == 0 {
		return d, model.ErrNotFound
	}
	d.Card = cards[0]
	d.File, err = fileRow(q.QueryRowContext(ctx, "SELECT "+fileColumns+fileJoins+" WHERE f.id=?", id))
	if err != nil {
		return d, err
	}
	d.Match, err = readMatch(ctx, q, d.Card.SubjectID, now)
	return d, err
}
func (s *Store) MediaDetail(ctx context.Context, id string, now int64) (model.MediaDetail, error) {
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return model.MediaDetail{}, err
	}
	defer tx.Rollback()
	d, err := readMediaDetail(ctx, tx, id, now)
	if err != nil {
		return d, err
	}
	return d, tx.Commit()
}
func (s *Store) SeriesDetail(ctx context.Context, id string, after, limit int, expected, now int64) (model.SeriesDetail, error) {
	d := model.SeriesDetail{Seasons: []model.SeasonSummary{}}
	if limit < 1 || limit > 100 {
		return d, model.ErrInvalid
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return d, err
	}
	defer tx.Rollback()
	cards, err := readCards(ctx, tx, []string{"series:" + id}, now)
	if err != nil {
		return d, err
	}
	if len(cards) == 0 {
		return d, model.ErrNotFound
	}
	d.Card = cards[0]
	d.Revision, d.Expires, err = readCatalogVersion(ctx, tx, "browse", d.Card.LibraryID, expected, now)
	if err != nil {
		return d, err
	}
	d.Match, err = readMatch(ctx, tx, d.Card.SubjectID, now)
	if err != nil {
		return d, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT season_number,count(DISTINCT episode_number),count(*) FROM file_organization WHERE series_id=? AND season_number>? GROUP BY season_number ORDER BY season_number LIMIT ?`, id, after, limit+1)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var v model.SeasonSummary
		if err = rows.Scan(&v.Number, &v.EpisodeCount, &v.FileCount); err != nil {
			break
		}
		if len(d.Seasons) == limit {
			d.More = true
			break
		}
		d.Seasons = append(d.Seasons, v)
	}
	err = errors.Join(err, rows.Err())
	rows.Close()
	if err != nil {
		return d, err
	}
	return d, tx.Commit()
}
func (s *Store) Episodes(ctx context.Context, id string, season, afterEpisode int, afterID string, limit int, expected, now int64) (model.CatalogPage, error) {
	page := model.CatalogPage{Items: []model.CatalogCard{}}
	if limit < 1 || limit > 100 {
		return page, model.ErrInvalid
	}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	var library string
	if err = tx.QueryRowContext(ctx, `SELECT library_id FROM series WHERE id=?`, id).Scan(&library); err != nil {
		return page, notFound(err)
	}
	page.Revision, page.Expires, err = readCatalogVersion(ctx, tx, "browse", library, expected, now)
	if err != nil {
		return page, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT media_file_id FROM file_organization WHERE series_id=? AND season_number=? AND (episode_number,media_file_id)>(?,?) ORDER BY episode_number,media_file_id LIMIT ?`, id, season, afterEpisode, afterID, limit+1)
	if err != nil {
		return page, err
	}
	keys := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		if len(keys) == limit {
			page.More = true
			break
		}
		keys = append(keys, "media:"+id)
	}
	err = errors.Join(err, rows.Err())
	rows.Close()
	if err != nil {
		return page, err
	}
	page.Items, err = readCards(ctx, tx, keys, now)
	if err != nil {
		return page, err
	}
	return page, tx.Commit()
}
func (s *Store) Home(ctx context.Context, after string, expected, now int64) (model.Home, error) {
	h := model.Home{Libraries: []model.LibraryPreview{}, Continue: []model.CatalogCard{}, Recent: []model.CatalogCard{}}
	tx, err := s.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return h, err
	}
	defer tx.Rollback()
	h.Revision, h.Expires, err = readCatalogVersion(ctx, tx, "browse", "", expected, now)
	if err != nil {
		return h, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,name,kind FROM libraries WHERE id>? ORDER BY id LIMIT 21`, after)
	if err != nil {
		return h, err
	}
	for rows.Next() {
		var l model.LibraryPreview
		if err = rows.Scan(&l.Library.ID, &l.Library.Name, &l.Library.Kind); err != nil {
			break
		}
		if len(h.Libraries) == 20 {
			h.More = true
			break
		}
		h.Libraries = append(h.Libraries, l)
	}
	err = errors.Join(err, rows.Err())
	rows.Close()
	if err != nil {
		return h, err
	}
	for i := range h.Libraries {
		p, e := readCatalog(ctx, tx, model.CatalogQuery{LibraryID: h.Libraries[i].Library.ID, View: "recent", Sort: "recent"}, model.CatalogPosition{}, 6, now)
		if e != nil {
			return h, e
		}
		h.Libraries[i].Items = p.Items
	}
	p, err := readCatalog(ctx, tx, model.CatalogQuery{View: "recent", Sort: "recent"}, model.CatalogPosition{}, 20, now)
	if err != nil {
		return h, err
	}
	h.Recent = p.Items
	p, err = readCatalog(ctx, tx, model.CatalogQuery{View: "continue", Sort: "progress"}, model.CatalogPosition{}, 20, now)
	if err != nil {
		return h, err
	}
	h.Continue = p.Items
	return h, tx.Commit()
}
