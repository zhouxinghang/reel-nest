package app

import (
	"database/sql"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Organization is local filename-derived structure, not a TMDB metadata match.
// A file has at most one episode reference; multiple files may share that reference.
type EpisodeReference struct {
	SeriesID      string `json:"seriesId"`
	SeriesTitle   string `json:"seriesTitle"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
}

type episodeName struct {
	title   string
	season  int
	episode int
	reason  string
}

var (
	episodeMarker = regexp.MustCompile(`(?i)s([0-9]+)e([0-9]+)`)
	extraEpisode  = regexp.MustCompile(`(?i)^[\s._-]*e[0-9]+`)
	episodeRange  = regexp.MustCompile(`(?i)^\s*[-+&~～至到.,_]+\s*e?[0-9]+(?:$|[^a-z0-9])`)
	seasonRange   = regexp.MustCompile(`(?i)s[0-9]+\s*[-+&~]\s*s?[0-9]+e[0-9]+`)
	seasonFolder  = regexp.MustCompile(`(?i)^(?:(?:season[ ._-]*|s)[0-9]+|第[0-9]+季)$`)
)

func asciiLetterOrDigit(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func titleFromFolder(relativePath string) string {
	parent := filepath.Dir(relativePath)
	if seasonFolder.MatchString(filepath.Base(parent)) {
		parent = filepath.Dir(parent)
	}
	if parent == "." {
		return ""
	}
	title := localTitle(filepath.Base(parent))
	switch strings.ToLower(title) {
	case "tv", "tv shows", "tvshows", "shows", "series", "season", "episodes", "media", "videos", "downloads", "unknown", "misc", "电视剧", "视频", "下载", "未命名":
		return ""
	}
	return title
}

func localTitle(value string) string {
	value = strings.NewReplacer(".", " ", "_", " ").Replace(value)
	return strings.Join(strings.Fields(strings.Trim(value, " -")), " ")
}

func identifyEpisode(relativePath string) episodeName {
	filename := filepath.Base(relativePath)
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	markers := episodeMarker.FindAllStringSubmatchIndex(stem, -1)
	if len(markers) != 1 {
		return episodeName{reason: "未识别到唯一的 S01E01 季集标记"}
	}
	marker := markers[0]
	suffix := stem[marker[1]:]
	if extraEpisode.MatchString(suffix) || episodeRange.MatchString(suffix) || seasonRange.MatchString(stem) {
		return episodeName{reason: "疑似多集合一文件，保留为一个未组织的媒体文件"}
	}
	if (marker[0] > 0 && asciiLetterOrDigit(stem[marker[0]-1])) || (len(suffix) > 0 && asciiLetterOrDigit(suffix[0])) {
		return episodeName{reason: "季集标记边界不明确，未自动组织"}
	}
	season, seasonErr := strconv.Atoi(stem[marker[2]:marker[3]])
	episode, episodeErr := strconv.Atoi(stem[marker[4]:marker[5]])
	if seasonErr != nil || episodeErr != nil || season > 999 || episode < 1 || episode > 9999 {
		return episodeName{reason: "季集编号超出支持范围"}
	}
	title := localTitle(stem[:marker[0]])
	if title == "" {
		title = titleFromFolder(relativePath)
	}
	if title == "" || seasonFolder.MatchString(title) {
		return episodeName{reason: "无法可靠识别电视剧名"}
	}
	return episodeName{title: title, season: season, episode: episode}
}

// This incremental migration works on the existing index, not the source files.
// Offline media need not be rescanned, and their stable file IDs are untouched.
func (a *App) initializeOrganization() error {
	tx, err := a.db.BeginTx(a.ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(a.ctx, `
 CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS series (
  id TEXT PRIMARY KEY, directory_id TEXT NOT NULL REFERENCES media_directories(id),
  title_key TEXT NOT NULL, title TEXT NOT NULL, UNIQUE(directory_id,title_key)
 );
 CREATE TABLE IF NOT EXISTS file_organization (
  media_file_id TEXT PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
  series_id TEXT REFERENCES series(id), season_number INTEGER, episode_number INTEGER,
  reason TEXT NOT NULL DEFAULT '',
  CHECK ((series_id IS NULL AND season_number IS NULL AND episode_number IS NULL)
   OR (series_id IS NOT NULL AND season_number IS NOT NULL AND episode_number IS NOT NULL AND season_number BETWEEN 0 AND 999 AND episode_number BETWEEN 1 AND 9999))
 );
 CREATE INDEX IF NOT EXISTS organization_series ON file_organization(series_id,season_number,episode_number);`)
	if err != nil {
		return err
	}
	var applied int
	if err = tx.QueryRowContext(a.ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version=3").Scan(&applied); err != nil {
		return err
	}
	if applied == 0 {
		rows, err := tx.QueryContext(a.ctx, "SELECT f.id,f.directory_id,f.relative_path FROM media_files f JOIN media_directories d ON d.id=f.directory_id WHERE d.kind='series' ORDER BY f.relative_path,f.id")
		if err != nil {
			return err
		}
		type existingFile struct{ id, directory, path string }
		files := []existingFile{}
		for rows.Next() {
			var file existingFile
			if err = rows.Scan(&file.id, &file.directory, &file.path); err != nil {
				rows.Close()
				return err
			}
			files = append(files, file)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, file := range files {
			if err = a.organizeMedia(tx, file.id, file.directory, file.path); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(a.ctx, "INSERT INTO schema_migrations(version) VALUES(3)"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *App) organizeMedia(tx *sql.Tx, mediaID, directoryID, relativePath string) error {
	name := identifyEpisode(relativePath)
	var seriesID, season, episode any
	if name.reason == "" {
		var id string
		err := tx.QueryRowContext(a.ctx, `INSERT INTO series(id,directory_id,title_key,title) VALUES(?,?,?,?)
   ON CONFLICT(directory_id,title_key) DO UPDATE SET title=series.title RETURNING id`,
			newID(), directoryID, strings.ToLower(name.title), name.title).Scan(&id)
		if err != nil {
			return err
		}
		seriesID, season, episode = id, name.season, name.episode
	}
	_, err := tx.ExecContext(a.ctx, `INSERT INTO file_organization(media_file_id,series_id,season_number,episode_number,reason)
  VALUES(?,?,?,?,?) ON CONFLICT(media_file_id) DO UPDATE SET series_id=excluded.series_id,
  season_number=excluded.season_number,episode_number=excluded.episode_number,reason=excluded.reason`,
		mediaID, seriesID, season, episode, name.reason)
	return err
}
