package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func openTestStore(t testing.TB) *Store {
	t.Helper()
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func execSQL(t testing.TB, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.writer.Exec(query, args...); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
}
func count(t testing.TB, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.reader.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func seedRelationships(t testing.TB, s *Store) {
	t.Helper()
	execSQL(t, s, `INSERT INTO household VALUES(1,'family',x'1234');
INSERT INTO sessions VALUES ('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',1,2000);
INSERT INTO libraries VALUES('movies','Movies','/synthetic/movies','1:1','movie',1,1,1000),('tv','TV','/synthetic/tv','1:2','series',1,1,1000);
INSERT INTO library_sources VALUES('movies','tmdb',0);
INSERT INTO media_files VALUES('m','movies','movie.mkv','1:10:100:1000',100,1000,1000,'available',''),('e','tv','Show/01.mkv','1:11:100:1000',100,1000,1000,'available','');
INSERT INTO series VALUES('s','tv','show','Show');
INSERT INTO file_organization VALUES('e','tv','s',1,1);
INSERT INTO watch_progress VALUES('m',1,60,0,1000);
INSERT INTO media_favorites VALUES('m',1000);
INSERT INTO series_favorites VALUES('s',1000);
INSERT INTO metadata_subjects VALUES('sm','movies','movie','m',NULL,1),('ss','tv','series',NULL,'s',1);
INSERT INTO remote_titles VALUES('rm','tmdb','tmdb','movie','AbC-001'),('rs','tvdb','tvdb','series','42');
INSERT INTO metadata_bindings VALUES('sm','movie','rm','manual',1,1000,'succeeded',NULL),('ss','series','rs','automatic',1,1000,'succeeded',NULL);
INSERT INTO metadata_snapshots VALUES('rm','Movie','Movie','',2026,'Overview','https://example.invalid/movie',1000,2000,1,'ready','ready',NULL,NULL);
INSERT INTO people VALUES('p','tmdb','remote-person:123');
INSERT INTO title_credits VALUES('c','rm','tmdb','p','actor',0,'Actor','["Character"]');
INSERT INTO assets VALUES('a','rm','tmdb',NULL,'primary','/safe.jpg',1,1,'missing',NULL,NULL,NULL,NULL),('photo','rm','tmdb','c','photo','/person.jpg',1,1,'missing',NULL,NULL,NULL,NULL);
INSERT INTO metadata_jobs(subject_id,requested_generation,state,requested_at) VALUES('sm',1,'queued',1000);
INSERT INTO metadata_job_providers VALUES('sm','tmdb',1,'tmdb');
INSERT INTO catalog_entries VALUES('media:m','movies','m',NULL,'sm','movie','Movie','movie','movie',1000,0,0,1,1),('series:s','tv',NULL,'s','ss','series','Show','show','show',1000,1,1,1,1);
INSERT INTO scan_runs(id,library_id,state,requested_at) VALUES('scan','movies','queued',1000);
INSERT INTO scan_entries VALUES('scan','movies','new.mkv','1:20:100:1000',100,1000);`)
}

func TestFreshSchemaRestartKeepsStateAndKeys(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	seedRelationships(t, s)
	keys := s.Keys()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Keys() != keys {
		t.Fatal("keys rotated")
	}
	for _, table := range []string{"household", "sessions", "watch_progress", "media_favorites", "series_favorites", "metadata_jobs", "scan_runs", "scan_entries"} {
		if n := count(t, s, table); n != 1 {
			t.Fatalf("%s lost state: %d", table, n)
		}
	}
	if count(t, s, "metadata_bindings") != 2 {
		t.Fatal("selection lost")
	}
	var state string
	if err = s.reader.QueryRow("SELECT state FROM scan_runs").Scan(&state); err != nil || state != "queued" {
		t.Fatal("store must not silently recover or schedule scans", state, err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		info, _ := e.Info()
		if info.Mode().Perm()&0077 != 0 {
			t.Fatalf("exposed file %s: %v", e.Name(), info.Mode())
		}
	}
}

func directoryDigest(t *testing.T, dir string) map[string][32]byte {
	t.Helper()
	m := map[string][32]byte{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		m[e.Name()] = sha256.Sum256(b)
	}
	return m
}
func assertRejectedUnchanged(t *testing.T, dir string) {
	t.Helper()
	before := directoryDigest(t, dir)
	s, err := Open(context.Background(), dir)
	if err == nil {
		s.Close()
		t.Fatal("accepted unsupported/incomplete data")
	}
	after := directoryDigest(t, dir)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected directory was modified: before %v after %v", before, after)
	}
}
func TestRefusesOldUnsupportedOrIncompleteDataWithoutWrites(t *testing.T) {
	for _, name := range []string{"legacy", "unknown-version", "wrong-application", "empty-db", "random-file", "missing-settings-key", "missing-stream-key", "replaced-key", "truncated-key"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			switch name {
			case "legacy":
				db, err := sql.Open("sqlite", filepath.Join(dir, databaseName))
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.Exec("CREATE TABLE old_state(secret TEXT); INSERT INTO old_state VALUES('synthetic-only')"); err != nil {
					t.Fatal(err)
				}
				db.Close()
				os.Chmod(filepath.Join(dir, databaseName), 0600)
			case "empty-db":
				if err := os.WriteFile(filepath.Join(dir, databaseName), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "random-file":
				if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				s, err := Open(context.Background(), dir)
				if err != nil {
					t.Fatal(err)
				}
				if name == "unknown-version" {
					execSQL(t, s, "PRAGMA user_version=999")
				}
				if name == "wrong-application" {
					execSQL(t, s, "PRAGMA application_id=1")
				}
				s.Close()
				switch name {
				case "missing-settings-key":
					os.Remove(filepath.Join(dir, settingsKeyName))
				case "missing-stream-key":
					os.Remove(filepath.Join(dir, streamKeyName))
				case "replaced-key":
					os.WriteFile(filepath.Join(dir, settingsKeyName), make([]byte, 32), 0600)
				case "truncated-key":
					os.WriteFile(filepath.Join(dir, settingsKeyName), []byte{1}, 0600)
				}
			}
			assertRejectedUnchanged(t, dir)
			if name == "legacy" {
				_, err := Open(context.Background(), dir)
				if !errors.Is(err, ErrUnsupportedSchema) {
					t.Fatalf("legacy schema diagnostic: %v", err)
				}
			}
		})
	}
}
func TestRefusesSymlinkAndConcurrentOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if other, err := Open(context.Background(), dir); !errors.Is(err, ErrDataDirLocked) {
		if other != nil {
			other.Close()
		}
		t.Fatalf("expected lock error: %v", err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err = os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(context.Background(), link); err == nil {
		other.Close()
		t.Fatal("accepted symlink DATA_DIR")
	}
}
func TestAllConnectionsEnforceForeignKeysAndReadersCannotWrite(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	var readers []*sql.Conn
	for i := 0; i < 4; i++ {
		c, err := s.reader.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		readers = append(readers, c)
		defer c.Close()
	}
	w, err := s.writer.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, c := range append(readers, w) {
		var fk, timeout int
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
			t.Fatal("foreign keys disabled", err)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != 5000 {
			t.Fatal("timeout not configured", err)
		}
	}
	for _, c := range readers {
		if _, err = c.ExecContext(ctx, "UPDATE revisions SET catalog=2"); err == nil {
			t.Fatal("reader was writable")
		}
	}
	if _, err = w.ExecContext(ctx, "INSERT INTO media_favorites VALUES('missing',1000)"); err == nil {
		t.Fatal("writer ignored FK")
	}
}
func TestSchemaConstraints(t *testing.T) {
	s := openTestStore(t)
	seedRelationships(t, s)
	bad := []string{
		`INSERT INTO media_favorites VALUES('missing',1000)`,
		`INSERT INTO library_sources VALUES('movies','bad',1)`,
		`INSERT INTO library_sources VALUES('movies','tvdb',0)`,
		`INSERT INTO file_organization VALUES('m','movies','s',1,2)`,
		`INSERT INTO metadata_subjects VALUES('bad','movies','movie',NULL,NULL,1)`,
		`INSERT INTO metadata_subjects VALUES('bad','movies','movie','m','s',1)`,
		`INSERT INTO metadata_subjects VALUES('bad','tv','series','e',NULL,1)`,
		`UPDATE metadata_bindings SET remote_title_id='rs' WHERE subject_id='sm'`,
		`INSERT INTO remote_titles VALUES('bad','tmdb','tvdb','movie','12')`,
		`UPDATE remote_titles SET remote_id='changed' WHERE id='rm'`,
		`UPDATE provider_catalogs SET base_url='https://evil.invalid' WHERE id='tmdb'`,
		`UPDATE watch_progress SET position=-1`,
		`UPDATE watch_progress SET duration=0`,
		`UPDATE watch_progress SET position=61`,
		`UPDATE watch_progress SET completed=1`,
		`UPDATE media_files SET relative_path='../outside' WHERE id='m'`,
		`INSERT INTO scan_runs(id,library_id,state,requested_at) VALUES('second','movies','running',1000)`,
		`INSERT INTO scan_entries VALUES('scan','tv','wrong.mkv','fingerprint',10,1000)`,
		`UPDATE metadata_jobs SET state='running'`,
		`UPDATE metadata_jobs SET attempts=4`,
		`UPDATE metadata_jobs SET claimed_generation=2`,
		`UPDATE metadata_job_providers SET catalog_id='tvdb'`,
		`UPDATE provider_settings SET catalog_id=NULL WHERE source='tmdb'`,
		`UPDATE catalog_entries SET subject_id='ss' WHERE media_file_id='m'`,
		`UPDATE title_credits SET role='writer'`,
		`UPDATE title_credits SET characters='{}'`,
		`UPDATE title_credits SET catalog_id='tvdb'`,
		`UPDATE assets SET state='ready'`,
		`UPDATE assets SET bytes=zeroblob(5242881)`,
		`UPDATE assets SET credit_id='c' WHERE id='a'`,
	}
	for _, q := range bad {
		t.Run(q, func(t *testing.T) {
			if _, err := s.writer.Exec(q); err == nil {
				t.Fatal("accepted invalid relationship/value")
			}
		})
	}
	for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		if _, err := s.writer.Exec("UPDATE watch_progress SET position=?", v); err == nil {
			t.Fatalf("accepted non-finite value %v", v)
		}
	}
	// Metadata-only expiration must not cascade into external identity or choice.
	execSQL(t, s, "DELETE FROM metadata_snapshots WHERE expires_at<=2000")
	for _, table := range []string{"title_credits", "assets"} {
		if count(t, s, table) != 0 {
			t.Fatal("orphaned", table)
		}
	}
	if count(t, s, "metadata_bindings") != 2 || count(t, s, "remote_titles") != 2 {
		t.Fatal("expiration removed selection/identity")
	}
	var mode string
	if err := s.reader.QueryRow("SELECT selection FROM metadata_bindings WHERE subject_id='sm'").Scan(&mode); err != nil || mode != "manual" {
		t.Fatal(mode, err)
	}
}
func TestTargetDeletionCascadesWithoutRevivingManualChoice(t *testing.T) {
	s := openTestStore(t)
	seedRelationships(t, s)
	execSQL(t, s, "DELETE FROM media_files WHERE id='m'")
	for _, table := range []string{"watch_progress", "media_favorites", "metadata_jobs", "metadata_job_providers"} {
		if count(t, s, table) != 0 {
			t.Fatal("orphaned", table)
		}
	}
	if _, err := s.writer.Exec("INSERT INTO metadata_bindings VALUES('sm','movie','rm','manual',2,1000,'succeeded',NULL)"); err == nil {
		t.Fatal("old task revived removed subject")
	}
	if count(t, s, "metadata_bindings") != 1 || count(t, s, "remote_titles") != 2 {
		t.Fatal("cascade removed unrelated state")
	}
	execSQL(t, s, "DELETE FROM libraries WHERE id='tv'")
	for _, table := range []string{"series", "file_organization", "series_favorites", "metadata_subjects", "metadata_bindings", "catalog_entries"} {
		if count(t, s, table) != 0 {
			t.Fatal("orphaned", table)
		}
	}
	var violation any
	err := s.reader.QueryRow("PRAGMA foreign_key_check").Scan(&violation)
	if err != sql.ErrNoRows {
		t.Fatal("FK violation", err)
	}
}
func TestSchemaRollbackAndNamespaceIsolation(t *testing.T) {
	s := openTestStore(t)
	seedRelationships(t, s)
	tx, err := s.writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("DELETE FROM metadata_subjects WHERE id='sm'"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec("INSERT INTO metadata_bindings VALUES('missing','movie','rm','manual',1,NULL,'never',NULL)"); err == nil {
		t.Fatal("expected real FK failure")
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "metadata_bindings") != 2 || count(t, s, "metadata_jobs") != 1 {
		t.Fatal("transaction did not roll back cascades")
	}
	execSQL(t, s, `INSERT INTO provider_catalogs VALUES('mt1','metatube','http://one.invalid/v1'),('mt2','metatube','http://two.invalid/v1');
INSERT INTO remote_titles VALUES('mt-a','mt1','provider','movie','AbC-001'),('mt-b','mt2','provider','movie','AbC-001'),('mt-c','mt1','provider','movie','abc-001');`)
	if count(t, s, "remote_titles") != 5 {
		t.Fatal("remote IDs collided")
	}
	if _, err = s.writer.Exec("INSERT INTO remote_titles VALUES('mt-series','mt1','provider','series','x')"); err == nil {
		t.Fatal("MetaTube accepted series")
	}
}

// This child intentionally exits without Close to preserve uncheckpointed WAL.
func TestStoreCrashWriter(t *testing.T) {
	dir := os.Getenv("REELNEST_STORE_CRASH_DIR")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	s, err := Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, s, "PRAGMA wal_autocheckpoint=0")
	seedRelationships(t, s)
	if os.Getenv("REELNEST_STORE_CRASH_UNSUPPORTED") == "1" {
		execSQL(t, s, "PRAGMA user_version=999")
	}
	os.Exit(0)
}

func TestDirtyRestartReadsWALAndRejectsUnsupportedWALWithoutWrites(t *testing.T) {
	for _, unsupported := range []bool{false, true} {
		t.Run(fmt.Sprint(unsupported), func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestStoreCrashWriter$")
			cmd.Env = append(os.Environ(), "REELNEST_STORE_CRASH_DIR="+dir)
			if unsupported {
				cmd.Env = append(cmd.Env, "REELNEST_STORE_CRASH_UNSUPPORTED=1")
			}
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("crash fixture: %v %s", err, b)
			}
			info, err := os.Stat(filepath.Join(dir, databaseName+"-wal"))
			if err != nil || info.Size() == 0 {
				t.Fatal("fixture did not retain WAL", err)
			}
			if unsupported {
				assertRejectedUnchanged(t, dir)
				return
			}
			s, err := Open(context.Background(), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if count(t, s, "metadata_bindings") != 2 || count(t, s, "watch_progress") != 1 {
				t.Fatal("uncheckpointed commits lost")
			}
		})
	}
}
