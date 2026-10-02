package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
)

const defaultMetadataSources = `["tmdb","tvdb"]`

var metadataSources = []struct{ id, name string }{{"tmdb", "TheMovieDB"}, {"tvdb", "TheTVDB"}, {"metatube", "MetaTube"}}

func validMetadataSource(source string) bool {
	for _, entry := range metadataSources {
		if entry.id == source {
			return true
		}
	}
	return false
}

type metadataProvider interface {
	configured() bool
	cooldownUntil() int64
	search(context.Context, string, string) ([]TitleMetadata, bool, error)
	details(context.Context, string, int64) (TitleMetadata, []byte, string, string, error)
	credits(context.Context, string, int64) (TitleCredits, error)
	photo(context.Context, string) ([]byte, string, error)
	artwork(context.Context, string, int64) (TitleArtwork, error)
	image(context.Context, string, string) ([]byte, string, error)
}

type metadataSourceStatus struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Configured    bool   `json:"configured"`
	CooldownUntil int64  `json:"cooldownUntil"`
}

func (s *metadataService) sources() []metadataSourceStatus {
	result := []metadataSourceStatus{}
	for _, source := range metadataSources {
		client := s.provider(source.id)
		result = append(result, metadataSourceStatus{source.id, source.name, client.configured(), client.cooldownUntil()})
	}
	return result
}
func (s *metadataService) configured() bool {
	for _, source := range metadataSources {
		client := s.provider(source.id)
		if client.configured() {
			return true
		}
	}
	return false
}

func validMetadataSources(sources []string) bool {
	if sources == nil || len(sources) > len(metadataSources) {
		return false
	}
	seen := map[string]bool{}
	for _, source := range sources {
		if !validMetadataSource(source) || seen[source] {
			return false
		}
		seen[source] = true
	}
	return true
}

func (a *App) initializeDirectorySources() error {
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info('media_directories') WHERE name='metadata_sources'").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := a.db.Exec(`ALTER TABLE media_directories ADD COLUMN metadata_sources TEXT NOT NULL DEFAULT '` + defaultMetadataSources + `'`)
		return err
	}
	return nil
}

func (a *App) directorySources(ctx context.Context, id string) ([]string, string, error) {
	var payload string
	if err := a.db.QueryRowContext(ctx, "SELECT metadata_sources FROM media_directories WHERE id=?", id).Scan(&payload); err != nil {
		return nil, "", err
	}
	var sources []string
	if json.Unmarshal([]byte(payload), &sources) != nil || !validMetadataSources(sources) {
		return nil, "", errors.New("媒体目录资料源配置无效")
	}
	return sources, payload, nil
}

func (a *App) updateDirectorySources(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Sources []string `json:"metadataSources"`
	}
	if !decode(w, r, &input) {
		return
	}
	if !validMetadataSources(input.Sources) {
		fail(w, 400, "metadataSources 必须是不重复的 tmdb/tvdb/metatube 有序数组；空数组禁用自动获取")
		return
	}
	// A successful save cannot be followed by an in-flight match using stale
	// priority. The UI can retry when another metadata write finishes.
	if !a.metadata.operation.TryLock() {
		fail(w, 409, "影片资料获取正在进行，请稍后保存")
		return
	}
	defer a.metadata.operation.Unlock()
	payload, _ := json.Marshal(input.Sources)
	result, err := a.db.ExecContext(r.Context(), "UPDATE media_directories SET metadata_sources=? WHERE id=?", string(payload), r.PathValue("id"))
	if err != nil {
		fail(w, 500, "无法保存目录资料源")
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		fail(w, 404, "媒体目录不存在")
		return
	}
	a.queueMetadata()
	respond(w, 200, map[string]any{"metadataSources": input.Sources})
}

