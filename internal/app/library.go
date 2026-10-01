package app

import (
	"database/sql"
	"errors"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
)

type MediaFile struct {
	AddedAt            int64               `json:"addedAt"`
	Favorite           bool                `json:"favorite"`
	FavoritedAt        int64               `json:"favoritedAt"`
	SeriesFavoritedAt  int64               `json:"-"`
	Match              metadataAssociation `json:"match"`
	Progress           *WatchProgress      `json:"progress"`
	DirectoryPath      string              `json:"directoryPath"`
	Episode            *EpisodeReference   `json:"episode"`
	OrganizationReason string              `json:"organizationReason"`
	ID                 string              `json:"id"`
	DirectoryID        string              `json:"directoryId"`
	Filename           string              `json:"filename"`
	RelativePath       string              `json:"relativePath"`
	Path               string              `json:"path"`
	Kind               string              `json:"kind"`
	Size               int64               `json:"size"`
	ModifiedAt         int64               `json:"modifiedAt"`
	Available          bool                `json:"available"`
	Matched            bool                `json:"matched"`
}

const mediaColumns = `f.id, f.directory_id, f.filename, f.relative_path, d.path, d.kind, f.size, f.modified_at, f.available, COALESCE(s.id,''), COALESCE(s.title,''), COALESCE(o.season_number,0), COALESCE(o.episode_number,0), COALESCE(o.reason,''), COALESCE(p.position,0), COALESCE(p.duration,0), COALESCE(p.completed,0), COALESCE(p.updated_at,0), f.added_at, COALESCE(mf.favorited_at,0), COALESCE(sf.favorited_at,0)`
const mediaFrom = ` FROM media_files f JOIN media_directories d ON d.id=f.directory_id LEFT JOIN file_organization o ON o.media_file_id=f.id LEFT JOIN series s ON s.id=o.series_id LEFT JOIN watch_progress p ON p.media_file_id=f.id LEFT JOIN media_favorites mf ON mf.target_id=f.id LEFT JOIN series_favorites sf ON sf.target_id=s.id`

func scanMedia(row scanner) (MediaFile, error) {
	var file MediaFile
	var root string
	var episode EpisodeReference
	var progress WatchProgress
	err := row.Scan(&file.ID, &file.DirectoryID, &file.Filename, &file.RelativePath, &root, &file.Kind, &file.Size, &file.ModifiedAt, &file.Available, &episode.SeriesID, &episode.SeriesTitle, &episode.SeasonNumber, &episode.EpisodeNumber, &file.OrganizationReason, &progress.Position, &progress.Duration, &progress.Completed, &progress.UpdatedAt, &file.AddedAt, &file.FavoritedAt, &file.SeriesFavoritedAt)
	file.Favorite = file.FavoritedAt > 0
	if progress.UpdatedAt > 0 {
		file.Progress = &progress
	}
	file.Path = filepath.Join(root, file.RelativePath)
	file.DirectoryPath = root
	if episode.SeriesID != "" {
		file.Episode = &episode
	}
	return file, err
}

func (a *App) library(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "movie" && kind != "series" {
		fail(w, 400, "未知媒体类型")
		return
	}
	query := strings.ToLower(r.URL.Query().Get("q"))
	if len(query) > 200 {
		fail(w, 400, "搜索内容过长")
		return
	}
	order := "f.filename, f.relative_path, f.id"
	recent := r.URL.Query().Get("sort") == "recent"
	if value := r.URL.Query().Get("sort"); value != "" && !recent {
		fail(w, 400, "未知排序方式")
		return
	}
	if recent {
		order = "f.added_at DESC, f.id"
	}
	rows, err := a.db.QueryContext(r.Context(), "SELECT "+mediaColumns+mediaFrom+" WHERE (?='' OR d.kind=?) ORDER BY "+order, kind, kind)
	if err != nil {
		fail(w, 500, "无法读取媒体库")
		return
	}
	defer rows.Close()
	files := []MediaFile{}
	for rows.Next() {
		file, err := scanMedia(rows)
		if err != nil {
			fail(w, 500, "无法读取媒体库")
			return
		}
		files = append(files, file)
	}
	if rows.Err() != nil {
		fail(w, 500, "无法读取媒体库")
		return
	}
	rows.Close()
	if err := a.attachMetadata(r.Context(), files); err != nil {
		fail(w, 500, "无法读取影片资料")
		return
	}
	filtered := []MediaFile{}
	for _, file := range files {
		if query == "" || strings.Contains(strings.ToLower(file.Filename), query) || (file.Episode != nil && strings.Contains(strings.ToLower(file.Episode.SeriesTitle), query)) || (file.Match.Metadata != nil && strings.Contains(strings.ToLower(file.Match.Metadata.Title), query)) {
			filtered = append(filtered, file)
		}
	}
	files = filtered
	series := seriesSummaries(files)
	if recent {
		sort.Slice(series, func(i, j int) bool {
			if series[i].AddedAt == series[j].AddedAt {
				return series[i].ID < series[j].ID
			}
			return series[i].AddedAt > series[j].AddedAt
		})
	}
	respond(w, 200, map[string]any{"items": files, "series": series})
}

func (a *App) media(w http.ResponseWriter, r *http.Request) {
	file, err := scanMedia(a.db.QueryRowContext(r.Context(), "SELECT "+mediaColumns+mediaFrom+" WHERE f.id=?", r.PathValue("id")))
	if errors.Is(err, sql.ErrNoRows) {
		fail(w, 404, "媒体文件不存在")
		return
	}
	if err != nil {
		fail(w, 500, "无法读取媒体文件详情")
		return
	}
	files := []MediaFile{file}
	if err := a.attachMetadata(r.Context(), files); err != nil {
		fail(w, 500, "无法读取影片资料")
		return
	}
	respond(w, 200, files[0])
}
