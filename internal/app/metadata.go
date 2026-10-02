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

type TitleMetadata struct {
	Source        string        `json:"source"`
	SourceID      int64         `json:"sourceId"`
	TMDBID        int64         `json:"tmdbId"`
	Kind          string        `json:"kind"`
	Title         string        `json:"title"`
	OriginalTitle string        `json:"originalTitle"`
	Number        string        `json:"number,omitempty"`
	Provider      string        `json:"provider,omitempty"`
	Year          string        `json:"year"`
	Overview      string        `json:"overview"`
	PosterURL     string        `json:"posterUrl"`
	ThumbURL      string        `json:"thumbUrl"`
	BackdropURL   string        `json:"backdropUrl"`
	Artwork       *TitleArtwork `json:"artwork,omitempty"`
	SourceURL     string        `json:"sourceUrl"`
	FetchedAt     int64         `json:"fetchedAt"`
	ExpiresAt     int64         `json:"expiresAt"`
	Credits       *TitleCredits `json:"credits,omitempty"`
}

type metadataService struct {
	imageGate   chan struct{} // Title images never queue behind metadata or person photos.
	photoGate   chan struct{} // Coalesce lazy artwork downloads without blocking metadata writes.
	providers   map[string]metadataProvider
	providersMu sync.RWMutex
	wake        chan struct{}
	operation   sync.Mutex // Serialize association changes, not library reads.
	mu          sync.Mutex
	running     bool
	lastError   string
	finished    int64
	revision    int64
}

type metadataTarget struct{ Kind, ID, Title, DirectoryID string }
type metadataAssociation struct {
	Metadata  *TitleMetadata `json:"metadata"`
	Selection string         `json:"selection"`
	Status    string         `json:"status"`
	Error     string         `json:"error"`
}

