// Temporary, isolated historical regression harness. Never a production entry.
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
)

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// Legacy migration only. Saved page settings prevent this reader from running.
// Files take precedence; unreadable optional credentials are never logged.
func optionalSecret(name string) string {
	if path := os.Getenv(name + "_FILE"); path != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			log.Printf("无法读取 %s_FILE；对应凭据不可用，本地媒体仍可使用", name)
			return ""
		}
		return strings.TrimSpace(string(contents))
	}
	return strings.TrimSpace(os.Getenv(name))
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
	application, err := app.New(app.Config{ImportLegacyMetadata: func() app.LegacyMetadataCredentials {
		return app.LegacyMetadataCredentials{TMDBToken: optionalSecret("TMDB_READ_ACCESS_TOKEN"), TVDBAPIKey: optionalSecret("TVDB_API_KEY"), TVDBPIN: optionalSecret("TVDB_PIN"), MetaTubeURL: os.Getenv("METATUBE_URL"), MetaTubeToken: optionalSecret("METATUBE_TOKEN")}
	}, DataDir: env("DATA_DIR", "./data"), Username: os.Getenv("HOUSEHOLD_USERNAME"), Password: password, CookieSecure: os.Getenv("COOKIE_SECURE") == "true", Assets: os.DirFS(".reelnest-test/legacy-web"), MediaRoots: filepath.SplitList(os.Getenv("MEDIA_ROOTS"))})
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
