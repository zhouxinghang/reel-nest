package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type metadataService struct {
	client    *tmdbClient
	wake      chan struct{}
	operation sync.Mutex // Serialize association changes, not library reads.
	mu        sync.Mutex
	running   bool
	lastError string
	finished  int64
	revision  int64
}

type metadataTarget struct{ Kind, ID, Title string }
type metadataAssociation struct {
	Metadata  *TitleMetadata `json:"metadata"`
	Selection string         `json:"selection"`
	Status    string         `json:"status"`
	Error     string         `json:"error"`
}

func (a *App) initializeMetadata() error {
	_, err := a.db.Exec(`CREATE TABLE IF NOT EXISTS metadata_matches (
  kind TEXT NOT NULL CHECK(kind IN ('movie','series')), target_id TEXT NOT NULL,
  tmdb_id INTEGER NOT NULL DEFAULT 0, selection TEXT NOT NULL DEFAULT 'automatic',
  status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', PRIMARY KEY(kind,target_id)
 );
 CREATE TABLE IF NOT EXISTS metadata_cache (
  kind TEXT NOT NULL, tmdb_id INTEGER NOT NULL, payload TEXT NOT NULL,
  expires_at INTEGER NOT NULL, poster BLOB, poster_type TEXT NOT NULL,
  PRIMARY KEY(kind,tmdb_id)
 );`)
	if err != nil {
		return err
	}
	client, err := newTMDB(a.config)
	if err != nil {
		return err
	}
	a.config.TMDBToken = ""
	a.metadata = &metadataService{client: client, wake: make(chan struct{}, 1)}
	if _, err = a.db.Exec("DELETE FROM metadata_cache WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return err
	}
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				// Cache housekeeping is not a media-directory scan. Never serve expired
				// payloads, even between purges or while the upstream service is offline.
				if result, err := a.db.ExecContext(a.ctx, "DELETE FROM metadata_cache WHERE expires_at<=?", time.Now().Unix()); err == nil {
					if count, _ := result.RowsAffected(); count > 0 {
						a.metadata.mu.Lock()
						a.metadata.revision++
						a.metadata.mu.Unlock()
					}
				}
			case <-a.metadata.wake:
				a.matchLibrary()
			}
		}
	}()
	a.queueMetadata()
	return nil
}

func (a *App) queueMetadata() {
	if a.metadata == nil || a.metadata.client.token == "" {
		return
	}
	select {
	case a.metadata.wake <- struct{}{}:
	default:
	}
}

func (a *App) metadataStatus(w http.ResponseWriter, r *http.Request) {
	s := a.metadata
	s.mu.Lock()
	running, lastError, finished, revision := s.running, s.lastError, s.finished, s.revision
	s.mu.Unlock()
	s.client.mu.Lock()
	cooldown := s.client.cooldown.Unix()
	s.client.mu.Unlock()
	if cooldown < 0 {
		cooldown = 0
	}
	respond(w, 200, map[string]any{"configured": s.client.token != "", "running": running || len(s.wake) > 0, "error": lastError, "finishedAt": finished, "revision": revision, "cooldownUntil": cooldown})
}

func (a *App) refreshMetadata(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if !decode(w, r, &body) {
		return
	}
	if a.metadata.client.token == "" {
		fail(w, 503, "未配置 TMDB 凭据；本地浏览与播放不受影响")
		return
	}
	a.metadata.mu.Lock()
	busy := a.metadata.running || len(a.metadata.wake) > 0
	a.metadata.mu.Unlock()
	if busy {
		fail(w, 409, "影片资料获取正在进行，请稍后查看结果")
		return
	}
	a.queueMetadata()
	respond(w, 202, map[string]string{"status": "queued"})
}

func (a *App) metadataTargets(ctx context.Context) ([]metadataTarget, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT 'movie', f.id, f.filename FROM media_files f JOIN media_directories d ON d.id=f.directory_id WHERE d.kind='movie' AND f.available=1
 UNION SELECT 'series',s.id,s.title FROM series s JOIN file_organization o ON o.series_id=s.id JOIN media_files f ON f.id=o.media_file_id WHERE f.available=1
 UNION SELECT 'series',f.id,f.filename FROM media_files f JOIN media_directories d ON d.id=f.directory_id LEFT JOIN file_organization o ON o.media_file_id=f.id JOIN metadata_matches m ON m.kind='series' AND m.target_id=f.id WHERE f.available=1 AND d.kind='series' AND o.series_id IS NULL AND m.tmdb_id>0 ORDER BY 1,2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := []metadataTarget{}
	for rows.Next() {
		var target metadataTarget
		if err := rows.Scan(&target.Kind, &target.ID, &target.Title); err != nil {
			return nil, err
		}
		if target.Kind == "movie" {
			target.Title = strings.TrimSuffix(target.Title, filepath.Ext(target.Title))
		}
		targets = append(targets, target)
	}
	return targets, rows.Err()
}

func (a *App) matchLibrary() {
	s := a.metadata
	s.mu.Lock()
	s.running = true
	s.lastError = ""
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.running = false; s.finished = time.Now().Unix(); s.revision++; s.mu.Unlock() }()
	targets, err := a.metadataTargets(a.ctx)
	if err != nil {
		s.mu.Lock()
		s.lastError = "无法读取待匹配媒体"
		s.mu.Unlock()
		return
	}
	for _, target := range targets {
		if a.ctx.Err() != nil {
			return
		}
		s.operation.Lock()
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		err := a.matchTarget(ctx, target)
		cancel()
		s.operation.Unlock()
		if err != nil {
			s.mu.Lock()
			s.lastError = err.Error()
			s.mu.Unlock()
		}
	}
}

