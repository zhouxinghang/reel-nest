package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"reelnest/internal/app"
)

const fakeTVDBKey = "isolated-tvdb-key"
const fakeTVDBPIN = "isolated-tvdb-pin"
const fakeTVDBJWT = "isolated-tvdb-jwt"

type tvdbFixture struct {
	mu       sync.Mutex
	mode     string
	requests []string
	server   *httptest.Server
}

func fakeTVDB(t *testing.T) *tvdbFixture {
	t.Helper()
	f := &tvdbFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		mode := f.mode
		f.requests = append(f.requests, r.URL.String())
		f.mu.Unlock()
		switch mode {
		case "invalid":
			w.WriteHeader(401)
			fmt.Fprint(w, fakeTVDBKey+fakeTVDBPIN+fakeTVDBJWT)
			return
		case "error":
			w.WriteHeader(500)
			return
		case "timeout":
			// A POST body can delay net/http disconnect detection; keep the fixture bounded too.
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
			return
		case "rate":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
			return
		case "malformed":
			fmt.Fprint(w, fakeTVDBJWT)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/login" {
			var credentials map[string]string
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&credentials) != nil || credentials["apikey"] != fakeTVDBKey || (credentials["pin"] != fakeTVDBPIN && credentials["pin"] != "") {
				t.Error("invalid TVDB login request")
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("JWT sent to login")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]string{"token": fakeTVDBJWT}})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/artwork/") {
			if r.URL.Path == "/artwork/types" {
				serveTVDBArtworkTypes(w)
				return
			}
			if (strings.HasSuffix(r.URL.Path, "/thumb.png") || strings.HasSuffix(r.URL.Path, "/backdrop.png")) && serveArtworkImageFailure(w, r, mode) {
				return
			}
			if strings.HasSuffix(r.URL.Path, "/person.png") && servePhotoFailure(w, r, mode) {
				return
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("credential leaked to artwork")
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(append([]byte{}, tinyPoster...))
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fakeTVDBJWT {
			t.Error("missing TVDB bearer token")
		}
		if r.URL.Path == "/search" {
			kind := r.URL.Query().Get("type")
			if (kind != "movie" && kind != "series") || r.URL.Query().Get("language") != "" || r.URL.Query().Get("limit") != "21" {
				t.Error("incorrect TVDB search contract")
			}
			title, original, year := "TVDB 降临", "Arrival", "2016"
			if kind == "series" {
				title, original, year = "TVDB 暗黑", "Dark", "2017"
			}
			results := []map[string]any{}
			if mode != "none" {
				results = append(results, map[string]any{"tvdb_id": "10", "type": kind, "name": original, "year": year, "translations": map[string]string{"zho": title}})
				if mode == "ambiguous" {
					results = append(results, map[string]any{"tvdb_id": "99", "type": kind, "name": original, "year": year})
				}
			}
			links := map[string]any{"next": nil}
			if mode == "more" {
				links["next"] = "untrusted-next-page"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": results, "links": links})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/extended") && r.URL.Query().Get("short") == "false" {
			serveTVDBCredits(t, w, r, mode)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/extended") || r.URL.Query().Get("meta") != "translations" || r.URL.Query().Get("short") != "true" {
			t.Error("incorrect TVDB details contract")
		}
		id := 10
		if strings.Contains(r.URL.Path, "/30/") {
			id = 30
		}
		if mode == "wrong-id" {
			id = 99
		}
		original, title, year := "Arrival", "TVDB 降临", "2016"
		if strings.Contains(r.URL.Path, "/series/") {
			original, title, year = "Dark", "TVDB 暗黑", "2017"
		}
		translations := map[string]any{"nameTranslations": []map[string]string{{"language": "zho", "name": title}}, "overviewTranslations": []map[string]string{{"language": "zho", "overview": "TVDB 中文简介"}}}
		if mode == "english" {
			translations = map[string]any{"nameTranslations": []map[string]string{{"language": "eng", "name": original}}, "overviewTranslations": []map[string]string{{"language": "eng", "overview": "English TVDB overview"}}}
		}
		if mode == "original" {
			translations = map[string]any{}
		}
		image := f.server.URL + "/artwork/poster.png"
		if mode == "unsafe-image" {
			image = "http://169.254.169.254/latest/meta-data"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"id": id, "name": original, "overview": "Original overview", "year": year, "image": image, "translations": translations}})
	}))
	t.Cleanup(f.server.Close)
	return f
}
func (f *tvdbFixture) setMode(mode string) { f.mu.Lock(); defer f.mu.Unlock(); f.mode = mode }
func (f *tvdbFixture) count() int          { f.mu.Lock(); defer f.mu.Unlock(); return len(f.requests) }
func (f *tvdbFixture) configure(config *app.Config) {
	config.TVDBAPIKey, config.TVDBPIN = fakeTVDBKey, fakeTVDBPIN
	config.TVDBAPIURL, config.TVDBImageURL = f.server.URL, f.server.URL+"/artwork"
	config.TVDBTimeout = 150 * time.Millisecond
}
func addSourceDirectory(t *testing.T, a *runningApp, root, kind string, sources []string) app.Directory {
	t.Helper()
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]any{"name": "测试媒体库", "path": root, "kind": kind, "metadataSources": sources}, 201)
	var dir app.Directory
	if err := json.Unmarshal(body, &dir); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dir.MetadataSources, sources) {
		t.Fatalf("sources not persisted: %s", body)
	}
	awaitScan(t, a, dir.ID, "completed")
	awaitMetadata(t, a)
	return dir
}
func setSources(t *testing.T, a *runningApp, id string, sources []string) {
	t.Helper()
	request(t, a.client, "POST", a.server.URL+"/api/directories/"+id+"/metadata-sources", map[string]any{"metadataSources": sources}, 200)
	awaitMetadata(t, a)
}
func sourceFile(t *testing.T, a *runningApp, dir string) app.MediaFile {
	t.Helper()
	for _, local := range library(t, a) {
		file := metadataMedia(t, a, local.ID)
		if file.DirectoryID == dir {
			return file
		}
	}
	t.Fatal("directory has no media")
	return app.MediaFile{}
}