// Rebuild the two legacy TMDB-only tables atomically. Existing payloads and
// poster URLs keep working; sourceId is filled when reading old JSON payloads.
func (a *App) initializeMetadataStorage() error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var legacy int
	if err = tx.QueryRow("SELECT COUNT(*) FROM pragma_table_info('metadata_cache') WHERE name='tmdb_id'").Scan(&legacy); err != nil {
		return err
	}
	if legacy > 0 {
		// These tables are a rebuildable projection of metadata_cache. Dropping
		// them also handles databases produced by development builds that created
		// the projection before exercising the legacy migration path.
		if _, err = tx.Exec(`DROP TRIGGER IF EXISTS metadata_people_payload_changed;
   DROP TRIGGER IF EXISTS metadata_people_cache_deleted;
   DROP TABLE IF EXISTS metadata_people;
   DROP TABLE IF EXISTS metadata_people_state;
   DROP TABLE IF EXISTS metadata_images;
   ALTER TABLE metadata_cache RENAME TO metadata_cache_legacy;
   ALTER TABLE metadata_matches RENAME TO metadata_matches_legacy;`); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS metatube_titles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  provider TEXT NOT NULL COLLATE NOCASE, remote_id TEXT NOT NULL,
  UNIQUE(provider,remote_id)
 );
 CREATE TABLE IF NOT EXISTS metadata_matches (
  kind TEXT NOT NULL CHECK(kind IN ('movie','series')), target_id TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT 'tmdb', source_id INTEGER NOT NULL DEFAULT 0,
  policy_key TEXT NOT NULL DEFAULT '', selection TEXT NOT NULL DEFAULT 'automatic',
  status TEXT NOT NULL, error TEXT NOT NULL DEFAULT '', PRIMARY KEY(kind,target_id)
 );
 CREATE TABLE IF NOT EXISTS metadata_cache (
  source TEXT NOT NULL, kind TEXT NOT NULL, source_id INTEGER NOT NULL, payload TEXT NOT NULL,
  expires_at INTEGER NOT NULL, poster BLOB, poster_type TEXT NOT NULL,
  PRIMARY KEY(source,kind,source_id)
 );
 CREATE TABLE IF NOT EXISTS person_photos (
  source TEXT NOT NULL, photo_path TEXT NOT NULL, photo BLOB, content_type TEXT NOT NULL,
  expires_at INTEGER NOT NULL, retry_at INTEGER NOT NULL,
  PRIMARY KEY(source,photo_path)
 );`)
	if err != nil {
		return err
	}
	if legacy > 0 {
		_, err = tx.Exec(`INSERT INTO metadata_cache SELECT 'tmdb',kind,tmdb_id,payload,expires_at,poster,poster_type FROM metadata_cache_legacy;
   INSERT INTO metadata_matches(kind,target_id,source,source_id,policy_key,selection,status,error)
   SELECT kind,target_id,'tmdb',tmdb_id,'` + defaultMetadataSources + `',selection,status,error FROM metadata_matches_legacy;
   DROP TABLE metadata_cache_legacy; DROP TABLE metadata_matches_legacy;`)
		if err != nil {
			return err
		}
	}
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS metadata_images (
  source TEXT NOT NULL, kind TEXT NOT NULL, source_id INTEGER NOT NULL,
  image_type TEXT NOT NULL, image_path TEXT NOT NULL, image BLOB, content_type TEXT NOT NULL,
  expires_at INTEGER NOT NULL, retry_at INTEGER NOT NULL,
  PRIMARY KEY(source,kind,source_id,image_type),
  FOREIGN KEY(source,kind,source_id) REFERENCES metadata_cache(source,kind,source_id) ON DELETE CASCADE
 );`)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (a *App) cachedMetadata(ctx context.Context, source, kind string, id int64) (TitleMetadata, []byte, string, bool, error) {
	var m TitleMetadata
	var payload, typ string
	var poster []byte
	err := a.db.QueryRowContext(ctx, `SELECT payload,poster,poster_type FROM metadata_cache WHERE source=? AND kind=? AND source_id=? AND expires_at>unixepoch()`, source, kind, id).Scan(&payload, &poster, &typ)
	if errors.Is(err, sql.ErrNoRows) {
		return m, nil, "", false, nil
	}
	if err != nil {
		return m, nil, "", false, err
	}
	if json.Unmarshal([]byte(payload), &m) != nil {
		return m, nil, "", false, nil
	}
	if m.SourceID == 0 {
		m.SourceID = m.TMDBID
	}
	return m, poster, typ, true, nil
}

func sourceEnabled(sources []string, source string) bool { return slices.Contains(sources, source) }
