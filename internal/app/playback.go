package app

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type mediaProblem struct {
	status  int
	message string
}

func (e *mediaProblem) Error() string { return e.message }
func reportMediaProblem(w http.ResponseWriter, err error) {
	var problem *mediaProblem
	if errors.As(err, &problem) {
		fail(w, problem.status, problem.message)
	} else {
		fail(w, 500, "无法读取媒体状态")
	}
}

// Open every component through bounded directory handles. The returned file is
// the very handle whose fingerprint was checked, never a second path-based open.
// Callers own both handles. No client-supplied filesystem paths are accepted.
func (a *App) openMedia(ctx context.Context, id string) (*os.Root, *os.File, error) {
	var path, relative, fingerprint, identity string
	var available bool
	err := a.db.QueryRowContext(ctx, `SELECT d.path,f.relative_path,f.fingerprint,d.identity,f.available
 FROM media_files f JOIN media_directories d ON d.id=f.directory_id WHERE f.id=?`, id).Scan(&path, &relative, &fingerprint, &identity, &available)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, &mediaProblem{404, "媒体文件不存在"}
	}
	if err != nil {
		return nil, nil, err
	}
	if !available {
		return nil, nil, &mediaProblem{410, "文件不可用：请检查媒体目录并重新扫描"}
	}
	if !filepath.IsLocal(relative) || filepath.Clean(relative) != relative || !videoExtensions[strings.ToLower(filepath.Ext(relative))] {
		return nil, nil, &mediaProblem{409, "媒体路径无效，拒绝读取"}
	}
	root, info, err := a.openDirectory(path)
	if err != nil {
		return nil, nil, &mediaProblem{409, "媒体目录不可访问：请检查挂载和读取权限"}
	}
	abort := func(message string) (*os.Root, *os.File, error) {
		root.Close()
		return nil, nil, &mediaProblem{409, message}
	}
	if fileIdentity(info) != identity {
		return abort("媒体目录已变化：请恢复原挂载后重试")
	}
	parts := strings.Split(relative, string(os.PathSeparator))
	for _, part := range parts[:len(parts)-1] {
		info, err := root.Lstat(part)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return abort("媒体路径不可用或包含符号链接")
		}
		child, err := root.OpenRoot(part)
		if err != nil {
			return abort("媒体子目录不可访问，请检查读取权限")
		}
		opened, err := child.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			child.Close()
			return abort("媒体路径在读取时发生变化")
		}
		root.Close()
		root = child
	}
	file, err := root.OpenFile(parts[len(parts)-1], os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		root.Close()
		if os.IsNotExist(err) {
			return nil, nil, &mediaProblem{410, "媒体文件已缺失，请重新扫描"}
		}
		return nil, nil, &mediaProblem{409, "媒体文件无法读取：请检查权限、挂载或符号链接"}
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || fileFingerprint(opened) != fingerprint {
		file.Close()
		return abort("媒体文件已变化，拒绝读取其他版本；请重新扫描")
	}
	return root, file, nil
}

var mediaTypes = map[string]string{".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".mkv": "video/x-matroska", ".avi": "video/x-msvideo", ".ts": "video/mp2t", ".m2ts": "video/mp2t", ".mpg": "video/mpeg", ".mpeg": "video/mpeg", ".wmv": "video/x-ms-wmv", ".flv": "video/x-flv"}

func (a *App) content(w http.ResponseWriter, r *http.Request) {
	root, file, err := a.openMedia(r.Context(), r.PathValue("id"))
	if err != nil {
		reportMediaProblem(w, err)
		return
	}
	defer root.Close()
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fail(w, 409, "媒体文件无法读取")
		return
	}
	w.Header().Set("Content-Type", mediaTypes[strings.ToLower(filepath.Ext(info.Name()))])
	w.Header().Set("ETag", `"`+r.PathValue("id")+`"`)
	// ServeContent implements streaming GET/HEAD, multipart ranges and If-Range
	// using the seekable handle, without buffering an entire video in memory.
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

type WatchProgress struct {
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
	Completed bool    `json:"completed"`
	UpdatedAt int64   `json:"updatedAt"`
}

func (a *App) initializePlayback() error {
	_, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS watch_progress (
 media_file_id TEXT PRIMARY KEY REFERENCES media_files(id), position REAL NOT NULL,
 duration REAL NOT NULL, completed INTEGER NOT NULL, updated_at INTEGER NOT NULL
 );`)
	return err
}
func (a *App) progress(w http.ResponseWriter, r *http.Request) {
	var exists bool
	if err := a.db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM media_files WHERE id=?)", r.PathValue("id")).Scan(&exists); err != nil {
		fail(w, 500, "无法读取媒体状态")
		return
	}
	if !exists {
		fail(w, 404, "媒体文件不存在")
		return
	}
	if r.Method == "POST" {
		var input struct {
			Position  *float64 `json:"position"`
			Duration  *float64 `json:"duration"`
			Completed *bool    `json:"completed"`
		}
		if !decode(w, r, &input) {
			return
		}
		if input.Position == nil || input.Duration == nil || input.Completed == nil {
			fail(w, 400, "必须提供完整观看进度")
			return
		}
		p, d := *input.Position, *input.Duration
		if math.IsNaN(p) || math.IsInf(p, 0) || math.IsNaN(d) || math.IsInf(d, 0) || d <= 0 || d > 366*24*3600 || p < 0 || p > d || (*input.Completed && p < d-0.5) {
			fail(w, 400, "观看进度或时长无效")
			return
		}
		saved := WatchProgress{p, d, *input.Completed, time.Now().UnixMilli()}
		_, err := a.db.ExecContext(r.Context(), `INSERT INTO watch_progress(media_file_id,position,duration,completed,updated_at) VALUES(?,?,?,?,?)
  ON CONFLICT(media_file_id) DO UPDATE SET position=excluded.position,duration=excluded.duration,completed=excluded.completed,updated_at=excluded.updated_at`, r.PathValue("id"), p, d, *input.Completed, saved.UpdatedAt)
		if err != nil {
			fail(w, 500, "观看进度保存失败")
			return
		}
		respond(w, 200, saved)
		return
	}
	var saved WatchProgress
	err := a.db.QueryRowContext(r.Context(), "SELECT position,duration,completed,updated_at FROM watch_progress WHERE media_file_id=?", r.PathValue("id")).Scan(&saved.Position, &saved.Duration, &saved.Completed, &saved.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		fail(w, 500, "无法读取观看进度")
		return
	}
	respond(w, 200, saved)
}
func (a *App) playbackStatus(w http.ResponseWriter, r *http.Request) {
	root, file, err := a.openMedia(r.Context(), r.PathValue("id"))
	if err != nil {
		reportMediaProblem(w, err)
		return
	}
	root.Close()
	file.Close()
	a.progress(w, r)
}