func TestDirectoryMetadataPriorityIsolationAndReordering(t *testing.T) {
	root := t.TempDir()
	tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
	config := tmdb.config(t, root)
	tvdb.configure(&config)
	for _, name := range []string{"tmdb", "tvdb", "off"} {
		writeMedia(t, filepath.Join(root, name, "Arrival (2016).webm"), "unchanged video")
	}
	a := start(t, config)
	login(t, a)
	first := addSourceDirectory(t, a, filepath.Join(root, "tmdb"), "movie", []string{"tmdb", "tvdb"})
	if tvdb.count() != 0 {
		t.Fatal("lower priority source contacted after success")
	}
	before := tmdb.count()
	second := addSourceDirectory(t, a, filepath.Join(root, "tvdb"), "movie", []string{"tvdb", "tmdb"})
	if tmdb.count() != before {
		t.Fatal("TMDB contacted despite TVDB success")
	}
	one, two := sourceFile(t, a, first.ID), sourceFile(t, a, second.ID)
	if one.Match.Metadata.Source != "TMDB" || two.Match.Metadata.Source != "TVDB" || one.Match.Metadata.SourceID != two.Match.Metadata.SourceID {
		t.Fatal("source identity collision or incorrect priority")
	}
	if one.Match.Metadata.PosterURL == two.Match.Metadata.PosterURL {
		t.Fatal("poster URL collision")
	}
	for _, file := range []app.MediaFile{one, two} {
		request(t, a.client, "GET", a.server.URL+file.Match.Metadata.PosterURL, nil, 200)
		request(t, http.DefaultClient, "GET", a.server.URL+file.Match.Metadata.PosterURL, nil, 401)
	}
	before, beforeTVDB := tmdb.count(), tvdb.count()
	off := addSourceDirectory(t, a, filepath.Join(root, "off"), "movie", []string{})
	if sourceFile(t, a, off.ID).Matched || tmdb.count() != before || tvdb.count() != beforeTVDB {
		t.Fatal("disabled directory fetched metadata")
	}
	request(t, a.client, "POST", a.server.URL+"/api/media/"+one.ID+"/progress", map[string]any{"position": 14, "duration": 60, "completed": false}, 200)
	setSources(t, a, first.ID, []string{"tvdb", "tmdb"})
	changed := sourceFile(t, a, first.ID)
	if changed.Match.Metadata.Source != "TVDB" || changed.ID != one.ID || changed.Progress.Position != 14 {
		t.Fatal("reorder failed or media identity/progress changed")
	}
	a.close()
	a = start(t, config)
	login(t, a)
	awaitMetadata(t, a)
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/directories", nil, 200)
	var listing struct {
		Directories []app.Directory `json:"directories"`
	}
	_ = json.Unmarshal(body, &listing)
	for _, dir := range listing.Directories {
		if dir.ID == first.ID && !reflect.DeepEqual(dir.MetadataSources, []string{"tvdb", "tmdb"}) {
			t.Fatal("priority lost on restart")
		}
	}
	if sourceFile(t, a, first.ID).Match.Metadata.Source != "TVDB" {
		t.Fatal("restarted source association changed")
	}
	data, err := os.ReadFile(filepath.Join(root, "tmdb", "Arrival (2016).webm"))
	if err != nil || string(data) != "unchanged video" {
		t.Fatal("source video altered")
	}
}

