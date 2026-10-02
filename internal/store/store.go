// Package store owns the new SQLite schema and transactional persistence.
// Callers receive use-case methods, never raw database handles or SQL transactions.
// This foundation is not wired into the legacy application until its consumers migrate.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

const (
	SchemaVersion   = 1
	applicationID   = 1380864852
	databaseName    = "reelnest.sqlite"
	streamKeyName   = "stream.key"
	settingsKeyName = "metadata-settings.key"
)

var (
	ErrUnsupportedSchema = errors.New("unsupported data directory or schema; use a new empty DATA_DIR")
	ErrInvalidKey        = errors.New("missing, invalid or replaced key; restore the complete data directory backup")
	ErrDataDirLocked     = errors.New("DATA_DIR is already in use")
)

//go:embed schema.sql
var schemaSQL string

// Store owns one writer, up to four query-only readers, and the DATA_DIR lock.
// Close is idempotent; workers must stop before their owner calls it.
type Store struct {
	writer    *sql.DB
	reader    *sql.DB
	directory *os.File
	keys      Keys
	closeOnce sync.Once
	closeErr  error
}

// Keys is a copy of installation-specific keys, for identity-free streaming and
// provider-setting encryption respectively. Never serialize or log this value.
type Keys struct {
	Stream   [32]byte
	Settings [32]byte
}

func (s *Store) Keys() Keys { return s.keys }

// Open initializes only an empty directory. Existing installations are inspected
// read-only before any writable SQLite connection is opened. Missing keys, old
// schemas and interrupted initialization fail closed: nothing is repaired or erased.
// No household account, task recovery, media scanning or legacy import happens here.
func Open(ctx context.Context, dataDir string) (_ *Store, err error) {
	if dataDir == "" {
		return nil, ErrUnsupportedSchema
	}
	path, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open DATA_DIR: %w", err)
	}
	s := &Store{directory: os.NewFile(uintptr(fd), path)}
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()
	// Lock the directory itself: rejecting an old directory does not create even
	// a lock file in it. This advisory lock is released only after both pools close.
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDataDirLocked, err)
	}
	entries, err := s.directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	fresh := len(entries) == 0
	if fresh {
		if err = s.initialize(ctx, path); err != nil {
			return nil, err
		}
	} else {
		if err = validateFiles(path, entries); err != nil {
			return nil, err
		}
		if err = s.inspect(ctx, path); err != nil {
			return nil, err
		}
	}
	s.writer, err = connect(ctx, path, false)
	if err != nil {
		return nil, err
	}
	// This mutation happens only after all schema/key checks have succeeded.
	var mode string
	if err = s.writer.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return nil, err
	}
	if mode != "wal" {
		return nil, errors.New("SQLite WAL is required")
	}
	s.reader, err = connect(ctx, path, true)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func validateFiles(path string, entries []os.DirEntry) error {
	foundDB := false
	for _, entry := range entries {
		switch entry.Name() {
		case databaseName:
			foundDB = true
		case streamKeyName, settingsKeyName, databaseName + "-wal", databaseName + "-shm":
		default:
			return ErrUnsupportedSchema
		}
		info, err := os.Lstat(filepath.Join(path, entry.Name()))
		if err != nil || !info.Mode().IsRegular() {
			return ErrUnsupportedSchema
		}
		if (entry.Name() == streamKeyName || entry.Name() == settingsKeyName) && info.Size() != 32 {
			return ErrInvalidKey
		}
		// Do not silently chmod preexisting data. Invalid backups must be repaired
		// explicitly by the operator, before retrying startup.
		if info.Mode().Perm()&0077 != 0 {
			return errors.New("DATA_DIR files must not be group/world accessible")
		}
	}
	if !foundDB {
		return ErrUnsupportedSchema
	}
	return nil
}

func readKeys(path string) (Keys, error) {
	var keys Keys
	for name, dst := range map[string]*[32]byte{streamKeyName: &keys.Stream, settingsKeyName: &keys.Settings} {
		b, err := os.ReadFile(filepath.Join(path, name))
		if err != nil || len(b) != len(dst) {
			return Keys{}, ErrInvalidKey
		}
		copy(dst[:], b)
	}
	return keys, nil
}

func connect(ctx context.Context, path string, readOnly bool) (*sql.DB, error) {
	return connectMode(ctx, path, readOnly, false)
}

