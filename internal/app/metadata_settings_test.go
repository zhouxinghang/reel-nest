package app_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reelnest/internal/app"
)

type settingsView struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	CredentialSet bool   `json:"credentialSet"`
	PINSet        bool   `json:"pinSet"`
	Configured    bool   `json:"configured"`
	Revision      int64  `json:"revision"`
}

func readSettings(t *testing.T, a *runningApp) map[string]settingsView {
	t.Helper()
	body, response := request(t, a.client, "GET", a.server.URL+"/api/settings/metadata", nil, 200)
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("settings may be cached")
	}
	for _, secret := range []string{fakeTMDBToken, fakeTVDBKey, fakeTVDBPIN, fakeMetaTubeToken} {
		if bytes.Contains(body, []byte(secret)) {
			t.Fatal("settings returned a secret")
		}
	}
	var data struct {
		Sources []settingsView `json:"sources"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	if len(data.Sources) != 3 {
		t.Fatal("missing providers")
	}
	result := map[string]settingsView{}
	for _, s := range data.Sources {
		result[s.ID] = s
	}
	return result
}

func saveSettings(t *testing.T, a *runningApp, source string, update map[string]any) settingsView {
	t.Helper()
	awaitMetadata(t, a)
	update["revision"] = readSettings(t, a)[source].Revision
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/settings/metadata/"+source, update, 200)
	var result settingsView
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	awaitMetadata(t, a)
	return result
}

func TestMetadataSettingsLiveProvidersAndValidation(t *testing.T) {
	root := t.TempDir()
	tmdb, tvdb, metatube := fakeTMDB(t), fakeTVDB(t), fakeMetaTube(t)
	config := tmdb.config(t, root)
	tvdb.configure(&config)
	config.TMDBToken, config.TVDBAPIKey, config.TVDBPIN = "", "", ""
	a := start(t, config)
	request(t, http.DefaultClient, "GET", a.server.URL+"/api/settings/metadata", nil, 401)
	request(t, http.DefaultClient, "POST", a.server.URL+"/api/settings/metadata/tmdb", map[string]any{"revision": 1, "credential": "no"}, 401)
	login(t, a)
	for _, s := range readSettings(t, a) {
		if s.Configured || s.CredentialSet || s.PINSet {
			t.Fatal("unexpected initial credentials")
		}
	}
	saveSettings(t, a, "tmdb", map[string]any{"credential": fakeTMDBToken})
	saveSettings(t, a, "tvdb", map[string]any{"credential": fakeTVDBKey, "pin": fakeTVDBPIN})
	saveSettings(t, a, "metatube", map[string]any{"url": metatube.server.URL, "credential": fakeMetaTubeToken})
	for _, s := range readSettings(t, a) {
		if !s.Configured || !s.CredentialSet {
			t.Fatal("settings not active")
		}
	}
	writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "local video")
	dir := addSourceDirectory(t, a, root, "movie", []string{"tmdb", "tvdb"})
	file := sourceFile(t, a, dir.ID)
	if file.Match.Metadata == nil {
		t.Fatal("new credentials not used for matching")
	}
	request(t, a.client, "POST", a.server.URL+"/api/matching/movie/"+file.ID+"/selection", map[string]any{"kind": "movie", "source": "tvdb", "sourceId": 30}, 200)
	request(t, a.client, "POST", a.server.URL+"/api/media/"+file.ID+"/progress", map[string]any{"position": 8, "duration": 60, "completed": false}, 200)
	request(t, a.client, "POST", a.server.URL+"/api/favorites/media/"+file.ID, map[string]any{"favorite": true}, 200)
	saveSettings(t, a, "tmdb", map[string]any{"clearCredential": true})
	after := metadataMedia(t, a, file.ID)
	if after.Match.Selection != "manual" || after.Match.Metadata.Source != "TVDB" || after.Progress.Position != 8 || !after.Favorite {
		t.Fatal("settings changed library state")
	}
	// New TVDB clients must authenticate with the saved key/PIN immediately.
	saveSettings(t, a, "tvdb", map[string]any{"clearCredential": true})
	request(t, a.client, "GET", a.server.URL+"/api/matching/movie/"+file.ID+"/search?q=Arrival", nil, 503)
	saveSettings(t, a, "tvdb", map[string]any{"credential": fakeTVDBKey})
	request(t, a.client, "GET", a.server.URL+"/api/matching/movie/"+file.ID+"/search?q=Arrival", nil, 200)
	previous := readSettings(t, a)
	for _, update := range []map[string]any{
		{}, {"revision": nil}, {"revision": -1}, {"revision": previous["tmdb"].Revision, "credential": "x", "clearCredential": true},
		{"revision": previous["tmdb"].Revision, "credential": "a\nb"}, {"revision": previous["tmdb"].Revision, "credential": strings.Repeat("x", 4097)},
		{"revision": previous["tmdb"].Revision, "url": "https://example.com"}, {"revision": previous["tmdb"].Revision, "pin": "pin"},
		{"revision": previous["tmdb"].Revision, "unknown": true},
	} {
		request(t, a.client, "POST", a.server.URL+"/api/settings/metadata/tmdb", update, 400)
	}
	for _, url := range []string{"file:///tmp/secret", "ftp://example.com", "https://user:password@example.com", "http://example.com/?token=secret", "http://example.com/#fragment"} {
		request(t, a.client, "POST", a.server.URL+"/api/settings/metadata/metatube", map[string]any{"revision": previous["metatube"].Revision, "url": url, "clearCredential": true}, 400)
	}
	request(t, a.client, "POST", a.server.URL+"/api/settings/metadata/metatube", map[string]any{"revision": previous["metatube"].Revision, "url": "https://other.example"}, 400)
	request(t, a.client, "POST", a.server.URL+"/api/settings/metadata/unknown", map[string]any{}, 404)
	if current := readSettings(t, a); current["tmdb"] != previous["tmdb"] || current["metatube"] != previous["metatube"] {
		t.Fatal("invalid settings changed state")
	}
	payload, _ := json.Marshal(map[string]any{"revision": previous["tmdb"].Revision, "credential": "cross-site"})
	req, _ := http.NewRequest("POST", a.server.URL+"/api/settings/metadata/tmdb", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	response, err := a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-site settings allowed")
	}
	// The API does not infer clear from blank input; explicit clears survive restart.
	unchanged := saveSettings(t, a, "tvdb", map[string]any{"credential": "", "pin": ""})
	if unchanged != previous["tvdb"] {
		t.Fatal("blank fields replaced saved credentials")
	}
	saveSettings(t, a, "tvdb", map[string]any{"clearPin": true})
	if readSettings(t, a)["tvdb"].PINSet {
		t.Fatal("PIN not cleared")
	}
}

func TestMetadataSettingsMigrationEncryptionAndRestart(t *testing.T) {
	root := t.TempDir()
	fixture := fakeTMDB(t)
	config := fixture.config(t, root)
	calls := 0
	config.ImportLegacyMetadata = func() app.LegacyMetadataCredentials {
		calls++
		return app.LegacyMetadataCredentials{TMDBToken: fakeTMDBToken, TVDBAPIKey: fakeTVDBKey, TVDBPIN: fakeTVDBPIN, MetaTubeToken: fakeMetaTubeToken}
	}
	a := start(t, config)
	login(t, a)
	original := readSettings(t, a)
	saveSettings(t, a, "tmdb", map[string]any{"clearCredential": true})
	saveSettings(t, a, "tvdb", map[string]any{"clearPin": true})
	a.close()
	config.MetaTubeURL = "invalid ignored legacy endpoint"
	a = start(t, config)
	login(t, a)
	current := readSettings(t, a)
	if calls != 1 || current["tmdb"].CredentialSet || current["tvdb"].PINSet || !current["tvdb"].CredentialSet || current["metatube"] != original["metatube"] {
		t.Fatal("legacy configuration overrode saved settings")
	}
	a.close()
	for _, name := range []string{"reelnest.sqlite", "reelnest.sqlite-wal", "metadata-settings.key"} {
		path := filepath.Join(config.DataDir, name)
		contents, err := os.ReadFile(path)
		if os.IsNotExist(err) && strings.HasSuffix(name, "-wal") {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{fakeTMDBToken, fakeTVDBKey, fakeTVDBPIN, fakeMetaTubeToken} {
			if bytes.Contains(contents, []byte(secret)) {
				t.Fatal("plaintext credential persisted")
			}
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatal("sensitive storage is not private")
		}
	}
	keyPath := filepath.Join(config.DataDir, "metadata-settings.key")
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if application, err := app.New(config); err == nil {
		application.Close()
		t.Fatal("lost encryption key silently replaced")
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatal("new key created over saved settings")
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	a = start(t, config)
	login(t, a)
	if readSettings(t, a)["tvdb"] != current["tvdb"] {
		t.Fatal("restore lost settings")
	}
	a.close()
	// A database-only edit cannot redirect the encrypted Token to a new origin.
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("UPDATE metadata_provider_settings SET url='https://changed.example' WHERE source='metatube'"); err != nil {
		t.Fatal(err)
	}
	if application, err := app.New(config); err == nil {
		application.Close()
		t.Fatal("tampered endpoint accepted with the old credential")
	}
}

func TestMetadataSettingsConflictsAndPersistenceFailure(t *testing.T) {
	config := app.Config{DataDir: t.TempDir(), Username: "family", Password: password}
	a := start(t, config)
	login(t, a)
	previous := readSettings(t, a)["tmdb"]
	saveSettings(t, a, "tmdb", map[string]any{"credential": fakeTMDBToken})
	request(t, a.client, "POST", a.server.URL+"/api/settings/metadata/tmdb", map[string]any{"revision": previous.Revision, "clearCredential": true}, 409)
	db, err := sql.Open("sqlite", filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER fail_settings BEFORE UPDATE ON metadata_provider_settings BEGIN SELECT RAISE(ABORT,'isolated failure'); END`); err != nil {
		t.Fatal(err)
	}
	current := readSettings(t, a)["tmdb"]
	request(t, a.client, "POST", a.server.URL+"/api/settings/metadata/tmdb", map[string]any{"revision": current.Revision, "clearCredential": true}, 500)
	if readSettings(t, a)["tmdb"] != current {
		t.Fatal("failed save changed persistent settings")
	}
	status, _ := request(t, a.client, "GET", a.server.URL+"/api/metadata/status", nil, 200)
	if !bytes.Contains(status, []byte(`"configured":true`)) {
		t.Fatal("failed save changed live client")
	}
}

