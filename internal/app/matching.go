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
		err = a.db.QueryRowContext(r.Context(), `SELECT f.filename,f.directory_id FROM media_files f JOIN media_directories d ON d.id=f.directory_id WHERE f.id=? AND d.kind='movie'`, target.ID).Scan(&target.Title, &target.DirectoryID)
	case "series":
		err = a.db.QueryRowContext(r.Context(), `SELECT s.title,s.directory_id FROM series s WHERE s.id=? AND EXISTS(SELECT 1 FROM file_organization o WHERE o.series_id=s.id)`, target.ID).Scan(&target.Title, &target.DirectoryID)
		if errors.Is(err, sql.ErrNoRows) {
			// Unorganized series files can have a title association without
			// inventing season/episode numbers or merging independent files.
			err = a.db.QueryRowContext(r.Context(), `SELECT f.filename,f.directory_id FROM media_files f JOIN media_directories d ON d.id=f.directory_id LEFT JOIN file_organization o ON o.media_file_id=f.id WHERE f.id=? AND d.kind='series' AND o.series_id IS NULL`, target.ID).Scan(&target.Title, &target.DirectoryID)
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
	ctx, cancel := context.WithTimeout(r.Context(), 65*time.Second)
	defer cancel()
	if r.Method == "GET" {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" || len(query) > 200 {
			fail(w, 400, "请输入 1–200 字节的作品标题")
			return
		}
		sources, _, err := a.directorySources(ctx, target.DirectoryID)
		if err != nil {
			fail(w, 500, "无法读取目录资料源")
			return
		}
		candidates := []TitleMetadata{}
		warnings := []string{}
		more, succeeded := false, false
		for _, source := range sources {
			client := a.metadata.provider(source)
			if !client.configured() {
				continue
			}
			sourceCtx, stop := context.WithTimeout(ctx, 20*time.Second)
			items, complete, err := client.search(sourceCtx, target.Kind, query)
			stop()
			if err != nil {
				warnings = append(warnings, err.Error())
				continue
			}
			succeeded = true
			candidates = append(candidates, items...)
			more = more || !complete
		}
		if !succeeded {
			if len(warnings) == 0 {
				warnings = append(warnings, "目录未启用已配置的资料源；请检查 TMDB / TVDB / MetaTube 设置")
			}
			fail(w, 503, strings.Join(warnings, "；"))
			return
		}
		// Read-only search preserves priority order and reports partial failures.
		respond(w, 200, map[string]any{"items": candidates, "moreResults": more, "warnings": warnings})
		return
	}
	var body struct {
		TMDBID   int64  `json:"tmdbId"`
		Source   string `json:"source"`
		SourceID int64  `json:"sourceId"`
		Kind     string `json:"kind"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Source == "" && body.TMDBID > 0 && body.SourceID == 0 {
		body.Source, body.SourceID = "tmdb", body.TMDBID // Legacy API clients.
	}
	body.Source = strings.ToLower(body.Source)
	if !validMetadataSource(body.Source) || body.SourceID <= 0 || body.Kind != target.Kind || (body.TMDBID != 0 && (body.Source != "tmdb" || body.TMDBID != body.SourceID)) {
		fail(w, 400, "候选作品类型或标识不符")
		return
	}
	if !a.metadata.operation.TryLock() {
		fail(w, 409, "后台匹配或其他确认正在进行，请稍后重试")
		return
	}
	defer a.metadata.operation.Unlock()
	client := a.metadata.provider(body.Source)
	sources, _, err := a.directorySources(ctx, target.DirectoryID)
	if err != nil {
		fail(w, 500, "无法读取目录资料源")
		return
	}
	if !sourceEnabled(sources, body.Source) {
		fail(w, 400, "候选资料源未在此目录启用")
		return
	}
	sourceCtx, stop := context.WithTimeout(ctx, 20*time.Second)
	m, poster, typ, warning, err := fetchMetadataDetails(sourceCtx, client, target.Kind, body.SourceID)
	stop()
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