func connectMode(ctx context.Context, path string, readOnly, immutable bool) (*sql.DB, error) {
	q := url.Values{"_pragma": {"foreign_keys(1)", "busy_timeout(5000)"}, "mode": {"rw"}}
	if readOnly {
		q.Set("mode", "ro")
		q.Add("_pragma", "query_only(1)")
	}
	if immutable {
		q.Set("immutable", "1")
	}
	uri := url.URL{Scheme: "file", Path: filepath.Join(path, databaseName), RawQuery: q.Encode()}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	if readOnly {
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(4)
	} else {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Store) inspect(ctx context.Context, path string) error {
	// SQLite mode=ro can still create/write WAL and SHM. With no pending WAL,
	// immutable inspection is safe under our directory lock. For dirty shutdown,
	// inspect a private copy including WAL (never ignore uncheckpointed commits).
	inspectPath, cleanup, immutable, err := inspectionPath(path)
	if err != nil {
		return err
	}
	defer cleanup()
	db, err := connectMode(ctx, inspectPath, true, immutable)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsupportedSchema, err)
	}
	defer db.Close()
	var appID, version int
	if err = db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&appID); err != nil {
		return ErrUnsupportedSchema
	}
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return ErrUnsupportedSchema
	}
	if appID != applicationID || version != SchemaVersion {
		return ErrUnsupportedSchema
	}
	var ddl, stream, settings []byte
	if err = db.QueryRowContext(ctx, "SELECT version, ddl_hash, stream_key_hash, settings_key_hash FROM schema_info WHERE singleton=1").Scan(&version, &ddl, &stream, &settings); err != nil {
		return ErrUnsupportedSchema
	}
	if version != SchemaVersion || !digestEqual(ddl, sha256.Sum256([]byte(schemaSQL))) {
		return ErrUnsupportedSchema
	}
	if s.keys, err = readKeys(path); err != nil {
		return err
	}
	if !digestEqual(stream, sha256.Sum256(s.keys.Stream[:])) || !digestEqual(settings, sha256.Sum256(s.keys.Settings[:])) {
		return ErrInvalidKey
	}
	return nil
}

func digestEqual(got []byte, expected [32]byte) bool {
	return len(got) == len(expected) && string(got) == string(expected[:])
}

func (s *Store) initialize(ctx context.Context, path string) error {
	// Exclusive creation prevents accidental replacement. A crash/failure leaves
	// an explicitly unsupported incomplete installation, never a guessed recovery.
	if _, err := rand.Read(s.keys.Stream[:]); err != nil {
		return err
	}
	if _, err := rand.Read(s.keys.Settings[:]); err != nil {
		return err
	}
	for _, item := range []struct {
		name string
		data []byte
	}{
		{streamKeyName, s.keys.Stream[:]}, {settingsKeyName, s.keys.Settings[:]}, {databaseName, nil},
	} {
		f, err := os.OpenFile(filepath.Join(path, item.name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		n, writeErr := f.Write(item.data)
		if writeErr == nil && n != len(item.data) {
			writeErr = io.ErrShortWrite
		}
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := s.directory.Sync(); err != nil {
		return err
	}
	db, err := connect(ctx, path, false)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	ddl, stream, settings := sha256.Sum256([]byte(schemaSQL)), sha256.Sum256(s.keys.Stream[:]), sha256.Sum256(s.keys.Settings[:])
	if _, err = tx.ExecContext(ctx, "INSERT INTO schema_info VALUES (1, ?, ?, ?, ?)", SchemaVersion, ddl[:], stream[:], settings[:]); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error {
	s.closeOnce.Do(func() {
		if s.reader != nil {
			s.closeErr = errors.Join(s.closeErr, s.reader.Close())
		}
		if s.writer != nil {
			s.closeErr = errors.Join(s.closeErr, s.writer.Close())
		}
		if s.directory != nil {
			s.closeErr = errors.Join(s.closeErr, s.directory.Close())
		}
	})
	return s.closeErr
}

// Only a dirty restart copies the database. This deliberately trades crash-startup
// IO for a strict no-writes guarantee on unsupported data. No user media is read.
func inspectionPath(path string) (string, func(), bool, error) {
	noop := func() {}
	info, err := os.Stat(filepath.Join(path, databaseName+"-wal"))
	if errors.Is(err, os.ErrNotExist) || (err == nil && info.Size() == 0) {
		return path, noop, true, nil
	}
	if err != nil {
		return "", noop, false, err
	}
	temp, err := os.MkdirTemp("", "reelnest-schema-inspect-")
	if err != nil {
		return "", noop, false, err
	}
	cleanup := func() { _ = os.RemoveAll(temp) }
	for _, name := range []string{databaseName, databaseName + "-wal"} {
		if err := copyPrivateFile(filepath.Join(path, name), filepath.Join(temp, name)); err != nil {
			cleanup()
			return "", noop, false, err
		}
	}
	return temp, cleanup, false, nil
}

func copyPrivateFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	return errors.Join(err, dst.Close())
}
