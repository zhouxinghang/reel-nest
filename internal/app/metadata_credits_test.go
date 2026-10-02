package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reelnest/internal/app"
)

// Wire-shaped external fixtures, including out-of-order/duplicate entries and
// non-director crew. Only provider HTTP is replaced; all app/storage paths run.
func creditsFailure(w http.ResponseWriter, r *http.Request, mode string) bool {
	switch mode {
	case "credits-error":
		w.WriteHeader(500)
	case "credits-malformed":
		fmt.Fprint(w, "invalid JSON")
	case "credits-large":
		fmt.Fprint(w, strings.Repeat("x", (1<<20)+1))
	case "credits-timeout":
		<-r.Context().Done()
	default:
		return false
	}
	return true
}

func serveTMDBCredits(t *testing.T, w http.ResponseWriter, r *http.Request, mode string, series bool) {
	t.Helper()
	if r.URL.Query().Get("language") != "zh-CN" || (series && !strings.HasSuffix(r.URL.Path, "/aggregate_credits")) {
		t.Error("incorrect TMDB credits endpoint/language")
	}
	if creditsFailure(w, r, mode) {
		return
	}
	id := 10
	if series {
		id = 20
	}
	if strings.Contains(r.URL.Path, "/30/") {
		id = 30
	}
	if mode == "credits-wrong-id" {
		id = 999
	}
	cast := []map[string]any{
		{"id": 102, "name": "同名演员", "character": "配角", "order": 2, "profile_path": "//evil.invalid/person.png"},
		{"id": 101, "name": " 主演 ", "character": "角色甲", "order": 0},
		{"id": 101, "name": "主演", "character": "角色乙", "order": 1, "profile_path": "/person.png"},
		{"id": 101, "name": "主演", "character": " 角色甲 ", "order": 1},
		{"id": 103, "name": "同名演员", "order": 3, "profile_path": "/../person.png"},
		{"id": 104, "name": " ", "character": "不能当姓名", "order": 4},
		{"name": "无编号演员", "character": "角色丙", "order": 5},
		{"name": " 无编号演员 ", "character": "角色丁", "order": 6},
	}
	if series {
		for _, p := range cast {
			p["roles"] = []map[string]any{{"character": p["character"], "episode_count": 2}}
			delete(p, "character")
		}
	}
	crew := []map[string]any{
		{"id": 201, "name": "导演甲", "job": "Director", "profile_path": "/director.png"},
		{"id": 201, "name": "导演甲", "job": "Director"},
		{"id": 202, "name": "导演乙", "job": "Director"},
		{"id": 301, "name": "非导演编剧", "job": "Writer", "department": "Directing"},
		{"id": 302, "name": "非导演助理", "job": "Assistant Director"},
		{"id": 303, "name": "非导演创作者", "job": "Creator"},
	}
	if mode == "credits-dual" {
		crew = append(crew, map[string]any{"id": 101, "name": "主演", "job": "Director"})
	}
	if series {
		for _, p := range crew {
			p["jobs"] = []map[string]any{{"job": p["job"], "episode_count": 2}}
			delete(p, "job")
		}
	}
	if mode == "credits-empty" {
		cast, crew = []map[string]any{}, []map[string]any{}
	}
	if mode == "credits-many" {
		for i := 0; i < 110; i++ {
			cast = append(cast, map[string]any{"id": 1000 + i, "name": fmt.Sprintf("演员%d", i), "order": 10 + i})
		}
		for i := 0; i < 60; i++ {
			crew = append(crew, map[string]any{"id": 2000 + i, "name": fmt.Sprintf("导演%d", i), "job": "Director"})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "cast": cast, "crew": crew})
}

