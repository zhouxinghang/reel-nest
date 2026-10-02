package app_test

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reelnest/internal/app"
)

const fakeTMDBToken = "isolated-test-token-not-a-real-credential"

var tinyPoster, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aWQAAAABJRU5ErkJggg==")

type tmdbFixture struct {
	mu       sync.Mutex
	mode     string
	requests []string
	server   *httptest.Server
}

func fakeTMDB(t *testing.T) *tmdbFixture {
	t.Helper()
	f := &tmdbFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		mode := f.mode
		f.requests = append(f.requests, r.URL.String())
		f.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/images/") {
			if (strings.HasSuffix(r.URL.Path, "/thumb.png") || strings.HasSuffix(r.URL.Path, "/backdrop.png")) && serveArtworkImageFailure(w, r, mode) {
				return
			}
			if strings.HasSuffix(r.URL.Path, "/person.png") && servePhotoFailure(w, r, mode) {
				return
			}
			if r.Header.Get("Authorization") != "" {
				t.Error("credential sent to image server")
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(tinyPoster)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fakeTMDBToken {
			t.Error("missing server-side bearer token")
		}
		switch mode {
		case "timeout":
			<-r.Context().Done()
			return
		case "invalid":
			w.WriteHeader(401)
			fmt.Fprint(w, fakeTMDBToken)
			return
		case "error":
			w.WriteHeader(500)
			fmt.Fprint(w, fakeTMDBToken)
			return
		case "rate":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
			return
		case "malformed":
			fmt.Fprint(w, "not JSON "+fakeTMDBToken)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		series := strings.Contains(r.URL.Path, "/tv")
		if strings.HasSuffix(r.URL.Path, "/images") {
			serveTMDBArtwork(w, r, mode, series)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/credits") || strings.HasSuffix(r.URL.Path, "/aggregate_credits") {
			serveTMDBCredits(t, w, r, mode, series)
			return
		}
		if strings.Contains(r.URL.Path, "/search/") {
			query := r.URL.Query().Get("query")
			if r.URL.Query().Get("language") != "zh-CN" || r.URL.Query().Get("include_adult") != "false" {
				t.Error("search not Chinese-first/non-adult")
			}
			results := []map[string]any{}
			if mode != "none" && query != "Nothing" {
				title, original, id, year := "降临", "Arrival", int64(10), "2016-01-01"
				if series {
					title, original, id, year = "暗黑", "Dark", 20, "2017-01-01"
				}
				if query == "Correct" {
					title, original, id, year = "正确作品", "Correct", 30, "2020-01-01"
				}
				candidate := map[string]any{"id": id, "title": title, "original_title": original, "release_date": year}
				if series {
					candidate = map[string]any{"id": id, "name": title, "original_name": original, "first_air_date": year}
				}
				results = append(results, candidate)
				if mode == "ambiguous" {
					results = append(results, map[string]any{"id": 99, "title": title, "original_title": original, "release_date": year})
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "total_pages": 1, "total_results": len(results)})
			return
		}
		id := int64(10)
		title, original, year := "降临", "Arrival", "2016-01-01"
		if series {
			id, title, original, year = 20, "暗黑", "Dark", "2017-01-01"
		}
		if strings.HasSuffix(r.URL.Path, "/30") {
			id, title, original, year = 30, "正确作品", "Correct", "2020-01-01"
		}
		overview, poster := "中文简介", "/poster.png"
		if mode == "fallback" {
			title, overview, poster = "", "", ""
			if r.URL.Query().Get("language") == "en-US" {
				title, overview, poster = original, "English overview", "/poster.png"
			}
		}
		body := map[string]any{"id": id, "title": title, "original_title": original, "overview": overview, "release_date": year, "poster_path": poster}
		if series {
			delete(body, "title")
			delete(body, "original_title")
			delete(body, "release_date")
			body["name"] = title
			body["original_name"] = original
			body["first_air_date"] = year
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(f.server.Close)
	return f
}
func (f *tmdbFixture) setMode(mode string) { f.mu.Lock(); defer f.mu.Unlock(); f.mode = mode }
func (f *tmdbFixture) count() int          { f.mu.Lock(); defer f.mu.Unlock(); return len(f.requests) }
func (f *tmdbFixture) config(t *testing.T, root string) app.Config {
	return app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}, TMDBToken: fakeTMDBToken, TMDBAPIURL: f.server.URL, TMDBImageURL: f.server.URL + "/images", TMDBTimeout: 300 * time.Millisecond}
}

func awaitMetadata(t *testing.T, a *runningApp) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		body, _ := request(t, a.client, "GET", a.server.URL+"/api/metadata/status", nil, 200)
		var status struct {
			Running    bool  `json:"running"`
			FinishedAt int64 `json:"finishedAt"`
		}
		if err := json.Unmarshal(body, &status); err != nil {
			t.Fatal(err)
		}
		if !status.Running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("metadata job did not finish")
}
func metadataMedia(t *testing.T, a *runningApp, id string) app.MediaFile {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/media/"+id, nil, 200)
	var f app.MediaFile
	if json.Unmarshal(body, &f) != nil {
		t.Fatal(string(body))
	}
	return f
}

