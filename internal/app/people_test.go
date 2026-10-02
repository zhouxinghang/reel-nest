package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reelnest/internal/app"
)

func servePhotoFailure(w http.ResponseWriter, r *http.Request, mode string) bool {
	switch mode {
	case "photo-error":
		w.WriteHeader(500)
	case "photo-redirect":
		w.Header().Set("Location", "/unexpected-photo-redirect")
		w.WriteHeader(302)
	case "photo-type":
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, "<svg/>")
	case "photo-spoof":
		w.Header().Set("Content-Type", "image/png")
		fmt.Fprint(w, "<html>not an image</html>")
	case "photo-large":
		w.Header().Set("Content-Type", "image/png")
		fmt.Fprint(w, strings.Repeat("x", (5<<20)+1))
	case "photo-timeout":
		<-r.Context().Done()
	default:
		return false
	}
	return true
}

func personDetails(t *testing.T, a *runningApp, source, key string) app.PersonDetail {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/people/"+source+"/"+key, nil, 200)
	var person app.PersonDetail
	if json.Unmarshal(body, &person) != nil {
		t.Fatalf("invalid person: %s", body)
	}
	return person
}

func TestPeopleLargeLibraryLookupIsIndexed(t *testing.T) {
	const titleCount = 5000
	data := t.TempDir()
	a := start(t, app.Config{DataDir: data, Username: "family", Password: password})
	a.close()

	db, err := sql.Open("sqlite", filepath.Join(data, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`INSERT INTO media_directories(id,path,kind,scan_status,scan_error,scan_started,scan_finished,identity,metadata_sources) VALUES('large-library','/tmp/large-library','movie','completed','',0,0,'large-library','["tmdb"]')`); err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour).Unix()
	for i := 0; i < titleCount; i++ {
		personID := int64(100000 + i)
		photoPath := ""
		if i%100 == 0 {
			personID = 101
			photoPath = "/person.png"
		}
		metadata := app.TitleMetadata{
			Source:    "TMDB",
			SourceID:  int64(i + 1),
			Kind:      "movie",
			Title:     fmt.Sprintf("作品 %d", i),
			Year:      "2026",
			ExpiresAt: expires,
			Credits: &app.TitleCredits{Status: "available", Cast: []app.CastMember{{
				CreditPerson: app.CreditPerson{ID: personID, Name: "演员", PhotoPath: photoPath},
			}}, Directors: []app.CreditPerson{}},
		}
		payload, err := json.Marshal(metadata)
		if err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprintf("large-%08d", i)
		if _, err = tx.Exec(`INSERT INTO media_files(id,directory_id,relative_path,filename,size,modified_at,fingerprint,available,added_at) VALUES(?,'large-library',?,?,1,1,?,1,1)`, id, id+".mkv", id+".mkv", id); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO metadata_cache(source,kind,source_id,payload,expires_at,poster,poster_type) VALUES('tmdb','movie',?,?,?,NULL,'')`, i+1, string(payload), expires); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO metadata_matches(kind,target_id,source,source_id,policy_key,selection,status,error) VALUES('movie',?,'tmdb',?,'','automatic','matched','')`, id, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(`INSERT INTO person_photos(source,photo_path,photo,content_type,expires_at,retry_at) VALUES('tmdb','/person.png',?,'image/png',?,0)`, tinyPoster, expires); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	a = start(t, app.Config{DataDir: data})
	login(t, a)
	started := time.Now()
	person := personDetails(t, a, "tmdb", "101")
	elapsed := time.Since(started)
	t.Logf("indexed lookup across %d titles returned %d works in %s", titleCount, len(person.Works), elapsed)
	if len(person.Works) != titleCount/100 {
		t.Fatalf("got %d indexed works, want %d", len(person.Works), titleCount/100)
	}
	if elapsed > time.Second {
		t.Fatalf("large-library person lookup took %s, want <= 1s", elapsed)
	}
	started = time.Now()
	photo, _ := request(t, a.client, "GET", a.server.URL+person.PhotoURL, nil, 200)
	photoElapsed := time.Since(started)
	t.Logf("indexed photo identity lookup completed in %s", photoElapsed)
	if !bytes.Equal(photo, tinyPoster) {
		t.Fatal("cached indexed person photo changed")
	}
	if photoElapsed > time.Second {
		t.Fatalf("large-library person photo lookup took %s, want <= 1s", photoElapsed)
	}
}

func TestPeoplePhotosWorksIdentityAndPersistence(t *testing.T) {
	for _, source := range []string{"tmdb", "tvdb"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
			config := tmdb.config(t, root)
			tvdb.configure(&config)
			a := start(t, config)
			login(t, a)
			movies, series := filepath.Join(root, "movies"), filepath.Join(root, "series")
			writeMedia(t, filepath.Join(movies, "Arrival (2016).webm"), "movie")
			writeMedia(t, filepath.Join(movies, "copy", "Arrival (2016).webm"), "second version")
			writeMedia(t, filepath.Join(series, "Dark S01E01.webm"), "episode one")
			writeMedia(t, filepath.Join(series, "Dark S01E02.webm"), "episode two")
			dir := addSourceDirectory(t, a, movies, "movie", []string{source})
			movie := sourceFile(t, a, dir.ID)
			showDir := addSourceDirectory(t, a, series, "series", []string{source})
			episode := sourceFile(t, a, showDir.ID)
			c := movie.Match.Metadata.Credits
			if c.Cast[0].PhotoURL == "" || c.Directors[0].PhotoURL == "" || c.Cast[1].PhotoURL != "" || c.Cast[2].PhotoURL != "" {
				t.Fatalf("photo mapping or origin validation failed: %+v", c)
			}
			before := tmdb.count() + tvdb.count()
			for _, key := range []string{"101", "201", "102", "103"} {
				p := personDetails(t, a, source, key)
				if p.Key != key || len(p.Works) != 2 || p.Works[0].TargetKind != "series" || p.Works[0].ID != episode.Episode.SeriesID || p.Works[1].TargetKind != "media" || !p.Works[1].Available {
					t.Fatalf("incorrect library works/deduplication: %+v", p)
				}
				if len(p.Works[1].Jobs) != 1 || (key == "201" && p.Works[1].Jobs[0] != "导演") || p.Works[1].Metadata.Credits != nil {
					t.Fatalf("incorrect work role: %+v", p.Works[1])
				}
			}
			unknown := personDetails(t, a, source, c.Cast[3].Key)
			if len(unknown.Works) != 1 || unknown.Works[0].Metadata.Kind != "movie" {
				t.Fatal("missing IDs were merged by name across titles")
			}
			other := "tmdb"
			if source == "tmdb" {
				other = "tvdb"
			}
			request(t, a.client, "GET", a.server.URL+"/api/people/"+other+"/101", nil, 404)
			if tmdb.count()+tvdb.count() != before {
				t.Fatal("person works queried an external filmography")
			}
			// A public/stolen stream link is not authorization to read people/photos.
			for _, suffix := range []string{"", "/photo"} {
				request(t, http.DefaultClient, "GET", a.server.URL+"/api/people/"+source+"/101"+suffix+"?token=anything", nil, 401)
			}
			request(t, a.client, "GET", a.server.URL+"/api/people/evil/101", nil, 400)
			request(t, a.client, "GET", a.server.URL+"/api/people/"+source+"/no-id", nil, 400)
			request(t, a.client, "GET", a.server.URL+"/api/people/"+source+"/102/photo", nil, 404)
			photoURL := a.server.URL + c.Cast[0].PhotoURL
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					body, res := request(t, a.client, "GET", photoURL, nil, 200)
					if !bytes.Equal(body, tinyPoster) || res.Header.Get("Content-Type") != "image/png" || res.Header.Get("Cache-Control") != "no-store" {
						t.Error("bad cached photo response")
					}
				}()
			}
			wg.Wait()
			if tmdb.count()+tvdb.count() != before+1 {
				t.Fatal("concurrent photo downloads not coalesced")
			}
			body, res := request(t, a.client, "HEAD", photoURL, nil, 200)
			if len(body) != 0 || res.ContentLength != int64(len(tinyPoster)) {
				t.Fatal("invalid photo HEAD")
			}
			request(t, a.client, "GET", a.server.URL+c.Directors[0].PhotoURL, nil, 200)
			before = tmdb.count() + tvdb.count()
			saveSettings(t, a, source, map[string]any{"clearCredential": true})
			a.close()
			config.TMDBToken, config.TVDBAPIKey = "", ""
			a = start(t, config)
			login(t, a)
			p := personDetails(t, a, source, "101")
			request(t, a.client, "GET", a.server.URL+p.PhotoURL, nil, 200)
			if tmdb.count()+tvdb.count() != before {
				t.Fatal("photo not persisted for offline restart")
			}
		})
	}
}