func serveTVDBCredits(t *testing.T, w http.ResponseWriter, r *http.Request, mode string) {
	t.Helper()
	if r.URL.Query().Get("meta") != "" {
		t.Error("credits need not refetch translations")
	}
	if creditsFailure(w, r, mode) {
		return
	}
	id := 10
	if strings.Contains(r.URL.Path, "/30/") {
		id = 30
	}
	if mode == "credits-wrong-id" {
		id = 999
	}
	characters := []map[string]any{
		{"peopleId": 102, "personName": "同名演员", "name": "配角", "peopleType": "Actor", "sort": 2, "personImgURL": "https://evil.invalid/person.png"},
		{"peopleId": 101, "personName": " 主演 ", "name": "角色甲", "peopleType": "Actor", "sort": 0},
		{"peopleId": 101, "personName": "主演", "name": "角色乙", "peopleType": "Actor", "sort": 1, "personImgURL": "/person.png"},
		{"peopleId": 101, "personName": "主演", "name": " 角色甲 ", "peopleType": "Actor", "sort": 1},
		{"peopleId": 103, "personName": "同名演员", "peopleType": "Actor", "sort": 3, "image": "/character.png"},
		{"peopleId": 104, "name": "不能当姓名", "peopleType": "Actor", "sort": 4},
		{"personName": "无编号演员", "name": "角色丙", "peopleType": "Actor", "sort": 5},
		{"personName": " 无编号演员 ", "name": "角色丁", "peopleType": "Actor", "sort": 6},
		{"peopleId": 201, "personName": "导演甲", "name": "Director", "peopleType": "Director", "sort": 5, "personImgURL": "/director.png"},
		{"peopleId": 201, "personName": "导演甲", "peopleType": "Director", "sort": 6},
		{"peopleId": 202, "personName": "导演乙", "peopleType": "Director", "sort": 7},
		{"peopleId": 301, "personName": "非导演编剧", "peopleType": "Writer"},
		{"peopleId": 302, "personName": "非导演助理", "peopleType": "Assistant Director"},
		{"peopleId": 303, "personName": "非导演创作者", "peopleType": "Creator"},
		{"peopleId": 304, "personName": "未明确职业", "type": 3},
	}
	if mode == "credits-dual" {
		characters = append(characters, map[string]any{"peopleId": 101, "personName": "主演", "peopleType": "Director"})
	}
	if mode == "credits-empty" {
		characters = []map[string]any{}
	}
	if mode == "credits-many" {
		for i := 0; i < 110; i++ {
			characters = append(characters, map[string]any{"peopleId": 1000 + i, "personName": fmt.Sprintf("演员%d", i), "peopleType": "Actor", "sort": 10 + i})
		}
		for i := 0; i < 60; i++ {
			characters = append(characters, map[string]any{"peopleId": 2000 + i, "personName": fmt.Sprintf("导演%d", i), "peopleType": "Director", "sort": 10 + i})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"id": id, "characters": characters, "artworks": tvdbArtworkFixtures(mode, strings.Contains(r.URL.Path, "/series/"))}})
}

func assertCredits(t *testing.T, m *app.TitleMetadata) {
	t.Helper()
	if m == nil || m.Credits == nil {
		t.Fatal("missing credits")
	}
	credits := *m.Credits
	credits.Cast = append([]app.CastMember{}, credits.Cast...)
	credits.Directors = append([]app.CreditPerson{}, credits.Directors...)
	for i := range credits.Cast {
		p := &credits.Cast[i].CreditPerson
		if p.Key == "" {
			t.Fatal("missing person key")
		}
		*p = app.CreditPerson{ID: p.ID, Name: p.Name}
	}
	for i := range credits.Directors {
		p := &credits.Directors[i]
		if p.Key == "" {
			t.Fatal("missing director key")
		}
		*p = app.CreditPerson{ID: p.ID, Name: p.Name}
	}
	wantCast := []app.CastMember{
		{CreditPerson: app.CreditPerson{ID: 101, Name: "主演"}, Roles: []string{"角色甲", "角色乙"}},
		{CreditPerson: app.CreditPerson{ID: 102, Name: "同名演员"}, Roles: []string{"配角"}},
		{CreditPerson: app.CreditPerson{ID: 103, Name: "同名演员"}, Roles: []string{}},
		{CreditPerson: app.CreditPerson{Name: "无编号演员"}, Roles: []string{"角色丙", "角色丁"}},
	}
	wantDirectors := []app.CreditPerson{{ID: 201, Name: "导演甲"}, {ID: 202, Name: "导演乙"}}
	if credits.Status != "available" || credits.FetchedAt <= 0 || !reflect.DeepEqual(credits.Cast, wantCast) || !reflect.DeepEqual(credits.Directors, wantDirectors) {
		t.Fatalf("wrong credits: %+v", credits)
	}
}