func TestMetadataSettingsConcurrentReadersAndUpdates(t *testing.T) {
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password})
	login(t, a)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 15; j++ {
				readSettings(t, a)
				request(t, a.client, "GET", a.server.URL+"/api/metadata/status", nil, 200)
				request(t, a.client, "GET", a.server.URL+"/api/directories", nil, 200)
			}
		}()
	}
	for i := 0; i < 10; i++ {
		saveSettings(t, a, "tmdb", map[string]any{"credential": fakeTMDBToken})
		saveSettings(t, a, "tmdb", map[string]any{"clearCredential": true})
	}
	workers.Wait()
	if readSettings(t, a)["tmdb"].Configured {
		t.Fatal("last update not active")
	}
}

func TestMetadataSettingsDoNotRaceAssociationWrites(t *testing.T) {
	root := t.TempDir()
	fixture := fakeTMDB(t)
	var hold atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	original := fixture.server.Config.Handler
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hold.Load() && r.URL.Path == "/movie/30" {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		original.ServeHTTP(w, r)
	}))
	t.Cleanup(upstream.Close)
	config := fixture.config(t, root)
	config.TMDBAPIURL = upstream.URL
	writeMedia(t, filepath.Join(root, "Arrival (2016).webm"), "local video")
	a := start(t, config)
	login(t, a)
	dir := addSourceDirectory(t, a, root, "movie", []string{"tmdb"})
	file := sourceFile(t, a, dir.ID)
	before := readSettings(t, a)["tmdb"]
	hold.Store(true)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	done := make(chan struct{})
	go func() {
		defer close(done)
		request(t, a.client, "POST", a.server.URL+"/api/matching/movie/"+file.ID+"/selection", map[string]any{"kind": "movie", "source": "tmdb", "sourceId": 30}, 200)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("manual matching did not start")
	}
	request(t, a.client, "POST", a.server.URL+"/api/settings/metadata/tmdb", map[string]any{"revision": before.Revision, "clearCredential": true}, 409)
	if readSettings(t, a)["tmdb"] != before {
		t.Fatal("busy save changed settings")
	}
	unblock()
	<-done
	saveSettings(t, a, "tmdb", map[string]any{"clearCredential": true})
	after := metadataMedia(t, a, file.ID)
	if after.Match.Selection != "manual" || after.Match.Metadata.SourceID != 30 {
		t.Fatal("credential change lost manual selection")
	}
}
