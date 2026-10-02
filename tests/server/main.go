// This harness replaces only the external metadata origins. The production app,
// legacy UI, authentication, SQLite, scanning and playback are retained for regression.
package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"reelnest/internal/app"
)

func main() {
	tvdbKey := ""
	if os.Getenv("TEST_TVDB") == "true" {
		tvdbKey = "isolated-browser-tvdb-key"
	}
	metaTubeURL := ""
	if os.Getenv("TEST_METATUBE") == "true" {
		metaTubeURL = "http://127.0.0.1:18095"
	}
	application, err := app.New(app.Config{MetaTubeURL: metaTubeURL, MetaTubeToken: "isolated-browser-metatube-token", TVDBAPIKey: tvdbKey, TVDBAPIURL: "http://127.0.0.1:18094", TVDBImageURL: "http://127.0.0.1:18094/artwork", DataDir: os.Getenv("DATA_DIR"), MediaRoots: []string{os.Getenv("MEDIA_ROOTS")}, Username: "family", Password: "browser-test-password", Assets: os.DirFS(".reelnest-test/legacy-web"), TMDBToken: "isolated-browser-token", TMDBAPIURL: "http://127.0.0.1:18092", TMDBImageURL: "http://127.0.0.1:18092/images"})
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