func TestTMDBAutomaticChineseMetadataPersistsWithoutChangingMedia(t *testing.T) {
	root := t.TempDir()
	f := fakeTMDB(t)
	config := f.config(t, root)
	video := filepath.Join(root, "Arrival (2016).webm")
	writeMedia(t, video, "real isolated video bytes")
	a := start(t, config)
	login(t, a)
	dir := addDirectory(t, a, root)
	awaitScan(t, a, dir.ID, "completed")
	awaitMetadata(t, a)
	file := metadataMedia(t, a, library(t, a)[0].ID)
	if !file.Matched || file.Match.Metadata.Title != "降临" || file.Match.Metadata.Year != "2016" || file.Match.Metadata.Overview != "中文简介" || file.Match.Metadata.Source != "TMDB" {
		t.Fatalf("wrong metadata: %+v", file.Match)
	}
	if file.Match.Metadata.ExpiresAt-file.Match.Metadata.FetchedAt > 31*24*3600 {
		t.Fatal("cache retention too long")
	}
	poster, _ := request(t, a.client, "GET", a.server.URL+file.Match.Metadata.PosterURL, nil, 200)
	if !bytes.Equal(poster, tinyPoster) {
		t.Fatal("wrong poster")
	}
	request(t, http.DefaultClient, "GET", a.server.URL+file.Match.Metadata.PosterURL, nil, 401)
	request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/progress", map[string]any{"position": 12, "duration": 60, "completed": false}, 200)
	before := f.count()
	rescan(t, a, dir.ID)
	awaitScan(t, a, dir.ID, "completed")
	awaitMetadata(t, a)
	if f.count() != before {
		t.Fatal("valid cache fetched again")
	}
	a.close()
	config.Username = ""
	config.Password = ""
	a = start(t, config)
	login(t, a)
	awaitMetadata(t, a)
	after := metadataMedia(t, a, file.ID)
	if !after.Matched || after.Match.Metadata.TMDBID != 10 || after.Progress == nil || after.Progress.Position != 12 {
		t.Fatalf("state not preserved: %+v", after)
	}
	if f.count() != before {
		t.Fatal("restart discarded cache")
	}
	contents, _ := os.ReadFile(video)
	if string(contents) != "real isolated video bytes" {
		t.Fatal("source changed")
	}
}

func TestTMDBSeriesAndEnglishFallback(t *testing.T) {
	root := t.TempDir()
	f := fakeTMDB(t)
	f.setMode("fallback")
	writeMedia(t, filepath.Join(root, "Dark S01E01.mkv"), "episode one")
	writeMedia(t, filepath.Join(root, "Dark S02E02.mkv"), "episode two")
	a := start(t, f.config(t, root))
	login(t, a)
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]string{"name": "测试媒体库", "path": root, "kind": "series"}, 201)
	var dir directory
	_ = json.Unmarshal(body, &dir)
	awaitScan(t, a, dir.ID, "completed")
	awaitMetadata(t, a)
	files := library(t, a)
	if len(files) != 2 {
		t.Fatal("files merged")
	}
	for _, local := range files {
		file := metadataMedia(t, a, local.ID)
		if file.Episode == nil || !file.Matched || file.Match.Metadata.Title != "Dark" || file.Match.Metadata.Overview != "English overview" {
			t.Fatalf("fallback failed: %+v", file)
		}
	}
	body, _ = request(t, a.client, "GET", a.server.URL+"/api/series/"+files[0].Episode.SeriesID, nil, 200)
	var show app.SeriesDetail
	_ = json.Unmarshal(body, &show)
	if !show.Matched || show.Match.Metadata.Kind != "series" || show.SeasonCount != 2 || show.FileCount != 2 {
		t.Fatalf("series changed: %+v", show)
	}
}