func TestPeoplePhotoFailuresAreBoundedAndDoNotBreakMetadata(t *testing.T) {
	for _, source := range []string{"tmdb", "tvdb"} {
		for _, mode := range []string{"photo-error", "photo-redirect", "photo-type", "photo-spoof", "photo-large", "photo-timeout"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
				config := tmdb.config(t, root)
				tvdb.configure(&config)
				writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "local movie")
				a := start(t, config)
				login(t, a)
				dir := addSourceDirectory(t, a, root, "movie", []string{source})
				file := sourceFile(t, a, dir.ID)
				photoURL := file.Match.Metadata.Credits.Cast[0].PhotoURL
				tmdb.setMode(mode)
				tvdb.setMode(mode)
				before := tmdb.count() + tvdb.count()
				request(t, a.client, "GET", a.server.URL+photoURL, nil, 404)
				request(t, a.client, "GET", a.server.URL+photoURL, nil, 404)
				if tmdb.count()+tvdb.count() != before+1 {
					t.Fatal("photo failure retried or redirected")
				}
				// Artwork has an independent cooldown; an image 500 must not poison title APIs.
				request(t, a.client, "POST", a.server.URL+"/api/matching/movie/"+file.ID+"/selection", map[string]any{"kind": "movie", "source": source, "sourceId": 30}, 200)
				request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID+"/content", nil, 200)
				before = tmdb.count() + tvdb.count()
				a.close()
				a = start(t, config)
				login(t, a)
				awaitMetadata(t, a)
				request(t, a.client, "GET", a.server.URL+photoURL, nil, 404)
				if tmdb.count()+tvdb.count() != before {
					t.Fatal("photo backoff not preserved on restart")
				}
				if mode == "photo-error" {
					a.close()
					db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
					if err != nil {
						t.Fatal(err)
					}
					_, err = db.Exec("UPDATE person_photos SET retry_at=0")
					db.Close()
					if err != nil {
						t.Fatal(err)
					}
					tmdb.setMode("")
					tvdb.setMode("")
					a = start(t, config)
					login(t, a)
					awaitMetadata(t, a)
					request(t, a.client, "GET", a.server.URL+photoURL, nil, 200)
					if tmdb.count()+tvdb.count() != before+1 {
						t.Fatal("photo retry did not recover after backoff")
					}
				}
			})
		}
	}
}

