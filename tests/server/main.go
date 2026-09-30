// This harness replaces only the external TMDB origins. The production app,
// embedded UI, authentication, SQLite, scanning and playback are unchanged.
package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"reelnest/internal/app"
	"reelnest/internal/web"
)

func main() {
	application, err := app.New(app.Config{DataDir: os.Getenv("DATA_DIR"), MediaRoots: []string{os.Getenv("MEDIA_ROOTS")}, Username: "family", Password: "browser-test-password", Assets: web.Assets(), TMDBToken: "isolated-browser-token", TMDBAPIURL: "http://127.0.0.1:18092", TMDBImageURL: "http://127.0.0.1:18092/images"})
	if err != nil {
		log.Fatal(err)
	}
	defer application.Close()
	server := &http.Server{Addr: "127.0.0.1:18091", Handler: application.Handler()}
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	_ = server.Close()
}