func (a *App) initializeMetadata() error {
	if err := a.initializeMetadataStorage(); err != nil {
		return err
	}
	if _, err := a.db.Exec("DELETE FROM metadata_cache WHERE expires_at<=?", time.Now().Unix()); err != nil {
		return err
	}
	if err := a.initializePeopleIndex(); err != nil {
		return err
	}
	providers, err := a.initializeMetadataSettings()
	if err != nil {
		return err
	}
	a.config.TMDBToken, a.config.TVDBAPIKey, a.config.TVDBPIN, a.config.MetaTubeToken, a.config.MetaTubeURL = "", "", "", "", ""
	a.config.ImportLegacyMetadata = nil
	a.metadata = &metadataService{providers: providers, wake: make(chan struct{}, 1), photoGate: make(chan struct{}, 1), imageGate: make(chan struct{}, 1)}
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
				_, _ = a.db.ExecContext(a.ctx, "DELETE FROM person_photos WHERE expires_at<=? AND retry_at<=?", time.Now().Unix(), time.Now().Unix())
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
	if a.metadata == nil || !a.metadata.configured() {
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
	sources := s.sources()
	var cooldown int64
	firstConfigured := true
	for _, source := range sources {
		if source.Configured && (firstConfigured || source.CooldownUntil < cooldown) {
			cooldown, firstConfigured = source.CooldownUntil, false
		}
	}
	respond(w, 200, map[string]any{"configured": s.configured(), "sources": sources, "running": running || len(s.wake) > 0, "error": lastError, "finishedAt": finished, "revision": revision, "cooldownUntil": cooldown})
}

func (a *App) refreshMetadata(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if !decode(w, r, &body) {
		return
	}
	if !a.metadata.configured() {
		fail(w, 503, "未配置影片资料源（TMDB / TVDB / MetaTube）；本地浏览与播放不受影响")
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
	rows, err := a.db.QueryContext(ctx, `SELECT 'movie', f.id, f.filename, f.directory_id FROM media_files f JOIN media_directories d ON d.id=f.directory_id WHERE d.kind='movie' AND f.available=1
 UNION SELECT 'series',s.id,s.title,s.directory_id FROM series s JOIN file_organization o ON o.series_id=s.id JOIN media_files f ON f.id=o.media_file_id WHERE f.available=1
 UNION SELECT 'series',f.id,f.filename,f.directory_id FROM media_files f JOIN media_directories d ON d.id=f.directory_id LEFT JOIN file_organization o ON o.media_file_id=f.id JOIN metadata_matches m ON m.kind='series' AND m.target_id=f.id WHERE f.available=1 AND d.kind='series' AND o.series_id IS NULL AND m.source_id>0 ORDER BY 1,2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := []metadataTarget{}
	for rows.Next() {
		var target metadataTarget
		if err := rows.Scan(&target.Kind, &target.ID, &target.Title, &target.DirectoryID); err != nil {
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
		ctx, cancel := context.WithTimeout(a.ctx, 65*time.Second)
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
	sources, policy, err := a.directorySources(ctx, target.DirectoryID)
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return nil
	}
	var oldID int64
	var oldSource, selection, oldPolicy string
	err = a.db.QueryRowContext(ctx, "SELECT source,source_id,selection,policy_key FROM metadata_matches WHERE kind=? AND target_id=?", target.Kind, target.ID).Scan(&oldSource, &oldID, &selection, &oldPolicy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return errors.New("无法读取影片资料关联")
	}
	if selection == "manual" {
		// Even a priority change must never silently replace a household's choice.
		if !sourceEnabled(sources, oldSource) {
			return nil
		}
		sources = []string{oldSource}
	}
	if oldID > 0 && (oldPolicy == policy || selection == "manual") {
		// Read only the JSON, never the (potentially 5 MiB) poster, to upgrade
		// older caches without discarding still-valid title information.
		var payload string
		err := a.db.QueryRowContext(ctx, "SELECT payload FROM metadata_cache WHERE source=? AND kind=? AND source_id=? AND expires_at>?", oldSource, target.Kind, oldID, time.Now().Unix()).Scan(&payload)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var m TitleMetadata
		if err == nil && json.Unmarshal([]byte(payload), &m) == nil {
			client := a.metadata.provider(oldSource)
			if (creditsDue(m.Credits) || artworkDue(m.Artwork)) && sourceEnabled(sources, oldSource) && client != nil && client.configured() {
				if artworkDue(m.Artwork) {
					m.Artwork = fetchTitleArtwork(ctx, client, target.Kind, oldID, m.Artwork)
					decorateArtwork(&m)
				}
				if creditsDue(m.Credits) {
					m.Credits = fetchTitleCredits(ctx, client, target.Kind, oldID, m.Credits)
				}
				updated, err := json.Marshal(m)
				if err != nil {
					return err
				}
				// Do not extend the original TTL or change the chosen association.
				tx, err := a.db.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback()
				result, err := tx.ExecContext(ctx, "UPDATE metadata_cache SET payload=? WHERE source=? AND kind=? AND source_id=? AND expires_at>?", string(updated), oldSource, target.Kind, oldID, time.Now().Unix())
				if err != nil {
					return err
				}
				if changed, _ := result.RowsAffected(); changed > 0 {
					if err = replaceMetadataPeople(ctx, tx, oldSource, target.Kind, oldID, m); err != nil {
						return err
					}
				}
				return tx.Commit()
			}
			return nil
		}
	}
	failures := []string{}
	failed := false
	for _, source := range sources {
		client := a.metadata.provider(source)
		if !client.configured() {
			failures = append(failures, strings.ToUpper(source)+" 未配置凭据")
			failed = true
			continue
		}
		// Per-source budget: a slow primary must leave time for the fallback.
		sourceCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		id := int64(0)
		if oldSource == source {
			id = oldID
		}
		var sourceErr error
		if id == 0 {
			query, year := matchQuery(target.Title)
			candidates, complete, err := client.search(sourceCtx, target.Kind, query)
			sourceErr = err
			if err == nil && complete && normalizedTitle(query) != "" {
				for _, candidate := range candidates {
					if (normalizedTitle(candidate.Title) == normalizedTitle(query) || normalizedTitle(candidate.OriginalTitle) == normalizedTitle(query) || normalizedTitle(candidate.Number) == normalizedTitle(query)) && (year == "" || year == candidate.Year) {
						if id != 0 {
							id = 0
							break
						}
						id = candidate.SourceID
					}
				}
			}
		}
		if sourceErr != nil || id == 0 {
			cancel()
			if sourceErr != nil {
				failed = true
				failures = append(failures, sourceErr.Error())
			} else {
				failures = append(failures, strings.ToUpper(source)+" 没有唯一可靠的匹配")
			}
			continue
		}
		m, poster, typ, valid, sourceErr := a.cachedMetadata(sourceCtx, source, target.Kind, id)
		warning := ""
		if sourceErr == nil && !valid {
			m, poster, typ, warning, sourceErr = fetchMetadataDetails(sourceCtx, client, target.Kind, id)
		} else if sourceErr == nil {
			if artworkDue(m.Artwork) {
				m.Artwork = fetchTitleArtwork(sourceCtx, client, target.Kind, id, m.Artwork)
				decorateArtwork(&m)
			}
			if creditsDue(m.Credits) {
				m.Credits = fetchTitleCredits(sourceCtx, client, target.Kind, id, m.Credits)
			}
		}
		cancel()
		if sourceErr != nil {
			failed = true
			failures = append(failures, sourceErr.Error())
			continue
		}
		if len(failures) > 0 {
			warning = strings.Join(append(failures, "已使用 "+m.Source, warning), "；")
		}
		if selection == "" {
			selection = "automatic"
		}
		if err := a.saveMetadata(ctx, target, m, poster, typ, selection, strings.TrimRight(warning, "；")); err != nil {
			return errors.New("无法保存影片资料，本地媒体未改变")
		}
		return nil
	}
	status := "unmatched"
	if failed {
		status = "failed"
	}
	return a.metadataFailure(ctx, target, status, errors.New(strings.Join(failures, "；")+"；请检查目录资料源或手动匹配"))
}

func (a *App) metadataFailure(ctx context.Context, target metadataTarget, status string, cause error) error {
	// Preserve the chosen ID even if the cache expires or the provider stops responding.
	// In particular, an explicit manual choice must never revert to auto-search.
	_, err := a.db.ExecContext(ctx, `INSERT INTO metadata_matches(kind,target_id,status,error) VALUES(?,?,?,?) ON CONFLICT(kind,target_id) DO UPDATE SET status=excluded.status,error=excluded.error`, target.Kind, target.ID, status, cause.Error())
	if err != nil {
		return errors.New("无法保存资料获取状态")
	}
	return cause
}

func (a *App) saveMetadata(ctx context.Context, target metadataTarget, m TitleMetadata, poster []byte, typ, selection, warning string) error {
	_, policy, err := a.directorySources(ctx, target.DirectoryID)
	if err != nil {
		return err
	}
	source := strings.ToLower(m.Source)
	payload, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO metadata_cache(source,kind,source_id,payload,expires_at,poster,poster_type) VALUES(?,?,?,?,?,?,?) ON CONFLICT(source,kind,source_id) DO UPDATE SET payload=excluded.payload,expires_at=excluded.expires_at,poster=excluded.poster,poster_type=excluded.poster_type`, source, target.Kind, m.SourceID, string(payload), m.ExpiresAt, poster, typ); err != nil {
		return err
	}
	if err = replaceMetadataPeople(ctx, tx, source, target.Kind, m.SourceID, m); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO metadata_matches(kind,target_id,source,source_id,policy_key,selection,status,error) VALUES(?,?,?,?,?,?,'matched',?) ON CONFLICT(kind,target_id) DO UPDATE SET source=excluded.source,source_id=excluded.source_id,policy_key=excluded.policy_key,selection=excluded.selection,status=excluded.status,error=excluded.error`, target.Kind, target.ID, source, m.SourceID, policy, selection, warning); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) attachMetadata(ctx context.Context, files []MediaFile) error {
	rows, err := a.db.QueryContext(ctx, `SELECT m.kind,m.target_id,m.selection,m.status,m.error,COALESCE(c.payload,'') FROM metadata_matches m LEFT JOIN metadata_cache c ON c.source=m.source AND c.kind=m.kind AND c.source_id=m.source_id AND c.expires_at>?`, time.Now().Unix())
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
				if m.SourceID == 0 {
					m.SourceID = m.TMDBID
				}
				decoratePeople(&m)
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
	source := r.PathValue("source")
	if source == "" {
		source = "tmdb"
	}
	var poster []byte
	var typ string
	err = a.db.QueryRowContext(r.Context(), "SELECT poster,poster_type FROM metadata_cache WHERE source=? AND kind=? AND source_id=? AND expires_at>?", source, r.PathValue("kind"), id, time.Now().Unix()).Scan(&poster, &typ)
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
