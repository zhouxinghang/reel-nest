// Isolated harness for the production runtime/assets. Only external provider
// origins are replaced; native video is a test-only consumer of the same API.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"syscall"
	"time"

	"reelnest/internal/app"
	"reelnest/internal/web"
)

func main() {
	runtime, e := app.NewRuntime(context.Background(), app.RuntimeConfig{DataDir: os.Getenv("DATA_DIR"), MediaRoots: []string{os.Getenv("MEDIA_ROOTS")}, Username: "family", Password: "browser-test-password", Assets: web.Assets(), ProviderTransport: fixtureTransport{}})
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

// Fail closed: browser fixtures must never call real providers or arbitrary hosts.
type fixtureTransport struct{}

func (fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	next := r.Clone(r.Context())
	u := *r.URL
	next.URL = &u
	switch r.URL.Host {
	case "api.themoviedb.org":
		next.URL.Host = "127.0.0.1:18092"
		next.URL.Path = strings.TrimPrefix(r.URL.Path, "/3")
	case "image.tmdb.org":
		next.URL.Host = "127.0.0.1:18092"
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/t/p/"), "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid fixture image")
		}
		next.URL.Path = "/images/" + parts[1]
	case "api4.thetvdb.com":
		next.URL.Host = "127.0.0.1:18094"
		next.URL.Path = strings.TrimPrefix(r.URL.Path, "/v4")
	case "artworks.thetvdb.com":
		next.URL.Host = "127.0.0.1:18094"
		next.URL.Path = "/artwork/" + strings.TrimPrefix(r.URL.Path, "/banners/")
	case "127.0.0.1:18095":
	default:
		return nil, fmt.Errorf("outbound origin is not a browser fixture")
	}
	next.URL.Scheme = "http"
	if next.URL.Path != r.URL.Path {
		next.URL.RawPath = ""
	}
	return http.DefaultTransport.RoundTrip(next)
}