func TestPeopleDualRolesAndExpiredPhotos(t *testing.T) {
	for _, source := range []string{"tmdb", "tvdb"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
			tmdb.setMode("credits-dual")
			tvdb.setMode("credits-dual")
			config := tmdb.config(t, root)
			tvdb.configure(&config)
			writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "movie")
			a := start(t, config)
			login(t, a)
			addSourceDirectory(t, a, root, "movie", []string{source})
			awaitMetadata(t, a)
			p := personDetails(t, a, source, "101")
			if len(p.Works) != 1 || len(p.Works[0].Jobs) != 2 || strings.Join(p.Works[0].Jobs, ",") != "演员,导演" || len(p.Works[0].Characters) != 2 {
				t.Fatalf("dual role duplicated work or lost roles: %+v", p)
			}
			request(t, a.client, "GET", a.server.URL+p.PhotoURL, nil, 200)
			db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err = db.Exec("UPDATE person_photos SET expires_at=0"); err != nil {
				t.Fatal(err)
			}
			before := tmdb.count() + tvdb.count()
			request(t, a.client, "GET", a.server.URL+p.PhotoURL, nil, 200)
			if tmdb.count()+tvdb.count() != before+1 {
				t.Fatal("expired photo not refreshed")
			}
		})
	}
}