func TestMetadataSourcesFallbackAndIndependentCooldowns(t *testing.T) {
	for _, mode := range []string{"no-key", "invalid", "timeout", "error", "rate", "malformed", "none", "ambiguous", "more", "wrong-id"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
			config := tmdb.config(t, root)
			tvdb.configure(&config)
			if mode == "no-key" {
				config.TVDBAPIKey = ""
			} else {
				tvdb.setMode(mode)
			}
			writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "video")
			a := start(t, config)
			login(t, a)
			dir := addSourceDirectory(t, a, root, "movie", []string{"tvdb", "tmdb"})
			file := sourceFile(t, a, dir.ID)
			if !file.Matched || file.Match.Metadata.Source != "TMDB" || !strings.Contains(file.Match.Error, "TVDB") {
				t.Fatalf("fallback missing: %+v", file.Match)
			}
			for _, secret := range []string{fakeTVDBKey, fakeTVDBPIN, fakeTVDBJWT} {
				if strings.Contains(file.Match.Error, secret) {
					t.Fatal("secret leaked")
				}
			}
			if mode == "no-key" && tvdb.count() != 0 {
				t.Fatal("TVDB called without key")
			}
			if mode == "rate" || mode == "invalid" || mode == "error" {
				before := tvdb.count()
				request(t, a.client, "GET", a.server.URL+"/api/matching/movie/"+file.ID+"/search?q=Arrival", nil, 200)
				if tvdb.count() != before {
					t.Fatal("TVDB cooldown ignored")
				}
			}
			request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID+"/content", nil, 200)
		})
	}
	// In the reverse direction, failed TMDB must not cool down TVDB.
	root := t.TempDir()
	tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
	tmdb.setMode("rate")
	config := tmdb.config(t, root)
	tvdb.configure(&config)
	writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "video")
	a := start(t, config)
	login(t, a)
	dir := addSourceDirectory(t, a, root, "movie", []string{"tmdb", "tvdb"})
	if file := sourceFile(t, a, dir.ID); !file.Matched || file.Match.Metadata.Source != "TVDB" {
		t.Fatal("TMDB failure blocked TVDB")
	}
}

