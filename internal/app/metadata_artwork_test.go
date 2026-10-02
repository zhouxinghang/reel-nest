package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func serveArtworkImageFailure(w http.ResponseWriter, r *http.Request, mode string) bool {
	return servePhotoFailure(w, r, strings.Replace(mode, "artwork-", "photo-", 1))
}

func serveTMDBArtwork(w http.ResponseWriter, r *http.Request, mode string, series bool) {
	if mode == "artwork-metadata-error" {
		w.WriteHeader(503)
		return
	}
	id := 10
	if series {
		id = 20
	}
	if strings.Contains(r.URL.Path, "/30/") {
		id = 30
	}
	if mode == "artwork-wrong-id" {
		id = 999
	}
	images := []map[string]any{
		{"file_path": "/english.png", "iso_639_1": "en", "width": 1920, "height": 1080, "vote_average": 10},
		{"file_path": "/thumb.png", "iso_639_1": "zh", "width": 1280, "height": 720, "vote_average": 8},
		{"file_path": "/backdrop.png", "iso_639_1": nil, "width": 1920, "height": 1080, "vote_average": 9},
		{"file_path": "/lower.png", "iso_639_1": nil, "width": 1920, "height": 1080, "vote_average": 2},
		{"file_path": "/portrait.png", "iso_639_1": "zh", "width": 500, "height": 750, "vote_average": 10},
		{"file_path": "//evil.invalid/image.png", "iso_639_1": "zh", "width": 1920, "height": 1080, "vote_average": 10},
	}
	if mode == "artwork-empty" {
		images = []map[string]any{}
	}
	if mode == "artwork-unsafe" {
		images = images[len(images)-1:]
	}
	if mode == "artwork-portrait" {
		images = images[4:5]
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "backdrops": images})
}

func serveTVDBArtworkTypes(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []map[string]any{
		{"id": 3, "name": "Series Backgrounds", "recordType": "series"},
		{"id": 8, "name": "Movie Backgrounds", "recordType": "movie"},
		{"id": 7, "name": "Movie Posters", "recordType": "movie"},
		{"id": 1, "name": "Series Banners", "recordType": "series"},
	}})
}

func tvdbArtworkFixtures(mode string, series bool) []map[string]any {
	typ := 8
	if series {
		typ = 3
	}
	images := []map[string]any{
		{"image": "/english.png", "type": typ, "includesText": true, "language": "eng", "width": 1920, "height": 1080, "score": 10},
		{"image": "/thumb.png", "type": typ, "includesText": true, "language": "zho", "width": 1280, "height": 720, "score": 8},
		{"image": "/backdrop.png", "type": typ, "includesText": false, "language": "", "width": 1920, "height": 1080, "score": 9},
		{"image": "/lower.png", "type": typ, "includesText": false, "language": "", "width": 1920, "height": 1080, "score": 2},
		{"image": "/portrait.png", "type": 7, "includesText": true, "language": "zho", "width": 500, "height": 750, "score": 10},
		{"image": "http://169.254.169.254/image.png", "type": typ, "includesText": true, "language": "zho", "width": 1920, "height": 1080, "score": 10},
		{"image": "/banner.png", "type": 1, "includesText": true, "language": "zho", "width": 1920, "height": 1080, "score": 10},
	}
	if mode == "artwork-empty" {
		return []map[string]any{}
	}
	if mode == "artwork-unsafe" {
		return images[5:6]
	}
	if mode == "artwork-portrait" {
		return images[4:5]
	}
	return images
}

