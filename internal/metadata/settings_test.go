package metadata_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reelnest/internal/metadata"
	"reelnest/internal/model"
	"reelnest/internal/store"
)

func ptr(s string) *string { return &s }
func setup(t *testing.T) (*store.Store, *metadata.Service, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := metadata.New(context.Background(), s)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(); s.Close() })
	return s, m, dir
}
func snapshot(t *testing.T, m *metadata.Service, source string) metadata.ProviderSnapshot {
	t.Helper()
	p, err := m.Snapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func save(t *testing.T, m *metadata.Service, source string, patch metadata.SettingsPatch) metadata.ProviderSnapshot {
	t.Helper()
	p, err := m.Update(context.Background(), source, patch)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSettingsEncryptedPersistentAndSnapshotsAreImmutable(t *testing.T) {
	s, m, dir := setup(t)
	ctx := context.Background()
	initial, err := m.Settings()
	if err != nil || len(initial) != 3 {
		t.Fatal(initial, err)
	}
	for i, source := range []string{"tmdb", "tvdb", "metatube"} {
		if initial[i].Source() != source || initial[i].Revision() != 1 || initial[i].Configured() {
			t.Fatal(initial)
		}
	}
	tmdbSecret, key, pin := "isolated-tmdb-secret", "isolated-tvdb-key", "isolated-tvdb-pin"
	tmdb := save(t, m, "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("  " + tmdbSecret + "  ")})
	tvdb := save(t, m, "tvdb", metadata.SettingsPatch{Revision: 1, Credential: &key, PIN: &pin})
	meta := save(t, m, "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("http://127.0.0.1:1/proxy")})
	if tmdb.CatalogID() != "tmdb" || tvdb.CatalogID() != "tvdb" || meta.CatalogID() == "" || meta.Credential() != "" || !meta.Configured() {
		t.Fatal(tmdb, tvdb, meta)
	}
	for _, p := range initial {
		if p.Configured() || p.Revision() != 1 {
			t.Fatal("old snapshot mutated")
		}
	}
	initial[0] = meta
	if snapshot(t, m, "tmdb") != tmdb {
		t.Fatal("returned slice aliases registry")
	}
	for _, patch := range []metadata.SettingsPatch{
		{Revision: 2}, {Revision: 2, Credential: ptr(" \t ")}, {Revision: 2, Credential: ptr(tmdbSecret)},
	} {
		if got := save(t, m, "tmdb", patch); got != tmdb {
			t.Fatal("empty or identical save changed configuration", got)
		}
	}
	if _, err := m.Update(ctx, "tmdb", metadata.SettingsPatch{Revision: 1}); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale no-op save must conflict", err)
	}
	if _, err := m.Snapshot("unknown"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal(err)
	}
	rows, err := s.ProviderSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		for _, secret := range []string{tmdbSecret, key, pin} {
			if bytes.Contains(row.Secrets, []byte(secret)) {
				t.Fatal("plaintext in stored settings")
			}
		}
	}
	for _, p := range []metadata.ProviderSnapshot{tmdb, tvdb, meta} {
		b, _ := json.Marshal(p)
		printed := fmt.Sprintf("%s %v %+v %#v %q", b, p, p, p, p)
		for _, secret := range []string{tmdbSecret, key, pin} {
			if strings.Contains(printed, secret) {
				t.Fatal("snapshot serialization/logging exposes credentials")
			}
		}
	}
	assertNoPlaintext := func() {
		t.Helper()
		for _, name := range []string{"reelnest.sqlite", "reelnest.sqlite-wal"} {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Fatal(err)
			}
			for _, secret := range []string{tmdbSecret, key, pin} {
				if bytes.Contains(b, []byte(secret)) {
					t.Fatal("plaintext in database or WAL", name)
				}
			}
		}
	}
	assertNoPlaintext()
	m.Close()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoPlaintext()
	reopened, err := store.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	next, err := metadata.New(ctx, reopened)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	for _, want := range []metadata.ProviderSnapshot{tmdb, tvdb, meta} {
		if got := snapshot(t, next, want.Source()); got != want {
			t.Fatal("restart changed saved configuration", want.Source())
		}
	}
	cleared := save(t, next, "tvdb", metadata.SettingsPatch{Revision: 2, ClearCredential: true})
	if cleared.Configured() || cleared.Credential() != "" || cleared.PIN() != pin {
		t.Fatal(cleared)
	}
	cleared = save(t, next, "tvdb", metadata.SettingsPatch{Revision: 3, ClearPIN: true})
	if cleared.PIN() != "" || cleared.Configured() {
		t.Fatal(cleared)
	}
}

func TestMetaTubeAddressAndTokenChangesKeepCatalogIsolation(t *testing.T) {
	_, m, _ := setup(t)
	old := save(t, m, "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("HTTP://Example.COM:80/Proxy/"), Credential: ptr("old-token")})
	if old.BaseURL() != "http://example.com/Proxy/v1" {
		t.Fatal(old.BaseURL())
	}
	if got := save(t, m, "metatube", metadata.SettingsPatch{Revision: 2, URL: ptr("http://example.com/Proxy/v1/")}); got != old {
		t.Fatal("equivalent API URL changed catalog")
	}
	for _, credential := range []*string{nil, ptr(""), ptr(" \t")} {
		if _, err := m.Update(context.Background(), "metatube", metadata.SettingsPatch{Revision: 2, URL: ptr("http://second.invalid"), Credential: credential}); !errors.Is(err, model.ErrInvalid) {
			t.Fatal("old token silently transferred", err)
		}
	}
	rotated := save(t, m, "metatube", metadata.SettingsPatch{Revision: 2, Credential: ptr("rotated-token")})
	if rotated.CatalogID() != old.CatalogID() || rotated.Revision() != 3 || old.Credential() != "old-token" {
		t.Fatal("token rotation changed directory space or old snapshot")
	}
	changed := save(t, m, "metatube", metadata.SettingsPatch{Revision: 3, URL: ptr("http://second.invalid"), ClearCredential: true})
	if changed.CatalogID() == old.CatalogID() || changed.Credential() != "" || !changed.Configured() {
		t.Fatal(changed)
	}
	// Returning to an already known canonical address reuses that address's
	// identity; it does not import credentials from the historical configuration.
	back := save(t, m, "metatube", metadata.SettingsPatch{Revision: 4, URL: ptr("http://example.com/Proxy")})
	if back.CatalogID() != old.CatalogID() || back.Credential() != "" {
		t.Fatal(back)
	}
	disabled := save(t, m, "metatube", metadata.SettingsPatch{Revision: 5, URL: ptr("")})
	if disabled.Configured() || disabled.CatalogID() != "" || disabled.BaseURL() != "" {
		t.Fatal(disabled)
	}
}

func TestSettingsRejectInvalidPatchesWithoutMutation(t *testing.T) {
	_, m, _ := setup(t)
	before, _ := m.Settings()
	tests := []struct {
		source string
		patch  metadata.SettingsPatch
	}{
		{"tmdb", metadata.SettingsPatch{Revision: 0}},
		{"tmdb", metadata.SettingsPatch{Revision: 9007199254740991}},
		{"tmdb", metadata.SettingsPatch{Revision: 1, PIN: ptr("")}},
		{"tmdb", metadata.SettingsPatch{Revision: 1, ClearPIN: true}},
		{"tmdb", metadata.SettingsPatch{Revision: 1, URL: ptr("")}},
		{"tvdb", metadata.SettingsPatch{Revision: 1, PIN: ptr("pin"), ClearPIN: true}},
		{"tvdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("value"), ClearCredential: true}},
		{"tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("with space")}},
		{"tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("with\nnewline")}},
		{"tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("with\x00nul")}},
		{"tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr(strings.Repeat("a", 4097))}},
		{"tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr(strings.Repeat("界", 1366))}},
		{"tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr(string([]byte{0xff}))}},
		{"metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("https://user:secret@example.com")}},
	}
	for _, tt := range tests {
		if _, err := m.Update(context.Background(), tt.source, tt.patch); !errors.Is(err, model.ErrInvalid) {
			t.Fatal(tt.source, err)
		}
	}
	after, _ := m.Settings()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("invalid patch changed settings")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Update(ctx, "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("valid")}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestConcurrentSettingsCASAndClose(t *testing.T) {
	_, m, _ := setup(t)
	const writers = 24
	start := make(chan struct{})
	results := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := m.Update(context.Background(), "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr(fmt.Sprintf("value-%d", i))})
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, model.ErrConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 || snapshot(t, m, "tmdb").Revision() != 2 {
		t.Fatal("CAS failed", successes)
	}
	// Reads, saves and shutdown may race, but no save enters the DB after Close.
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := m.Snapshot("tmdb")
			if err == nil {
				_, err = m.Update(context.Background(), "tmdb", metadata.SettingsPatch{Revision: p.Revision(), Credential: ptr("next")})
			}
			if err != nil && !errors.Is(err, model.ErrClosed) && !errors.Is(err, model.ErrConflict) {
				t.Error(err)
			}
			_, _ = m.Settings()
		}()
	}
	m.Close()
	wg.Wait()
	m.Close()
	if _, err := m.Update(context.Background(), "tmdb", metadata.SettingsPatch{Revision: 2}); !errors.Is(err, model.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := m.Settings(); !errors.Is(err, model.ErrClosed) {
		t.Fatal(err)
	}
}

// This exercises a real slow HTTP consumer of the immutable configuration seam,
// not the yet-to-be-implemented title adapter/worker or production upstreams.
func TestSlowSnapshotConsumerDoesNotBlockSettingsOrLocalWrites(t *testing.T) {
	s, m, _ := setup(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	oldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/probe" || r.Header.Get("Authorization") != "Bearer old-token" {
			t.Error("old snapshot request changed")
		}
		close(entered)
		select {
		case <-release:
		case <-r.Context().Done():
		}
		w.WriteHeader(204)
	}))
	defer oldServer.Close()
	defer unblock()
	newServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/probe" || r.Header.Get("Authorization") != "Bearer new-token" {
			t.Error("new server received wrong configuration")
		}
		w.WriteHeader(204)
	}))
	defer newServer.Close()
	old := save(t, m, "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr(oldServer.URL), Credential: ptr("old-token")})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	request := func(p metadata.ProviderSnapshot) error {
		r, _ := http.NewRequestWithContext(ctx, "GET", p.BaseURL()+"/probe", nil)
		r.Header.Set("Authorization", "Bearer "+p.Credential())
		resp, err := http.DefaultClient.Do(r)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		return err
	}
	done := make(chan error, 1)
	go func() { done <- request(old) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("slow consumer never entered")
	}
	// Synthetic database rows only; no user file or provider credential is read.
	l, err := s.CreateLibrary(ctx, model.Library{ID: model.NewID(), Name: "local", Path: "/synthetic/slow-provider", Identity: "root", Kind: "movie"})
	if err != nil {
		t.Fatal(err)
	}
	scan, err := s.ClaimScan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StageScan(ctx, scan.ID, []model.ScanEntry{{Relative: "local.mp4", Fingerprint: "v1", Size: 1, ModifiedAt: 1}}); err != nil {
		t.Fatal(err)
	}
	if err = s.CommitScan(ctx, scan.ID); err != nil {
		t.Fatal(err)
	}
	files, err := s.MediaFiles(ctx, l.ID, "", 1)
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	times := []time.Duration{}
	for i := 0; i < 40; i++ {
		p := snapshot(t, m, "tmdb")
		start := time.Now()
		save(t, m, "tmdb", metadata.SettingsPatch{Revision: p.Revision(), Credential: ptr(fmt.Sprintf("value-%d", i))})
		times = append(times, time.Since(start))
		if _, err = s.SaveProgress(ctx, files[0].ID, model.Progress{Position: 1, Duration: 60}); err != nil {
			t.Fatal(err)
		}
	}
	next := save(t, m, "metatube", metadata.SettingsPatch{Revision: 2, URL: ptr(newServer.URL), Credential: ptr("new-token")})
	if next.CatalogID() == old.CatalogID() || request(next) != nil {
		t.Fatal("new settings not immediately usable")
	}
	select {
	case err := <-done:
		t.Fatal("old consumer completed before release; test did not hold it", err)
	default:
	}
	slices.Sort(times)
	t.Logf("40 settings saves while old consumer is blocked: p95=%s max=%s (small synthetic DB, not full worker benchmark)", times[37], times[39])
	unblock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Deliberate test-only corruption/failure injection; application code never
// receives raw SQL or repairs unsupported/corrupted data at startup.
func inspectDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	u := url.URL{Scheme: "file", Path: filepath.Join(dir, "reelnest.sqlite")}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}
func TestSettingsFailureRollsBackDatabaseAndPublication(t *testing.T) {
	s, m, dir := setup(t)
	db := inspectDB(t, dir)
	_, err := db.Exec(`CREATE TRIGGER fail_setting_revision BEFORE UPDATE ON revisions BEGIN SELECT RAISE(ABORT,'test failure'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := m.Settings()
	rowsBefore, _ := s.ProviderSettings(context.Background())
	_, err = m.Update(context.Background(), "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("http://isolated.invalid"), Credential: ptr("test-secret")})
	if err == nil {
		t.Fatal("injected transaction failure did not fail save")
	}
	after, _ := m.Settings()
	rowsAfter, _ := s.ProviderSettings(context.Background())
	if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(rowsBefore, rowsAfter) {
		t.Fatal("failed transaction leaked into DB or registry")
	}
	var catalogs int
	if err = db.QueryRow("SELECT count(*) FROM provider_catalogs WHERE source='metatube'").Scan(&catalogs); err != nil || catalogs != 0 {
		t.Fatal("catalog creation was not rolled back", catalogs, err)
	}
	if _, err = db.Exec("DROP TRIGGER fail_setting_revision"); err != nil {
		t.Fatal(err)
	}
	save(t, m, "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("http://isolated.invalid")})
}

func TestSettingsAuthenticationRejectsTamperedPersistence(t *testing.T) {
	for name, mutation := range map[string]string{
		"ciphertext": `UPDATE provider_settings SET secrets=zeroblob(60) WHERE source='tmdb'`,
		"source":     `UPDATE provider_settings SET secrets=(SELECT secrets FROM provider_settings WHERE source='tmdb') WHERE source='tvdb'`,
		"revision":   `UPDATE provider_settings SET revision=revision+1 WHERE source='tmdb'`,
		"stripped":   `UPDATE provider_settings SET secrets=NULL WHERE source='tmdb'`,
		"catalog":    `INSERT INTO provider_catalogs VALUES('other','metatube','http://other.invalid/v1'); UPDATE provider_settings SET catalog_id='other' WHERE source='metatube'`,
		"missing":    `DELETE FROM provider_settings WHERE source='tvdb'`,
	} {
		t.Run(name, func(t *testing.T) {
			s, m, dir := setup(t)
			save(t, m, "tmdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("test-token")})
			save(t, m, "tvdb", metadata.SettingsPatch{Revision: 1, Credential: ptr("test-key")})
			save(t, m, "metatube", metadata.SettingsPatch{Revision: 1, URL: ptr("http://first.invalid"), Credential: ptr("meta-token")})
			m.Close()
			db := inspectDB(t, dir)
			if _, err := db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if next, err := metadata.New(context.Background(), s); !errors.Is(err, metadata.ErrSettingsUnreadable) {
				if next != nil {
					next.Close()
				}
				t.Fatal("corrupted settings accepted", err)
			}
		})
	}
}