func TestTVDBMovieSeriesLocalizationAndUnsafeArtwork(t *testing.T) {
	for _, mode := range []string{"success", "english", "original", "unsafe-image"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
			tvdb.setMode(mode)
			config := tmdb.config(t, root)
			tvdb.configure(&config)
			config.TMDBToken = ""
			config.TVDBPIN = ""
			writeMedia(t, filepath.Join(root, "movies", "Arrival (2016).webm"), "movie")
			writeMedia(t, filepath.Join(root, "series", "Dark S01E01.webm"), "episode")
			a := start(t, config)
			login(t, a)
			for _, entry := range []struct{ path, kind string }{{"movies", "movie"}, {"series", "series"}} {
				dir := addSourceDirectory(t, a, filepath.Join(root, entry.path), entry.kind, []string{"tvdb"})
				file := sourceFile(t, a, dir.ID)
				if !file.Matched || file.Match.Metadata.Source != "TVDB" || file.Match.Metadata.Kind != entry.kind {
					t.Fatalf("TVDB metadata missing: %+v", file.Match)
				}
				if entry.kind == "series" && file.Episode == nil {
					t.Fatal("series hierarchy lost")
				}
				expected := "TVDB 中文简介"
				if mode == "english" {
					expected = "English TVDB overview"
				}
				if mode == "original" {
					expected = "Original overview"
				}
				if file.Match.Metadata.Overview != expected {
					t.Fatal("translation fallback failed")
				}
				if mode == "unsafe-image" && (file.Match.Metadata.PosterURL != "" || file.Match.Error == "") {
					t.Fatal("unsafe artwork trusted")
				}
			}
			tvdb.mu.Lock()
			logins := 0
			for _, path := range tvdb.requests {
				if path == "/login" {
					logins++
				}
			}
			tvdb.mu.Unlock()
			if logins != 1 {
				t.Fatalf("token was not reused: %d", logins)
			}
		})
	}
}

func TestSourceAwareManualMatchingAndValidation(t *testing.T) {
	root := t.TempDir()
	tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
	config := tmdb.config(t, root)
	tvdb.configure(&config)
	writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "video")
	a := start(t, config)
	login(t, a)
	dir := addSourceDirectory(t, a, root, "movie", []string{"tvdb", "tmdb"})
	file := sourceFile(t, a, dir.ID)
	base := a.server.URL + "/api/matching/movie/" + file.ID
	body, _ := request(t, a.client, "GET", base+"/search?q=Arrival", nil, 200)
	var result struct {
		Items []app.TitleMetadata `json:"items"`
	}
	_ = json.Unmarshal(body, &result)
	if len(result.Items) != 2 || result.Items[0].Source != "TVDB" || result.Items[1].Source != "TMDB" {
		t.Fatalf("search priority: %s", body)
	}
	request(t, a.client, "POST", base+"/selection", map[string]any{"source": "tvdb", "sourceId": 30, "kind": "movie"}, 200)
	setSources(t, a, dir.ID, []string{"tmdb", "tvdb"})
	if f := sourceFile(t, a, dir.ID); f.Match.Selection != "manual" || f.Match.Metadata.Source != "TVDB" || f.Match.Metadata.SourceID != 30 {
		t.Fatal("priority overwrote manual choice")
	}
	for _, value := range []any{nil, []string{"tvdb", "tvdb"}, []string{"unknown"}, []string{"tmdb", "tvdb", "tmdb"}, "tvdb"} {
		request(t, a.client, "POST", a.server.URL+"/api/directories/"+dir.ID+"/metadata-sources", map[string]any{"metadataSources": value}, 400)
	}
	request(t, http.DefaultClient, "POST", a.server.URL+"/api/directories/"+dir.ID+"/metadata-sources", map[string]any{"metadataSources": []string{}}, 401)
	request(t, a.client, "POST", a.server.URL+"/api/directories/missing/metadata-sources", map[string]any{"metadataSources": []string{}}, 404)
	request(t, a.client, "POST", base+"/selection", map[string]any{"source": "other", "sourceId": 10, "kind": "movie"}, 400)
	request(t, a.client, "POST", base+"/selection", map[string]any{"source": "tvdb", "sourceId": 10, "kind": "series"}, 400)
	setSources(t, a, dir.ID, []string{"tmdb"})
	request(t, a.client, "POST", base+"/selection", map[string]any{"source": "tvdb", "sourceId": 10, "kind": "movie"}, 400)
	a.close()
	// The manual provider identity must survive expiry, even with another source
	// now ahead of it. Force expiry in the real persisted database.
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE metadata_cache SET expires_at=0; UPDATE media_directories SET metadata_sources='[\"tmdb\",\"tvdb\"]'")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	a = start(t, config)
	login(t, a)
	awaitMetadata(t, a)
	if f := sourceFile(t, a, dir.ID); !f.Matched || f.Match.Selection != "manual" || f.Match.Metadata.Source != "TVDB" || f.Match.Metadata.SourceID != 30 {
		t.Fatal("expiry lost manual provider")
	}
}

