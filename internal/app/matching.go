package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (a *App) matchingTarget(r *http.Request) (metadataTarget, error) {
	target := metadataTarget{Kind: r.PathValue("kind"), ID: r.PathValue("id")}
	var err error
	switch target.Kind {
	case "movie":
		err = a.db.QueryRowContext(r.Context(), `SELECT f.filename FROM media_files f JOIN media_directories d ON d.id=f.directory_id WHERE f.id=? AND d.kind='movie'`, target.ID).Scan(&target.Title)
	case "series":
		err = a.db.QueryRowContext(r.Context(), `SELECT s.title FROM series s WHERE s.id=? AND EXISTS(SELECT 1 FROM file_organization o WHERE o.series_id=s.id)`, target.ID).Scan(&target.Title)
		if errors.Is(err, sql.ErrNoRows) {
			// Unorganized series files can have a title association without
			// inventing season/episode numbers or merging independent files.
			err = a.db.QueryRowContext(r.Context(), `SELECT f.filename FROM media_files f JOIN media_directories d ON d.id=f.directory_id LEFT JOIN file_organization o ON o.media_file_id=f.id WHERE f.id=? AND d.kind='series' AND o.series_id IS NULL`, target.ID).Scan(&target.Title)
		}
	default:
		err = sql.ErrNoRows
	}
	return target, err
}

func (a *App) matching(w http.ResponseWriter, r *http.Request) {
	target, err := a.matchingTarget(r)
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "对应类型的电影或电视剧不存在")
		return
	}
	if err != nil {
		fail(w, 500, "无法读取匹配对象")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if r.Method == "GET" {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" || len(query) > 200 {
			fail(w, 400, "请输入 1–200 字节的作品标题")
			return
		}
		candidates, complete, err := a.metadata.client.search(ctx, target.Kind, query)
		if err != nil {
			fail(w, 503, err.Error())
			return
		}
		// Search never changes a stored association, including empty results/cancel.
		respond(w, 200, map[string]any{"items": candidates, "moreResults": !complete})
		return
	}
	var body struct {
		TMDBID int64  `json:"tmdbId"`
		Kind   string `json:"kind"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.TMDBID <= 0 || body.Kind != target.Kind {
		fail(w, 400, "候选作品类型或标识不符")
		return
	}
	if !a.metadata.operation.TryLock() {
		fail(w, 409, "后台匹配或其他确认正在进行，请稍后重试")
		return
	}
	defer a.metadata.operation.Unlock()
	m, poster, typ, warning, err := a.metadata.client.details(ctx, target.Kind, body.TMDBID)
	if err != nil {
		fail(w, 503, err.Error())
		return
	}
	// Commit only after the requested typed title was successfully fetched. A
	// network error/cancellation must leave the previous valid state untouched.
	if err := a.saveMetadata(ctx, target, m, poster, typ, "manual", warning); err != nil {
		fail(w, 500, "保存匹配失败，原有资料关联未改变")
		return
	}
	a.metadata.mu.Lock()
	a.metadata.revision++
	a.metadata.mu.Unlock()
	respond(w, 200, metadataAssociation{Metadata: &m, Selection: "manual", Status: "matched", Error: warning})
}
