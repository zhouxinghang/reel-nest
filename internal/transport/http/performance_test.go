package http_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"
)

var slowReport = flag.String("slow-provider-report", "", "opt-in 20-second upstream isolation benchmark report")

func TestSlowProviderHTTPIsolation(t *testing.T) {
	if *slowReport == "" {
		t.Skip("opt-in -args -slow-provider-report=/absolute/report.json")
	}
	entered := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-time.After(20 * time.Second):
			io.WriteString(w, `{"data":[]}`)
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	h := start(t)
	h.client.Timeout = 30 * time.Second
	h.login(t)
	writeFile(t, filepath.Join(h.root, "one.webm"), "only temporary bytes")
	_, files := fixtureFiles(t, h, h.root, "movie")
	file := files["one.webm"]
	h.request(t, "PATCH", "/api/settings/metadata/metatube", fmt.Sprintf(`{"revision":1,"url":%q}`, upstream.URL), 200)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	start := time.Now()
	go func() {
		request, _ := http.NewRequestWithContext(ctx, "GET", h.server.URL+"/api/metadata/subjects/"+file.SubjectID+"/candidates?source=metatube&q=one", nil)
		response, err := h.client.Do(request)
		if err == nil {
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream not entered")
	}
	samples := map[string][]float64{"home": {}, "progress": {}, "settings": {}}
	for i := 0; i < 30; i++ {
		for _, name := range []string{"home", "progress", "settings"} {
			before := time.Now()
			switch name {
			case "home":
				h.request(t, "GET", "/api/home", "", 200)
			case "progress":
				h.request(t, "PUT", mediaBase(file.ID)+"/progress", `{"position":12,"duration":60,"completed":false}`, 200)
			case "settings":
				h.request(t, "PATCH", "/api/settings/metadata/tvdb", fmt.Sprintf(`{"revision":%d,"credential":"synthetic-%d"}`, i+1, i), 200)
			}
			samples[name] = append(samples[name], float64(time.Since(before).Microseconds())/1000)
		}
		// Spread requests across a sustained slow network request, not just a latch.
		time.Sleep(500 * time.Millisecond)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if elapsed < 19*time.Second {
		t.Fatal("upstream did not remain slow", elapsed)
	}
	result := map[string]any{}
	for name, values := range samples {
		sort.Float64s(values)
		result[name] = map[string]any{"samplesMs": values, "p95Ms": values[28], "maxMs": values[29]}
		if values[28] > 200 {
			t.Errorf("%s p95 %.3f exceeds 200ms", name, values[28])
		}
	}
	report := map[string]any{"generatedAt": time.Now().UTC().Format(time.RFC3339), "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "upstreamElapsedMs": elapsed.Milliseconds(), "conditions": "30 real authenticated HTTP requests per endpoint over 15 seconds during one 20-second upstream request; one temporary file/library, real SQLite and provider HTTP; no frontend/mock transaction", "results": result}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(*slowReport, append(body, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("slow network duration=%s local results=%v", elapsed, result)
}