func TestLegacyTMDBCacheAndDirectoryMigration(t *testing.T) {
	root := t.TempDir()
	tmdb := fakeTMDB(t)
	config := tmdb.config(t, root)
	writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "video")
	a := start(t, config)
	login(t, a)
	dir := addSourceDirectory(t, a, root, "movie", []string{"tmdb", "tvdb"})
	file := sourceFile(t, a, dir.ID)
	request(t, a.client, "POST", a.server.URL+"/api/matching/movie/"+file.ID+"/selection", map[string]any{"kind": "movie", "tmdbId": 30}, 200)
	request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/progress", map[string]any{"position": 9, "duration": 60, "completed": false}, 200)
	a.close()
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`ALTER TABLE media_directories DROP COLUMN metadata_sources;
 ALTER TABLE media_directories DROP COLUMN name;
 ALTER TABLE metadata_cache RENAME TO new_cache; ALTER TABLE metadata_matches RENAME TO new_matches;
 CREATE TABLE metadata_cache(kind TEXT NOT NULL,tmdb_id INTEGER NOT NULL,payload TEXT NOT NULL,expires_at INTEGER NOT NULL,poster BLOB,poster_type TEXT NOT NULL,PRIMARY KEY(kind,tmdb_id));
 CREATE TABLE metadata_matches(kind TEXT NOT NULL,target_id TEXT NOT NULL,tmdb_id INTEGER NOT NULL DEFAULT 0,selection TEXT NOT NULL DEFAULT 'automatic',status TEXT NOT NULL,error TEXT NOT NULL DEFAULT '',PRIMARY KEY(kind,target_id));
 INSERT INTO metadata_cache SELECT kind,source_id,json_remove(payload,'$.sourceId'),expires_at,poster,poster_type FROM new_cache;
 INSERT INTO metadata_matches SELECT kind,target_id,source_id,selection,status,error FROM new_matches;
 DROP TABLE new_cache; DROP TABLE new_matches;`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	before := tmdb.count()
	a = start(t, config)
	login(t, a)
	awaitMetadata(t, a)
	after := sourceFile(t, a, dir.ID)
	if after.DirectoryName != filepath.Base(root) {
		t.Fatalf("legacy library name not migrated: %+v", after)
	}
	if after.Match.Metadata.SourceID != 30 || after.Match.Metadata.TMDBID != 30 || after.Match.Selection != "manual" || after.Progress.Position != 9 || tmdb.count() != before {
		t.Fatalf("legacy migration lost state: %+v", after)
	}
	body, _ := request(t, a.client, "GET", a.server.URL+after.Match.Metadata.PosterURL, nil, 200)
	if !bytes.Equal(body, tinyPoster) {
		t.Fatal("legacy poster lost")
	}
	a.close()
	a = start(t, config)
	login(t, a)
	awaitMetadata(t, a)
	if sourceFile(t, a, dir.ID).Match.Metadata.SourceID != 30 {
		t.Fatal("migration not idempotent")
	}
}