func TestCreditsSourcesMoviesSeriesAndManualPersistence(t *testing.T) {
	for _, source := range []string{"tmdb", "tvdb"} {
		for _, kind := range []string{"movie", "series"} {
			t.Run(source+"/"+kind, func(t *testing.T) {
				root := t.TempDir()
				tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
				config := tmdb.config(t, root)
				tvdb.configure(&config)
				name := "Arrival (2016).webm"
				if kind == "series" {
					name = "Dark S01E01.webm"
				}
				writeMedia(t, filepath.Join(root, name), "unchanged video")
				a := start(t, config)
				login(t, a)
				dir := addSourceDirectory(t, a, root, kind, []string{source})
				file := sourceFile(t, a, dir.ID)
				assertCredits(t, file.Match.Metadata)
				target := file.ID
				if kind == "series" {
					target = file.Episode.SeriesID
					body, _ := request(t, a.client, "GET", a.server.URL+"/api/series/"+target, nil, 200)
					var show app.SeriesDetail
					_ = json.Unmarshal(body, &show)
					assertCredits(t, show.Match.Metadata)
				}
				request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/progress", map[string]any{"position": 12, "duration": 60, "completed": false}, 200)
				request(t, a.client, "POST", a.server.URL+"/api/matching/"+kind+"/"+target+"/selection", map[string]any{"kind": kind, "source": source, "sourceId": 30}, 200)
				before := tmdb.count() + tvdb.count()
				a.close()
				a = start(t, config)
				login(t, a)
				awaitMetadata(t, a)
				after := metadataMedia(t, a, file.ID)
				assertCredits(t, after.Match.Metadata)
				if after.Match.Selection != "manual" || after.Match.Metadata.SourceID != 30 || after.Match.Metadata.Source != strings.ToUpper(source) || after.Progress.Position != 12 || tmdb.count()+tvdb.count() != before {
					t.Fatalf("state changed: %+v", after)
				}
			})
		}
	}
}

func TestCreditsEmptyBoundedAndFailureDoesNotLoseTitle(t *testing.T) {
	for _, source := range []string{"tmdb", "tvdb"} {
		for _, mode := range []string{"credits-empty", "credits-many", "credits-error", "credits-malformed", "credits-large", "credits-timeout", "credits-wrong-id"} {
			t.Run(source+"/"+mode, func(t *testing.T) {
				root := t.TempDir()
				tmdb, tvdb := fakeTMDB(t), fakeTVDB(t)
				config := tmdb.config(t, root)
				tvdb.configure(&config)
				if source == "tmdb" {
					tmdb.setMode(mode)
				} else {
					tvdb.setMode(mode)
				}
				writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "local video")
				a := start(t, config)
				login(t, a)
				sources := []string{"tmdb", "tvdb"}
				if source == "tvdb" {
					sources = []string{"tvdb", "tmdb"}
				}
				dir := addSourceDirectory(t, a, root, "movie", sources)
				file := sourceFile(t, a, dir.ID)
				m := file.Match.Metadata
				if !file.Matched || m.Source != strings.ToUpper(source) || m.Title == "" || m.Credits == nil {
					t.Fatalf("title lost: %+v", file)
				}
				poster, _ := request(t, a.client, "GET", a.server.URL+m.PosterURL, nil, 200)
				if !bytes.Equal(poster, tinyPoster) {
					t.Fatal("poster lost")
				}
				content, _ := request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID+"/content", nil, 200)
				if string(content) != "local video" {
					t.Fatal("credits failure affected video reads")
				}
				c := m.Credits
				switch mode {
				case "credits-empty":
					if c.Status != "available" || len(c.Cast) != 0 || len(c.Directors) != 0 {
						t.Fatalf("wrong empty result: %+v", c)
					}
				case "credits-many":
					if c.Status != "available" || len(c.Cast) != 100 || len(c.Directors) != 50 || !c.Truncated {
						t.Fatal("credits not bounded")
					}
				default:
					if c.Status != "failed" || c.Error == "" || c.RetryAt < time.Now().Add(23*time.Hour).Unix() {
						t.Fatalf("failure not observable/bounded: %+v", c)
					}
				}
				before := tmdb.count() + tvdb.count()
				request(t, a.client, "POST", a.server.URL+"/api/metadata/refresh", map[string]any{}, 202)
				awaitMetadata(t, a)
				if tmdb.count()+tvdb.count() != before {
					t.Fatal("credits retried immediately")
				}
				if (source == "tmdb" && tvdb.count() != 0) || (source == "tvdb" && tmdb.count() != 0) {
					t.Fatal("mixed credits from lower-priority source")
				}
			})
		}
	}
}

