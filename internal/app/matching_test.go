package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reelnest/internal/app"
)

func TestManualMatchingPreservesFilesVersionsProgressAndChoice(t *testing.T) {
	for _, kind := range []string{"movie", "series", "unorganized"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			f := fakeTMDB(t)
			config := f.config(t, root)
			names := []string{"Arrival (2016).webm", "other/Arrival (2016).webm"}
			directoryKind := kind
			if kind == "series" {
				names = []string{"Dark S01E01.mkv", "Dark S02E02.mkv"}
			}
			if kind == "unorganized" {
				directoryKind = "series"
				names = []string{"unknown.mkv", "unknown-other.mkv"}
			}
			for _, name := range names {
				writeMedia(t, filepath.Join(root, name), "unchanged "+name)
			}
			a := start(t, config)
			login(t, a)
			body, _ := request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]string{"path": root, "kind": directoryKind}, 201)
			var dir directory
			_ = json.Unmarshal(body, &dir)
			awaitScan(t, a, dir.ID, "completed")
			awaitMetadata(t, a)
			files := library(t, a)
			if len(files) != 2 {
				t.Fatal("missing files")
			}
			file := metadataMedia(t, a, files[0].ID)
			target := file.ID
			if file.Episode != nil {
				target = file.Episode.SeriesID
			}
			base := a.server.URL + "/api/matching/" + directoryKind + "/" + target
			request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/progress", map[string]any{"position": 15, "duration": 60, "completed": false}, 200)
			before := metadataMedia(t, a, file.ID)
			body, _ = request(t, a.client, "GET", base+"/search?q=Correct", nil, 200)
			var candidates struct {
				Items []app.TitleMetadata `json:"items"`
			}
			_ = json.Unmarshal(body, &candidates)
			if len(candidates.Items) != 1 || candidates.Items[0].Kind != directoryKind || candidates.Items[0].TMDBID != 30 {
				t.Fatalf("wrong candidate: %s", body)
			}
			// Search followed by cancel/no confirmation is a read-only operation.
			after := metadataMedia(t, a, file.ID)
			old, _ := json.Marshal(before)
			current, _ := json.Marshal(after)
			if !bytes.Equal(old, current) {
				t.Fatal("search changed original association")
			}
			request(t, a.client, "GET", base+"/search?q=Nothing", nil, 200)
			wrongKind := "series"
			if directoryKind == "series" {
				wrongKind = "movie"
			}
			request(t, a.client, "POST", base+"/selection", map[string]any{"kind": wrongKind, "tmdbId": 30}, 400)
			request(t, a.client, "GET", a.server.URL+"/api/matching/"+wrongKind+"/"+target+"/search?q=Correct", nil, 404)
			request(t, a.client, "POST", base+"/selection", map[string]any{"kind": directoryKind, "tmdbId": 30}, 200)
			after = metadataMedia(t, a, file.ID)
			if after.Match.Selection != "manual" || after.Match.Metadata.TMDBID != 30 || after.Match.Metadata.Title != "正确作品" || after.Progress.Position != 15 {
				t.Fatalf("wrong manual choice: %+v", after)
			}
			other := metadataMedia(t, a, files[1].ID)
			if kind == "series" {
				if other.Match.Metadata.TMDBID != 30 || other.Episode.SeriesID != file.Episode.SeriesID {
					t.Fatal("series matching split hierarchy")
				}
			} else if other.Match.Selection == "manual" {
				t.Fatal("distinct file versions merged")
			}
			for _, name := range names {
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil || string(data) != "unchanged "+name {
					t.Fatal("source altered")
				}
			}
			rescan(t, a, dir.ID)
			awaitScan(t, a, dir.ID, "completed")
			awaitMetadata(t, a)
			a.close()
			a = start(t, config)
			login(t, a)
			awaitMetadata(t, a)
			after = metadataMedia(t, a, file.ID)
			if after.Match.Selection != "manual" || after.Match.Metadata.TMDBID != 30 || after.Progress.Position != 15 {
				t.Fatal("rescan/restart replaced manual choice")
			}
			if (file.Episode == nil) != (after.Episode == nil) {
				t.Fatal("matching invented episode hierarchy")
			}
			f.setMode("error")
			base = a.server.URL + "/api/matching/" + directoryKind + "/" + target
			request(t, a.client, "POST", base+"/selection", map[string]any{"kind": directoryKind, "tmdbId": 10}, 503)
			after = metadataMedia(t, a, file.ID)
			if after.Match.Metadata.TMDBID != 30 || after.Match.Selection != "manual" {
				t.Fatal("failed confirmation lost valid association")
			}
			request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID+"/content", nil, 200)
		})
	}
}

