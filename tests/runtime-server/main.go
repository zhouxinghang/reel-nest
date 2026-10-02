// Isolated browser harness for the new runtime. The native <video> consumer is
// test-only: it verifies real codec/Range/subtitle HTTP behavior, not the pending
// React UI switch. No provider, database, auth or filesystem operation is mocked.
package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"reelnest/internal/app"
)

func main() {
	runtime, e := app.NewRuntime(context.Background(), app.RuntimeConfig{DataDir: os.Getenv("DATA_DIR"), MediaRoots: []string{os.Getenv("MEDIA_ROOTS")}, Username: "family", Password: "browser-test-password"})
	if e != nil {
		log.Fatal(e)
	}
	defer runtime.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__native-player", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<!doctype html><html lang="zh"><title>新 runtime 原生视频测试</title><video controls preload="metadata" width="640"></video></html>`)
	})
	mux.Handle("/", runtime.Handler())
	server := &http.Server{Addr: "127.0.0.1:18096", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		if e := server.ListenAndServe(); e != http.ErrServerClosed {
			log.Fatal(e)
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	<-signals
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if e := server.Shutdown(ctx); e != nil {
		server.Close()
	}
}
