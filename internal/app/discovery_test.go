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

type discoveryLibrary struct {
	Items  []app.MediaFile     `json:"items"`
	Series []app.SeriesSummary `json:"series"`
}

func readDiscovery(t *testing.T, a *runningApp) discoveryLibrary {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/library?sort=recent", nil, 200)
	var result discoveryLibrary
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHouseholdFavoritesPersistAndDoNotWriteProgress(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	writeMedia(t, filepath.Join(root, "movies", "favorite.mp4"), "isolated movie")
	writeMedia(t, filepath.Join(root, "series", "家庭.S01E01.mp4"), "isolated episode")
	a := start(t, app.Config{DataDir: data, Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	dir := addDirectory(t, a, filepath.Join(root, "movies"))
	awaitScan(t, a, dir.ID, "completed")
	addSeriesDirectory(t, a, filepath.Join(root, "series"))
	initial := readDiscovery(t, a)
	var movie app.MediaFile
	for _, file := range initial.Items {
		if file.Kind == "movie" {
			movie = file
		}
	}
	show := initial.Series[0]
	base := a.server.URL + "/api/favorites/media/" + movie.ID
	before, _ := request(t, a.client, "GET", a.server.URL+"/api/media/"+movie.ID+"/progress", nil, 200)
	request(t, http.DefaultClient, "POST", base, map[string]bool{"favorite": true}, 401)
	for _, body := range []any{map[string]any{}, map[string]any{"favorite": nil}, map[string]any{"favorite": "true"}, map[string]any{"favorite": true, "path": "/tmp/other"}} {
		request(t, a.client, "POST", base, body, 400)
	}
	request(t, a.client, "POST", a.server.URL+"/api/favorites/unknown/"+movie.ID, map[string]bool{"favorite": true}, 400)
	request(t, a.client, "POST", a.server.URL+"/api/favorites/media/missing", map[string]bool{"favorite": true}, 404)
	request(t, a.client, "POST", a.server.URL+"/api/favorites/series/"+movie.ID, map[string]bool{"favorite": true}, 404)
	stream, err := url.Parse(streamPath(t, a, movie.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/favorites/media/" + movie.ID, "/api/favorites/series/" + show.ID} {
		request(t, http.DefaultClient, "POST", a.server.URL+path+"?token="+stream.Query().Get("token"), map[string]bool{"favorite": true}, 401)
	}
	req, _ := http.NewRequest("POST", base, bytes.NewBufferString(`{"favorite":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://untrusted.example")
	response, err := a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-site favorite was not rejected")
	}
	first, _ := request(t, a.client, "POST", base, map[string]bool{"favorite": true}, 200)
	second, _ := request(t, a.client, "POST", base, map[string]bool{"favorite": true}, 200)
	if !bytes.Equal(first, second) {
		t.Fatal("setting the same state must preserve the favorite timestamp")
	}
	request(t, a.client, "POST", a.server.URL+"/api/favorites/series/"+show.ID, map[string]bool{"favorite": true}, 200)
	after, _ := request(t, a.client, "GET", a.server.URL+"/api/media/"+movie.ID+"/progress", nil, 200)
	if !bytes.Equal(before, after) {
		t.Fatal("favorites wrote watch progress")
	}
	a.close()
	b := start(t, app.Config{DataDir: data, MediaRoots: []string{root}})
	login(t, b)
	stored := readDiscovery(t, b)
	if !stored.Series[0].Favorite || stored.Series[0].FavoritedAt <= 0 {
		t.Fatal("series favorite did not survive restart")
	}
	for _, file := range stored.Items {
		if (file.ID == movie.ID) != file.Favorite {
			t.Fatal("movie/series/episode favorite states are not independent")
		}
	}
	rescan(t, b, dir.ID)
	awaitScan(t, b, dir.ID, "completed")
	body, _ := request(t, b.client, "GET", b.server.URL+"/api/media/"+movie.ID, nil, 200)
	var detail app.MediaFile
	json.Unmarshal(body, &detail)
	if !detail.Favorite || detail.AddedAt != movie.AddedAt {
		t.Fatal("rescan reset favorite or first-discovery time")
	}
	request(t, b.client, "POST", b.server.URL+"/api/favorites/media/"+movie.ID, map[string]bool{"favorite": false}, 200)
	request(t, b.client, "POST", b.server.URL+"/api/favorites/media/"+movie.ID, map[string]bool{"favorite": false}, 200)
	for _, file := range readDiscovery(t, b).Items {
		if file.ID == movie.ID && (file.Favorite || file.FavoritedAt != 0) {
			t.Fatal("favorite was not removed")
		}
	}
}

func TestNewEpisodesRefreshRecentSeriesAndIndependentFavorites(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	firstPath := filepath.Join(root, "First.S01E01.mp4")
	writeMedia(t, firstPath, "first episode")
	a := start(t, app.Config{DataDir: data, Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	dir := addSeriesDirectory(t, a, root)
	first := readDiscovery(t, a).Series[0]
	writeMedia(t, filepath.Join(root, "Second.S01E01.mp4"), "another series")
	rescan(t, a, dir.ID)
	awaitScan(t, a, dir.ID, "completed")
	if readDiscovery(t, a).Series[0].Title != "Second" {
		t.Fatal("new series did not appear first")
	}
	nextPath := filepath.Join(root, "First.S01E02.mp4")
	writeMedia(t, nextPath, "new episode")
	rescan(t, a, dir.ID)
	awaitScan(t, a, dir.ID, "completed")
	current := readDiscovery(t, a)
	if current.Series[0].ID != first.ID || current.Series[0].AddedAt <= current.Series[1].AddedAt {
		t.Fatal("adding an episode did not bring its series forward")
	}
	var episode app.MediaFile
	for _, file := range current.Items {
		if file.Path == firstPath {
			episode = file
			if file.AddedAt != first.AddedAt {
				t.Fatal("new episode changed the old episode discovery time")
			}
		}
	}
	request(t, a.client, "POST", a.server.URL+"/api/favorites/series/"+first.ID, map[string]bool{"favorite": true}, 200)
	request(t, a.client, "POST", a.server.URL+"/api/favorites/media/"+episode.ID, map[string]bool{"favorite": true}, 200)
	request(t, a.client, "POST", a.server.URL+"/api/favorites/series/"+first.ID, map[string]bool{"favorite": false}, 200)
	state := readDiscovery(t, a)
	if state.Series[0].Favorite {
		t.Fatal("series favorite was not removed")
	}
	for _, file := range state.Items {
		if file.ID == episode.ID && !file.Favorite {
			t.Fatal("removing series favorite removed independent episode favorite")
		}
	}

}

func TestRecentAdditionsKeepFirstDiscoveryAcrossRescans(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.mp4")
	writeMedia(t, firstPath, "first")
	if err := os.Chtimes(firstPath, time.Unix(2000000000, 0), time.Unix(2000000000, 0)); err != nil {
		t.Fatal(err)
	}
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	dir := addDirectory(t, a, root)
	awaitScan(t, a, dir.ID, "completed")
	first := readDiscovery(t, a).Items[0]
	lastPath := filepath.Join(root, "last.mp4")
	writeMedia(t, lastPath, "last")
	if err := os.Chtimes(lastPath, time.Unix(1000000000, 0), time.Unix(1000000000, 0)); err != nil {
		t.Fatal(err)
	}
	rescan(t, a, dir.ID)
	awaitScan(t, a, dir.ID, "completed")
	items := readDiscovery(t, a).Items
	if items[0].Filename != "last.mp4" || items[0].AddedAt <= first.AddedAt || items[1].AddedAt != first.AddedAt {
		t.Fatalf("wrong discovery order or unstable timestamps: %+v", items)
	}
	rescan(t, a, dir.ID)
	awaitScan(t, a, dir.ID, "completed")
	if again := readDiscovery(t, a).Items; again[0].AddedAt != items[0].AddedAt || again[1].AddedAt != items[1].AddedAt {
		t.Fatal("unchanged rescan bumped recently added items")
	}
	request(t, a.client, "GET", a.server.URL+"/api/library?sort=invalid", nil, 400)
}

func TestDiscoveryMigrationPreservesOldRecordsWithoutInventingDates(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	writeMedia(t, filepath.Join(root, "old.mp4"), "old")
	a := start(t, app.Config{DataDir: data, Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	dir := addDirectory(t, a, root)
	awaitScan(t, a, dir.ID, "completed")
	file := readDiscovery(t, a).Items[0]
	a.close()
	db, err := sql.Open("sqlite", filepath.Join(data, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	// Recreate the previous schema, not a mocked migration implementation.
	_, err = db.Exec(`DROP TABLE media_favorites; DROP TABLE series_favorites; DROP INDEX media_files_added; ALTER TABLE media_files DROP COLUMN added_at;`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	b := start(t, app.Config{DataDir: data, MediaRoots: []string{root}})
	login(t, b)
	got := readDiscovery(t, b).Items[0]
	if got.ID != file.ID || got.AddedAt != 0 || got.Favorite {
		t.Fatal("migration lost the file or invented a discovery date")
	}
	rescan(t, b, dir.ID)
	awaitScan(t, b, dir.ID, "completed")
	if readDiscovery(t, b).Items[0].AddedAt != 0 {
		t.Fatal("rescan changed an unknown historical discovery date")
	}
}