func editCreditsCache(t *testing.T, config app.Config, expr string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("UPDATE metadata_cache SET payload=" + expr); err != nil {
		t.Fatal(err)
	}
}

func TestCreditsUpgradePreservesManualTitlePosterProgressAndBackoff(t *testing.T) {
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
			request(t, a.client, "POST", a.server.URL+"/api/matching/movie/"+file.ID+"/selection", map[string]any{"kind": "movie", "source": source, "sourceId": 30}, 200)
			request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/progress", map[string]any{"position": 12, "duration": 60, "completed": false}, 200)
			original := metadataMedia(t, a, file.ID).Match.Metadata
			a.close()
			editCreditsCache(t, config, "json_remove(payload,'$.credits')")
			// No credential: keep the old payload readable. Restore credentials and fail
			// just the supplemental fetch; manual choice and the original TTL must survive.
			offline := config
			offline.TMDBToken = ""
			offline.TVDBAPIKey = ""
			a = start(t, offline)
			login(t, a)
			awaitMetadata(t, a)
			if m := metadataMedia(t, a, file.ID).Match.Metadata; m == nil || m.Credits != nil || m.Title != original.Title {
				t.Fatal("old cache not readable offline")
			}
			a.close()
			tmdb.setMode("credits-error")
			tvdb.setMode("credits-error")
			before := tmdb.count() + tvdb.count()
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			check := func(wantStatus string) {
				after := metadataMedia(t, a, file.ID)
				m := after.Match.Metadata
				if m == nil || m.Credits == nil || m.Credits.Status != wantStatus || m.Title != original.Title || m.FetchedAt != original.FetchedAt || m.ExpiresAt != original.ExpiresAt || m.SourceID != 30 || after.Match.Selection != "manual" || after.Progress.Position != 12 {
					t.Fatalf("upgrade lost state: %+v", after)
				}
				poster, _ := request(t, a.client, "GET", a.server.URL+m.PosterURL, nil, 200)
				if !bytes.Equal(poster, tinyPoster) {
					t.Fatal("upgrade lost poster")
				}
			}
			check("failed")
			expected := 1
			if source == "tvdb" {
				expected = 2
			} // login + full record
			if tmdb.count()+tvdb.count() != before+expected {
				t.Fatal("upgrade fetched unrelated title/poster/search")
			}
			before = tmdb.count() + tvdb.count()
			a.close()
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			check("failed")
			if tmdb.count()+tvdb.count() != before {
				t.Fatal("restart ignored persisted backoff")
			}
			a.close()
			editCreditsCache(t, config, "json_set(payload,'$.credits.retryAt',0)")
			tmdb.setMode("")
			tvdb.setMode("")
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			check("available")
			assertCredits(t, metadataMedia(t, a, file.ID).Match.Metadata)
		})
	}
}