func TestTitleArtworkTypesSourcesPersistenceAndExpiry(t *testing.T) {
	for _, kind := range []string{"movie", "series"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
			config := tmdb.config(t, root)
			tvdb.configure(&config)
			name := "Arrival (2016).webm"
			if kind == "series" {
				name = "Dark S01E01.webm"
			}
			a := start(t, config)
			login(t, a)
			urls := []string{}
			for _, source := range []string{"tmdb", "tvdb"} {
				path := filepath.Join(root, source)
				writeMedia(t, filepath.Join(path, name), "unchanged video")
				dir := addSourceDirectory(t, a, path, kind, []string{source})
				file := sourceFile(t, a, dir.ID)
				m := file.Match.Metadata
				if m.Artwork == nil || m.Artwork.Status != "available" || !strings.HasSuffix(m.Artwork.ThumbPath, "/thumb.png") || !strings.HasSuffix(m.Artwork.BackdropPath, "/backdrop.png") {
					t.Fatalf("wrong artwork selection: %+v", m.Artwork)
				}
				if m.ThumbURL == m.BackdropURL || m.PosterURL == m.ThumbURL {
					t.Fatal("image types collapsed")
				}
				before := tmdb.count() + tvdb.count()
				_ = library(t, a)
				if tmdb.count()+tvdb.count() != before {
					t.Fatal("library eagerly downloads landscapes")
				}
				stream, _ := url.Parse(streamPath(t, a, file.ID))
				for _, imageURL := range []string{m.PosterURL, m.ThumbURL, m.BackdropURL} {
					data, response := request(t, a.client, "GET", a.server.URL+imageURL, nil, 200)
					if !bytes.Equal(data, tinyPoster) || response.Header.Get("Content-Type") != "image/png" {
						t.Fatal("bad image")
					}
					data, response = request(t, a.client, "HEAD", a.server.URL+imageURL, nil, 200)
					if len(data) != 0 || response.ContentLength != int64(len(tinyPoster)) {
						t.Fatal("invalid image HEAD")
					}
					request(t, http.DefaultClient, "GET", a.server.URL+imageURL, nil, 401)
					request(t, http.DefaultClient, "GET", a.server.URL+imageURL+"?"+stream.RawQuery, nil, 401)
					urls = append(urls, imageURL)
				}
				request(t, a.client, "GET", a.server.URL+strings.Replace(m.ThumbURL, "/thumb", "/logo", 1), nil, 404)
			}
			before := tmdb.count() + tvdb.count()
			a.close()
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			for _, u := range urls {
				request(t, a.client, "GET", a.server.URL+u, nil, 200)
			}
			if before != tmdb.count()+tvdb.count() {
				t.Fatal("restart discarded artwork cache")
			}
			saveSettings(t, a, "tmdb", map[string]any{"clearCredential": true})
			saveSettings(t, a, "tvdb", map[string]any{"clearCredential": true})
			a.close()
			db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec("UPDATE metadata_cache SET expires_at=?", time.Now().Add(-time.Hour).Unix()); err != nil {
				t.Fatal(err)
			}
			db.Close()
			config.TMDBToken, config.TVDBAPIKey = "", ""
			a = start(t, config)
			login(t, a)
			for _, u := range urls {
				request(t, a.client, "GET", a.server.URL+u, nil, 404)
			}
		})
	}
}

func TestTitleArtworkBackfillPreservesManualMetadataAndBackoff(t *testing.T) {
	for _, source := range []string{"tmdb", "tvdb"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
			config := tmdb.config(t, root)
			tvdb.configure(&config)
			writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "local video")
			a := start(t, config)
			login(t, a)
			dir := addSourceDirectory(t, a, root, "movie", []string{source})
			file := sourceFile(t, a, dir.ID)
			request(t, a.client, "POST", a.server.URL+"/api/matching/movie/"+file.ID+"/selection", map[string]any{"source": source, "sourceId": 30, "kind": "movie"}, 200)
			request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/progress", map[string]any{"position": 12, "duration": 60, "completed": false}, 200)
			original := metadataMedia(t, a, file.ID).Match.Metadata
			a.close()
			editCreditsCache(t, config, "json_remove(payload,'$.artwork','$.thumbUrl','$.backdropUrl')")
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			after := metadataMedia(t, a, file.ID)
			if after.Match.Selection != "manual" || after.Match.Metadata.SourceID != 30 || after.Progress.Position != 12 || after.Match.Metadata.ExpiresAt != original.ExpiresAt || after.Match.Metadata.Title != original.Title || after.Match.Metadata.PosterURL != original.PosterURL || after.Match.Metadata.ThumbURL == "" {
				t.Fatal("backfill changed existing metadata/state or failed")
			}
			a.close()
			editCreditsCache(t, config, "json_remove(payload,'$.artwork','$.thumbUrl','$.backdropUrl')")
			tmdb.setMode("error")
			tvdb.setMode("error")
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			failed := metadataMedia(t, a, file.ID)
			if !failed.Matched || failed.Match.Metadata.Artwork.Status != "failed" || failed.Match.Metadata.ExpiresAt != original.ExpiresAt || failed.Match.Metadata.Credits.Status != "available" {
				t.Fatal("supplemental failure erased valid metadata")
			}
			before := tmdb.count() + tvdb.count()
			request(t, a.client, "POST", a.server.URL+"/api/metadata/refresh", map[string]any{}, 202)
			awaitMetadata(t, a)
			if before != tmdb.count()+tvdb.count() {
				t.Fatal("artwork failure retried immediately")
			}
			request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID+"/content", nil, 200)
			a.close()
			editCreditsCache(t, config, "json_set(payload,'$.artwork.retryAt',0)")
			tmdb.setMode("")
			tvdb.setMode("")
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			if metadataMedia(t, a, file.ID).Match.Metadata.ThumbURL == "" {
				t.Fatal("artwork never retried")
			}
		})
	}
}

