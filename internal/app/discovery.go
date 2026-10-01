package app

import (
	"database/sql"
	"errors"
	"net/http"
	"time"
)

// Discovery state belongs to the household, not a browser or a stream credential.
func (a *App) initializeDiscovery() error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var columns int
	if err = tx.QueryRow("SELECT COUNT(*) FROM pragma_table_info('media_files') WHERE name='added_at'").Scan(&columns); err != nil {
		return err
	}
	if columns == 0 {
		// Old versions did not record discovery time. Zero means unknown: do not
		// misrepresent file modification time or migration time as first discovery.
		if _, err = tx.Exec("ALTER TABLE media_files ADD COLUMN added_at INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`
 CREATE INDEX IF NOT EXISTS media_files_added ON media_files(added_at DESC, id);
 CREATE TABLE IF NOT EXISTS media_favorites (
  target_id TEXT PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
  favorited_at INTEGER NOT NULL
 );
 CREATE TABLE IF NOT EXISTS series_favorites (
  target_id TEXT PRIMARY KEY REFERENCES series(id) ON DELETE CASCADE,
  favorited_at INTEGER NOT NULL
 );`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) favorite(w http.ResponseWriter, r *http.Request) {
	target, favorites := "media_files", "media_favorites"
	switch r.PathValue("kind") {
	case "media":
	case "series":
		target, favorites = "series", "series_favorites"
	default:
		fail(w, 400, "未知收藏类型")
		return
	}
	var input struct {
		Favorite *bool `json:"favorite"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Favorite == nil {
		fail(w, 400, "请指定收藏状态")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "无法保存收藏，请重试")
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	var found string
	err = tx.QueryRowContext(r.Context(), "SELECT id FROM "+target+" WHERE id=?", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "收藏对象不存在")
		return
	}
	if err != nil {
		fail(w, 500, "无法读取收藏对象")
		return
	}
	var favoritedAt int64
	if *input.Favorite {
		_, err = tx.ExecContext(r.Context(), "INSERT INTO "+favorites+"(target_id,favorited_at) VALUES(?,?) ON CONFLICT(target_id) DO NOTHING", id, time.Now().UnixMilli())
		if err == nil {
			err = tx.QueryRowContext(r.Context(), "SELECT favorited_at FROM "+favorites+" WHERE target_id=?", id).Scan(&favoritedAt)
		}
	} else {
		_, err = tx.ExecContext(r.Context(), "DELETE FROM "+favorites+" WHERE target_id=?", id)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		fail(w, 500, "无法保存收藏，请重试")
		return
	}
	respond(w, 200, map[string]any{"favorite": *input.Favorite, "favoritedAt": favoritedAt})
}