func TestMetadataManagementRequiresHouseholdSession(t *testing.T) {
	root := t.TempDir()
	f := fakeTMDB(t)
	writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "media")
	a := start(t, f.config(t, root))
	cookie := login(t, a)
	dir := addDirectory(t, a, root)
	awaitScan(t, a, dir.ID, "completed")
	awaitMetadata(t, a)
	id := library(t, a)[0].ID
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/media/"+id+"/stream-link", map[string]any{}, 200)
	var link struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(body, &link)
	u, _ := url.Parse(link.Path)
	token := u.Query().Get("token")
	request(t, http.DefaultClient, "GET", a.server.URL+link.Path, nil, 200)
	for _, route := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/metadata/status", nil},
		{"POST", "/api/metadata/refresh", map[string]any{}},
		{"GET", "/api/matching/movie/" + id + "/search?q=Correct", nil},
		{"POST", "/api/matching/movie/" + id + "/selection", map[string]any{"kind": "movie", "tmdbId": 30}},
	} {
		path := route.path
		separator := "?"
		if len(path) > 7 && route.method == "GET" && route.path != "/api/metadata/status" {
			separator = "&"
		}
		request(t, http.DefaultClient, route.method, a.server.URL+path+separator+"token="+token, route.body, 401)
	}
	request(t, a.client, "POST", a.server.URL+"/api/logout", map[string]any{}, 200)
	req, _ := http.NewRequest("GET", a.server.URL+"/api/matching/movie/"+id+"/search?q=Correct", nil)
	req.AddCookie(cookie)
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("expired session accepted")
	}
}

func TestManualMatchingFailureWithoutCredentialsOrDuringTimeout(t *testing.T) {
	for _, mode := range []string{"no-token", "timeout", "rate"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			f := fakeTMDB(t)
			config := f.config(t, root)
			if mode == "no-token" {
				config.TMDBToken = ""
			} else {
				f.setMode(mode)
			}
			writeMedia(t, filepath.Join(root, "Nothing.webm"), "video")
			a := start(t, config)
			login(t, a)
			dir := addDirectory(t, a, root)
			awaitScan(t, a, dir.ID, "completed")
			awaitMetadata(t, a)
			id := library(t, a)[0].ID
			base := a.server.URL + "/api/matching/movie/" + id
			request(t, a.client, "GET", base+"/search?q=Correct", nil, 503)
			request(t, a.client, "POST", base+"/selection", map[string]any{"kind": "movie", "tmdbId": 30}, 503)
			if metadataMedia(t, a, id).Matched {
				t.Fatal("failed match reported success")
			}
			request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/content", nil, 200)
		})
	}
}

func TestManualChoiceSurvivesCacheExpiryAndFailedRefresh(t *testing.T) {
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
	request(t, a.client, "POST", a.server.URL+"/api/matching/movie/"+id+"/selection", map[string]any{"kind": "movie", "tmdbId": 30}, 200)
	a.close()
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("UPDATE metadata_cache SET expires_at=?", time.Now().Add(-time.Hour).Unix())
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	f.setMode("error")
	a = start(t, config)
	login(t, a)
	awaitMetadata(t, a)
	file := metadataMedia(t, a, id)
	if file.Matched || file.Match.Selection != "manual" || file.Match.Error == "" {
		t.Fatal("expired manual association lost or shown as fresh")
	}
	request(t, a.client, "GET", a.server.URL+"/api/media/"+id+"/content", nil, 200)
	a.close()
	f.setMode("success")
	a = start(t, config)
	login(t, a)
	awaitMetadata(t, a)
	file = metadataMedia(t, a, id)
	if !file.Matched || file.Match.Selection != "manual" || file.Match.Metadata.TMDBID != 30 {
		t.Fatalf("expired manual choice reverted to auto: %+v", file.Match)
	}
}