func TestTMDBFailuresNeverBlockLocalFiles(t *testing.T) {
	for _, mode := range []string{"no-token", "invalid", "timeout", "error", "rate", "malformed", "none", "ambiguous"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			f := fakeTMDB(t)
			f.setMode(mode)
			config := f.config(t, root)
			if mode == "no-token" {
				config.TMDBToken = ""
			}
			writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "playable boundary bytes")
			a := start(t, config)
			login(t, a)
			dir := addDirectory(t, a, root)
			awaitScan(t, a, dir.ID, "completed")
			awaitMetadata(t, a)
			files := library(t, a)
			if len(files) != 1 || files[0].Matched || !files[0].Available {
				t.Fatalf("local media affected: %+v", files)
			}
			body, _ := request(t, a.client, "GET", a.server.URL+"/api/media/"+files[0].ID+"/content", nil, 200)
			if string(body) != "playable boundary bytes" {
				t.Fatal("cannot read original video")
			}
			body, _ = request(t, a.client, "GET", a.server.URL+"/api/media/"+files[0].ID, nil, 200)
			if bytes.Contains(body, []byte(fakeTMDBToken)) {
				t.Fatal("credential leaked")
			}
			file := metadataMedia(t, a, files[0].ID)
			if mode != "no-token" && file.Match.Error == "" {
				t.Fatal("missing failure feedback")
			}
			if mode == "rate" || mode == "error" || mode == "invalid" {
				before := f.count()
				request(t, a.client, "POST", a.server.URL+"/api/metadata/refresh", map[string]any{}, 202)
				awaitMetadata(t, a)
				if f.count() != before {
					t.Fatal("upstream cooldown ignored")
				}
			}
			if mode == "no-token" && f.count() != 0 {
				t.Fatal("called TMDB without credentials")
			}
		})
	}
}

func TestTMDBExpiredCacheFallsBackButKeepsProgressAndAssociation(t *testing.T) {
	root := t.TempDir()
	f := fakeTMDB(t)
	config := f.config(t, root)
	writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "video")
	a := start(t, config)
	login(t, a)
	dir := addDirectory(t, a, root)
	awaitScan(t, a, dir.ID, "completed")
	awaitMetadata(t, a)
	id := library(t, a)[0].ID
	file := metadataMedia(t, a, id)
	poster := file.Match.Metadata.PosterURL
	request(t, a.client, "POST", a.server.URL+"/api/media/"+id+"/progress", map[string]any{"position": 8, "duration": 60, "completed": false}, 200)
	a.close()
	// Age a real persisted cache fixture, not the application clock or storage API.
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE metadata_cache SET expires_at=?", time.Now().Add(-time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	config.TMDBToken = ""
	a = start(t, config)
	login(t, a)
	file = metadataMedia(t, a, id)
	if file.Matched || file.Match.Status != "expired" || file.Progress == nil || file.Progress.Position != 8 {
		t.Fatalf("expired cache used or state lost: %+v", file)
	}
	request(t, a.client, "GET", a.server.URL+poster, nil, 404)
	request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/content", nil, 200)
	a.close()
	config.TMDBToken = fakeTMDBToken
	a = start(t, config)
	login(t, a)
	awaitMetadata(t, a)
	file = metadataMedia(t, a, id)
	if !file.Matched || file.Match.Metadata.TMDBID != 10 || file.Progress.Position != 8 {
		t.Fatal("association not restored")
	}
}
