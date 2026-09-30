package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"reelnest/internal/app"
	"reelnest/internal/web"
)

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func run() error {
	password := os.Getenv("HOUSEHOLD_PASSWORD")
	if path := os.Getenv("HOUSEHOLD_PASSWORD_FILE"); path != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			return errors.New("无法读取 HOUSEHOLD_PASSWORD_FILE")
		}
		password = strings.TrimRight(string(contents), "\r\n")
	}
	token := os.Getenv("TMDB_READ_ACCESS_TOKEN")
	if path := os.Getenv("TMDB_READ_ACCESS_TOKEN_FILE"); path != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			// Optional metadata configuration must not take down local media.
			token = ""
			log.Print("无法读取 TMDB_READ_ACCESS_TOKEN_FILE；已禁用资料获取，本地媒体仍可使用")
		} else {
			token = strings.TrimSpace(string(contents))
		}
	}
	application, err := app.New(app.Config{TMDBToken: token, DataDir: env("DATA_DIR", "./data"), Username: os.Getenv("HOUSEHOLD_USERNAME"), Password: password, CookieSecure: os.Getenv("COOKIE_SECURE") == "true", Assets: web.Assets(), MediaRoots: filepath.SplitList(os.Getenv("MEDIA_ROOTS"))})
	if err != nil {
		return err
	}
	defer application.Close()
	server := &http.Server{Addr: env("LISTEN_ADDR", ":8090"), Handler: application.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	finished := make(chan error, 1)
	go func() { log.Printf("Reel Nest listening on %s", server.Addr); finished <- server.ListenAndServe() }()
	select {
	case err := <-finished:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			return err
		}
		err := <-finished
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