func TestPeopleUpgradeRetainsNamesOnFailureAndRetries(t *testing.T) {
	for _, source := range []string{"tmdb", "tvdb"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
			config := tmdb.config(t, root)
			tvdb.configure(&config)
			writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "local movie")
			a := start(t, config)
			login(t, a)
			dir := addSourceDirectory(t, a, root, "movie", []string{source})
			file := sourceFile(t, a, dir.ID)
			request(t, a.client, "POST", a.server.URL+"/api/matching/movie/"+file.ID+"/selection", map[string]any{"kind": "movie", "source": source, "sourceId": 30}, 200)
			request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/progress", map[string]any{"position": 12, "duration": 60, "completed": false}, 200)
			original := metadataMedia(t, a, file.ID).Match.Metadata
			a.close()
			editCreditsCache(t, config, "json_remove(payload,'$.credits.version','$.credits.cast[0].photoPath','$.credits.cast[0].photoUrl')")
			tmdb.setMode("credits-error")
			tvdb.setMode("credits-error")
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			m := metadataMedia(t, a, file.ID)
			assertCredits(t, m.Match.Metadata)
			c := m.Match.Metadata.Credits
			if c.Version != 0 || c.Error == "" || c.RetryAt < time.Now().Add(23*time.Hour).Unix() || c.Cast[0].PhotoURL != "" || m.Match.Selection != "manual" || m.Progress.Position != 12 || m.Match.Metadata.ExpiresAt != original.ExpiresAt {
				t.Fatalf("old credits upgrade lost state/backoff: %+v", m)
			}
			before := tmdb.count() + tvdb.count()
			request(t, a.client, "POST", a.server.URL+"/api/metadata/refresh", map[string]any{}, 202)
			awaitMetadata(t, a)
			if tmdb.count()+tvdb.count() != before {
				t.Fatal("old credits retried immediately")
			}
			a.close()
			editCreditsCache(t, config, "json_set(payload,'$.credits.retryAt',0)")
			tmdb.setMode("")
			tvdb.setMode("")
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			m = metadataMedia(t, a, file.ID)
			if m.Match.Metadata.Credits.Version != 1 || m.Match.Metadata.Credits.Cast[0].PhotoURL == "" || m.Match.Metadata.ExpiresAt != original.ExpiresAt || m.Progress.Position != 12 {
				t.Fatal("photo upgrade not retried or changed title TTL/progress")
			}
		})
	}
}

func TestPeopleIgnoreOldMatchesExpiredCachesAndRemovedMedia(t *testing.T) {
	root := t.TempDir()
	fixture := fakeTMDB(t)
	config := fixture.config(t, root)
	path := filepath.Join(root, "Arrival (2016).webm")
	writeMedia(t, path, "local movie")
	a := start(t, config)
	login(t, a)
	dir := addSourceDirectory(t, a, root, "movie", []string{"tmdb"})
	file := sourceFile(t, a, dir.ID)
	oldKey := file.Match.Metadata.Credits.Cast[3].Key
	request(t, a.client, "POST", a.server.URL+"/api/matching/movie/"+file.ID+"/selection", map[string]any{"kind": "movie", "source": "tmdb", "sourceId": 30}, 200)
	request(t, a.client, "GET", a.server.URL+"/api/people/tmdb/"+oldKey, nil, 404)
	p := personDetails(t, a, "tmdb", "101")
	if len(p.Works) != 1 || p.Works[0].Metadata.SourceID != 30 {
		t.Fatal("old manual selection included")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	request(t, a.client, "POST", a.server.URL+"/api/directories/"+dir.ID+"/scan", map[string]any{}, 202)
	awaitScan(t, a, dir.ID, "completed")
	request(t, a.client, "GET", a.server.URL+"/api/people/tmdb/101", nil, 404)
	request(t, a.client, "GET", a.server.URL+p.PhotoURL, nil, 404)
	writeMedia(t, path, "new local movie")
	request(t, a.client, "POST", a.server.URL+"/api/directories/"+dir.ID+"/scan", map[string]any{}, 202)
	awaitScan(t, a, dir.ID, "completed")
	awaitMetadata(t, a)
	personDetails(t, a, "tmdb", "101")
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("UPDATE metadata_cache SET expires_at=0"); err != nil {
		t.Fatal(err)
	}
	request(t, a.client, "GET", a.server.URL+"/api/people/tmdb/101", nil, 404)
	request(t, a.client, "GET", a.server.URL+p.PhotoURL, nil, 404)
}