func TestTitleArtworkMissingOrUnsafeReferences(t *testing.T) {
	for _, source := range []string{"tmdb", "tvdb"} {
		for _, mode := range []string{"artwork-empty", "artwork-unsafe", "artwork-portrait"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
				tmdb.setMode(mode)
				tvdb.setMode(mode)
				config := tmdb.config(t, root)
				tvdb.configure(&config)
				writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "video")
				a := start(t, config)
				login(t, a)
				dir := addSourceDirectory(t, a, root, "movie", []string{source})
				file := sourceFile(t, a, dir.ID)
				m := file.Match.Metadata
				if m == nil || m.Artwork.Status != "available" || m.PosterURL == "" || m.ThumbURL != "" || m.BackdropURL != "" {
					t.Fatalf("unsafe artwork or missing poster: %+v", m)
				}
				before := tmdb.count() + tvdb.count()
				request(t, a.client, "GET", a.server.URL+fmt.Sprintf("/api/metadata/images/%s/movie/10/thumb", source), nil, 404)
				request(t, a.client, "POST", a.server.URL+"/api/metadata/refresh", map[string]any{}, 202)
				awaitMetadata(t, a)
				if before != tmdb.count()+tvdb.count() {
					t.Fatal("empty artwork retried")
				}
			})
		}
	}
}

func TestTitleArtworkDownloadsBoundedCoalescedAndNegativeCached(t *testing.T) {
	for _, source := range []string{"tmdb", "tvdb"} {
		for _, mode := range []string{"artwork-spoof", "artwork-type", "artwork-large", "artwork-redirect", "artwork-timeout"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
				config := tmdb.config(t, root)
				tvdb.configure(&config)
				writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "video")
				a := start(t, config)
				login(t, a)
				dir := addSourceDirectory(t, a, root, "movie", []string{source})
				file := sourceFile(t, a, dir.ID)
				m := file.Match.Metadata
				tmdb.setMode(mode)
				tvdb.setMode(mode)
				before := tmdb.count() + tvdb.count()
				var wg sync.WaitGroup
				for i := 0; i < 4; i++ {
					wg.Add(1)
					go func() { defer wg.Done(); request(t, a.client, "GET", a.server.URL+m.ThumbURL, nil, 404) }()
				}
				wg.Wait()
				if tmdb.count()+tvdb.count() != before+1 {
					t.Fatal("image requests not bounded/coalesced")
				}
				request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID+"/content", nil, 200)
				a.close()
				a = start(t, config)
				login(t, a)
				awaitMetadata(t, a)
				request(t, a.client, "GET", a.server.URL+m.ThumbURL, nil, 404)
				if tmdb.count()+tvdb.count() != before+1 {
					t.Fatal("image failure backoff lost on restart")
				}
			})
		}
	}
}