func (a *App) matchTarget(ctx context.Context, target metadataTarget) error {
	var id int64
	var selection string
	err := a.db.QueryRowContext(ctx, "SELECT tmdb_id,selection FROM metadata_matches WHERE kind=? AND target_id=?", target.Kind, target.ID).Scan(&id, &selection)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("无法读取影片资料关联")
	}
	if id > 0 {
		var valid int
		if err := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM metadata_cache WHERE kind=? AND tmdb_id=? AND expires_at>?", target.Kind, id, time.Now().Unix()).Scan(&valid); err != nil {
			return errors.New("无法读取影片资料缓存")
		}
		if valid > 0 {
			return nil
		}
	} else {
		query, year := matchQuery(target.Title)
		candidates, complete, err := a.metadata.client.search(ctx, target.Kind, query)
		if err != nil {
			return a.metadataFailure(ctx, target, "failed", err)
		}
		if complete && normalizedTitle(query) != "" {
			for _, candidate := range candidates {
				if (normalizedTitle(candidate.Title) == normalizedTitle(query) || normalizedTitle(candidate.OriginalTitle) == normalizedTitle(query)) && (year == "" || year == candidate.Year) {
					if id != 0 {
						id = 0
						break
					}
					id = candidate.TMDBID
				}
			}
		}
		if id == 0 {
			return a.metadataFailure(ctx, target, "unmatched", errors.New("没有唯一可靠的匹配，请手动搜索并选择影片资料"))
		}
	}
	m, poster, typ, warning, err := a.metadata.client.details(ctx, target.Kind, id)
	if err != nil {
		return a.metadataFailure(ctx, target, "failed", err)
	}
	if selection == "" {
		selection = "automatic"
	}
	if err := a.saveMetadata(ctx, target, m, poster, typ, selection, warning); err != nil {
		return errors.New("无法保存影片资料，本地媒体未改变")
	}
	return nil
}

func (a *App) metadataFailure(ctx context.Context, target metadataTarget, status string, cause error) error {
	// Preserve the chosen ID even if the cache expires or TMDB stops responding.
	// In particular, an explicit manual choice must never revert to auto-search.
	_, err := a.db.ExecContext(ctx, `INSERT INTO metadata_matches(kind,target_id,status,error) VALUES(?,?,?,?) ON CONFLICT(kind,target_id) DO UPDATE SET status=excluded.status,error=excluded.error`, target.Kind, target.ID, status, cause.Error())
	if err != nil {
		return errors.New("无法保存资料获取状态")
	}
	return cause
}

func (a *App) saveMetadata(ctx context.Context, target metadataTarget, m TitleMetadata, poster []byte, typ, selection, warning string) error {
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO metadata_cache(kind,tmdb_id,payload,expires_at,poster,poster_type) VALUES(?,?,?,?,?,?) ON CONFLICT(kind,tmdb_id) DO UPDATE SET payload=excluded.payload,expires_at=excluded.expires_at,poster=excluded.poster,poster_type=excluded.poster_type`, target.Kind, m.TMDBID, string(payload), m.ExpiresAt, poster, typ); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO metadata_matches(kind,target_id,tmdb_id,selection,status,error) VALUES(?,?,?,?,'matched',?) ON CONFLICT(kind,target_id) DO UPDATE SET tmdb_id=excluded.tmdb_id,selection=excluded.selection,status=excluded.status,error=excluded.error`, target.Kind, target.ID, m.TMDBID, selection, warning); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) attachMetadata(ctx context.Context, files []MediaFile) error {
	rows, err := a.db.QueryContext(ctx, `SELECT m.kind,m.target_id,m.selection,m.status,m.error,COALESCE(c.payload,'') FROM metadata_matches m LEFT JOIN metadata_cache c ON c.kind=m.kind AND c.tmdb_id=m.tmdb_id AND c.expires_at>?`, time.Now().Unix())
	if err != nil {
		return err
	}
	defer rows.Close()
	associations := map[string]metadataAssociation{}
	for rows.Next() {
		var kind, id, payload string
		var association metadataAssociation
		if err := rows.Scan(&kind, &id, &association.Selection, &association.Status, &association.Error, &payload); err != nil {
			return err
		}
		if payload != "" {
			var m TitleMetadata
			if json.Unmarshal([]byte(payload), &m) == nil {
				association.Metadata = &m
			}
		}
		if association.Metadata == nil && association.Status == "matched" {
			association.Status = "expired"
			association.Error = "影片资料缓存已到期，待重新获取；本地媒体与观看进度不受影响"
		}
		associations[kind+":"+id] = association
	}
	if rows.Err() != nil {
		return rows.Err()
	}
	for i := range files {
		f := &files[i]
		id := f.ID
		if f.Episode != nil {
			id = f.Episode.SeriesID
		}
		f.Match = associations[f.Kind+":"+id]
		if f.Match.Status == "" {
			f.Match.Status = "unmatched"
		}
		f.Matched = f.Match.Metadata != nil
	}
	return nil
}

func (a *App) metadataPoster(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		fail(w, 404, "海报不可用")
		return
	}
	var poster []byte
	var typ string
	err = a.db.QueryRowContext(r.Context(), "SELECT poster,poster_type FROM metadata_cache WHERE kind=? AND tmdb_id=? AND expires_at>?", r.PathValue("kind"), id, time.Now().Unix()).Scan(&poster, &typ)
	if err != nil || len(poster) == 0 {
		fail(w, 404, "海报不可用或已到期")
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Content-Length", strconv.Itoa(len(poster)))
	if r.Method != "HEAD" {
		_, _ = w.Write(poster)
	}
}
