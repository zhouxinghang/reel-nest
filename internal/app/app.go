// Package app owns the household application, its persistent state and HTTP boundary.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Config struct {
	TMDBToken    string
	TMDBAPIURL   string // Override only for controlled external-service tests.
	TMDBImageURL string
	TMDBTimeout  time.Duration

	DataDir      string
	Username     string
	Password     string
	CookieSecure bool
	Assets       fs.FS
	MediaRoots   []string
}

type App struct {
	metadata *metadataService

	streamKey    []byte
	db           *sql.DB
	config       Config
	ctx          context.Context
	cancel       context.CancelFunc
	workers      sync.WaitGroup
	loginMu      sync.Mutex
	directoryMu  sync.Mutex
	failedLogins int
	loginWindow  int64

	// Serialize complete scan snapshots with destructive filesystem mutations.
	mediaMutationMu sync.Mutex
}

func New(config Config) (*App, error) {
	config.MediaRoots = append([]string{}, config.MediaRoots...)
	if config.DataDir == "" {
		return nil, errors.New("必须设置应用数据目录")
	}
	if err := os.MkdirAll(config.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("创建应用数据目录: %w", err)
	}
	path, err := filepath.Abs(filepath.Join(config.DataDir, "reelnest.sqlite"))
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开应用数据: %w", err)
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{db: db, config: config, ctx: ctx, cancel: cancel}
	if err := a.initialize(); err != nil {
		a.Close()
		return nil, err
	}
	// Bootstrap secrets are never retained in the running application configuration.
	a.config.Password = ""
	if err := a.initializeLibrary(); err != nil {
		a.Close()
		return nil, err
	}
	if err := a.initializeOrganization(); err != nil {
		a.Close()
		return nil, err
	}
	if err := a.initializeDiscovery(); err != nil {
		a.Close()
		return nil, err
	}
	if err := a.initializePlayback(); err != nil {
		a.Close()
		return nil, err
	}
	if err := a.initializeStreams(); err != nil {
		a.Close()
		return nil, err
	}
	if err := a.initializeMetadata(); err != nil {
		a.Close()
		return nil, err
	}
	return a, nil
}

func (a *App) initialize() error {
	_, err := a.db.Exec(`
 CREATE TABLE IF NOT EXISTS household (
  id INTEGER PRIMARY KEY CHECK (id=1), username TEXT NOT NULL, password_hash BLOB NOT NULL
 );
 CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY, expires_at INTEGER NOT NULL
 );`)
	if err != nil {
		return err
	}
	var count int
	if err := a.db.QueryRow("SELECT COUNT(*) FROM household").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if len(a.config.Username) < 1 || len(a.config.Username) > 80 || len(a.config.Password) < 12 || len(a.config.Password) > 72 {
			return errors.New("首次启动须设置家庭共享账号（1–80 字节）和密码（12–72 字节）；可通过 HOUSEHOLD_PASSWORD_FILE 提供密码")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(a.config.Password), bcrypt.DefaultCost)
		if err != nil {
			return errors.New("无法保存家庭共享账号")
		}
		if _, err = a.db.Exec("INSERT INTO household(id, username, password_hash) VALUES (1, ?, ?)", a.config.Username, hash); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) Close() error {
	a.cancel()
	a.workers.Wait()
	return a.db.Close()
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /api/login", a.login)
	mux.Handle("POST /api/logout", a.auth(http.HandlerFunc(a.logout)))
	mux.Handle("GET /api/session", a.auth(http.HandlerFunc(a.session)))
	mux.Handle("GET /api/matching/{kind}/{id}/search", a.auth(http.HandlerFunc(a.matching)))
	mux.Handle("POST /api/matching/{kind}/{id}/selection", a.auth(http.HandlerFunc(a.matching)))
	mux.Handle("GET /api/metadata/status", a.auth(http.HandlerFunc(a.metadataStatus)))
	mux.Handle("POST /api/metadata/refresh", a.auth(http.HandlerFunc(a.refreshMetadata)))
	mux.Handle("GET /api/metadata/posters/{kind}/{id}", a.auth(http.HandlerFunc(a.metadataPoster)))
	mux.Handle("GET /api/library", a.auth(http.HandlerFunc(a.library)))
	mux.Handle("POST /api/favorites/{kind}/{id}", a.auth(http.HandlerFunc(a.favorite)))
	mux.Handle("GET /api/media/{id}", a.auth(http.HandlerFunc(a.media)))
	mux.Handle("POST /api/media/{id}/deletion-preview", a.auth(http.HandlerFunc(a.deletionPreview)))
	mux.Handle("POST /api/media/{id}/delete", a.auth(http.HandlerFunc(a.deleteMedia)))
	mux.HandleFunc("GET /stream/{id}", a.stream)
	mux.Handle("POST /api/media/{id}/stream-link", a.auth(http.HandlerFunc(a.streamLink)))
	mux.Handle("GET /api/media/{id}/subtitles", a.auth(http.HandlerFunc(a.subtitles)))
	mux.Handle("GET /api/media/{id}/subtitles/{subtitle}", a.auth(http.HandlerFunc(a.subtitles)))
	mux.Handle("GET /api/media/{id}/content", a.auth(http.HandlerFunc(a.content)))
	mux.Handle("GET /api/media/{id}/playback", a.auth(http.HandlerFunc(a.playbackStatus)))
	mux.Handle("GET /api/media/{id}/progress", a.auth(http.HandlerFunc(a.progress)))
	mux.Handle("POST /api/media/{id}/progress", a.auth(http.HandlerFunc(a.progress)))
	mux.Handle("GET /api/series/{id}", a.auth(http.HandlerFunc(a.series)))
	mux.Handle("GET /api/directories", a.auth(http.HandlerFunc(a.directories)))
	mux.Handle("POST /api/directories", a.auth(http.HandlerFunc(a.addDirectory)))
	mux.Handle("POST /api/directories/{id}/scan", a.auth(http.HandlerFunc(a.rescan)))
	mux.Handle("/api/", a.auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "接口不存在") })))
	mux.HandleFunc("/", a.page)
	return a.secure(mux)
}
