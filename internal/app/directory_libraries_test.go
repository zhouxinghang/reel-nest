package app_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reelnest/internal/app"
)

type libraryResponse struct {
	Directories []app.Directory     `json:"directories"`
	Items       []app.MediaFile     `json:"items"`
	Series      []app.SeriesSummary `json:"series"`
}

func readLibrary(t *testing.T, a *runningApp, query string) libraryResponse {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/library"+query, nil, 200)
	var result libraryResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestNamedLibrariesValidationIsolationAndRename(t *testing.T) {
	root := t.TempDir()
	config := app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}}
	a := start(t, config)
	login(t, a)
	path := filepath.Join(root, "first", "movies")
	writeMedia(t, filepath.Join(path, "same.webm"), "isolated movie")
	request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]any{"path": path, "kind": "movie"}, 400)
	for _, name := range []any{nil, 42, "", " \t\n", "bad\x00name", "bad\nname", strings.Repeat("名", 81)} {
		request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]any{"name": name, "path": path, "kind": "movie"}, 400)
	}
	if len(readLibrary(t, a, "").Directories) != 0 {
		t.Fatal("invalid names created libraries")
	}
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]any{"name": "  家庭珍藏  ", "path": path, "kind": "movie", "metadataSources": []string{}}, 201)
	var first app.Directory
	if err := json.Unmarshal(body, &first); err != nil || first.Name != "家庭珍藏" {
		t.Fatalf("name not normalized: %s", body)
	}
	awaitScan(t, a, first.ID, "completed")
	other := filepath.Join(root, "second", "movies")
	writeMedia(t, filepath.Join(other, "same.webm"), "another isolated movie")
	second := addDirectory(t, a, other)
	awaitScan(t, a, second.ID, "completed")
	shows := filepath.Join(root, "series")
	writeMedia(t, filepath.Join(shows, "Example.S01E01.webm"), "episode")
	writeMedia(t, filepath.Join(shows, "unorganized.webm"), "unorganized")
	seriesDir := addSeriesDirectory(t, a, shows)
	awaitScan(t, a, seriesDir.ID, "completed")
	empty := filepath.Join(root, "empty")
	if err := os.Mkdir(empty, 0755); err != nil {
		t.Fatal(err)
	}
	emptyDir := addDirectory(t, a, empty)
	awaitScan(t, a, emptyDir.ID, "completed")
	all := readLibrary(t, a, "")
	if len(all.Directories) != 4 || len(all.Items) != 4 || len(all.Series) != 1 {
		t.Fatalf("unexpected resource inventory: %+v", all)
	}
	one := readLibrary(t, a, "?directoryId="+first.ID+"&kind=movie&q=same&sort=recent")
	if len(one.Items) != 1 || one.Items[0].DirectoryID != first.ID || one.Items[0].DirectoryName != first.Name || len(one.Series) != 0 {
		t.Fatalf("library leaked files: %+v", one)
	}
	if len(readLibrary(t, a, "?directoryId="+emptyDir.ID).Items) != 0 {
		t.Fatal("empty library returned other libraries' files")
	}
	seriesOnly := readLibrary(t, a, "?directoryId="+seriesDir.ID)
	if len(seriesOnly.Items) != 2 || len(seriesOnly.Series) != 1 || seriesOnly.Series[0].DirectoryID != seriesDir.ID || seriesOnly.Series[0].DirectoryName != "测试媒体库" {
		t.Fatalf("series library not isolated: %+v", seriesOnly)
	}
	request(t, a.client, "GET", a.server.URL+"/api/library?directoryId=missing", nil, 404)
	request(t, http.DefaultClient, "GET", a.server.URL+"/api/library?directoryId="+first.ID, nil, 401)
	nameURL := a.server.URL + "/api/directories/" + first.ID + "/name"
	request(t, http.DefaultClient, "POST", nameURL, map[string]string{"name": "新名称"}, 401)
	for _, name := range []string{"", " \t", "bad\nname", strings.Repeat("a", 81)} {
		request(t, a.client, "POST", nameURL, map[string]string{"name": name}, 400)
	}
	request(t, a.client, "POST", a.server.URL+"/api/directories/missing/name", map[string]string{"name": "新名称"}, 404)
	file := one.Items[0]
	saveProgress(t, a, file.ID, 9, false)
	request(t, a.client, "POST", a.server.URL+"/api/favorites/media/"+file.ID, map[string]bool{"favorite": true}, 200)
	before := readLibrary(t, a, "?directoryId="+first.ID)
	newName := strings.Repeat("名", 80)
	request(t, a.client, "POST", nameURL, map[string]string{"name": " " + newName + " "}, 200)
	// Renaming does not trigger scanning or change any stored media identity/state.
	after := readLibrary(t, a, "?directoryId="+first.ID)
	for _, d := range after.Directories {
		if d.ID == first.ID && (d.Name != newName || d.Path != path || len(d.MetadataSources) != 0) {
			t.Fatalf("rename changed directory settings: %+v", d)
		}
	}
	before.Items[0].DirectoryName = newName
	if !reflect.DeepEqual(before.Items, after.Items) {
		t.Fatal("rename changed media state")
	}
	a.close()
	a = start(t, config)
	login(t, a)
	if !reflect.DeepEqual(after, readLibrary(t, a, "?directoryId="+first.ID)) {
		t.Fatal("named library/state did not persist")
	}
}

func TestDirectoryNameMigrationWorksOfflineAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "legacy", "电影珍藏")
	writeMedia(t, filepath.Join(path, "legacy.webm"), "isolated legacy media")
	config := app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}}
	a := start(t, config)
	login(t, a)
	dir := addDirectory(t, a, path)
	awaitScan(t, a, dir.ID, "completed")
	file := readLibrary(t, a, "").Items[0]
	saveProgress(t, a, file.ID, 8, false)
	request(t, a.client, "POST", a.server.URL+"/api/favorites/media/"+file.ID, map[string]bool{"favorite": true}, 200)
	before := readLibrary(t, a, "").Items
	a.close()
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("ALTER TABLE media_directories DROP COLUMN name")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, path+"-offline"); err != nil {
		t.Fatal(err)
	}
	a = start(t, config)
	login(t, a)
	after := readLibrary(t, a, "")
	if len(after.Directories) != 1 || after.Directories[0].Name != "电影珍藏" || after.Directories[0].ID != dir.ID {
		t.Fatalf("legacy name/identity migration failed: %+v", after.Directories)
	}
	before[0].DirectoryName = "电影珍藏"
	if !reflect.DeepEqual(before, after.Items) {
		t.Fatal("migration lost media state")
	}
	request(t, a.client, "POST", a.server.URL+"/api/directories/"+dir.ID+"/name", map[string]string{"name": "经典"}, 200)
	a.close()
	a = start(t, config)
	login(t, a)
	if readLibrary(t, a, "").Directories[0].Name != "经典" {
		t.Fatal("restart overwrote custom name")
	}
}
