package store_test

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"reelnest/internal/catalog"
	"reelnest/internal/identity"
	"reelnest/internal/mediafs"
	"reelnest/internal/metadata"
	"reelnest/internal/store"
	transport "reelnest/internal/transport/http"
)

var httpBenchmarkOutput = flag.String("catalog-http-report", "", "opt-in real HTTP performance report; only temporary synthetic stores")

func TestCatalogHTTPPerformance(t *testing.T) {
	if *httpBenchmarkOutput == "" {
		t.Skip("opt-in -args -catalog-http-report=/absolute/report.json")
	}
	report := map[string]any{}
	environment := map[string]any{"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpuCount": runtime.NumCPU(), "samples": 30, "generatedAt": time.Now().UTC().Format(time.RFC3339), "coldCondition": "first HTTP request after reopening SQLite separately for every endpoint; OS filesystem cache uncontrolled, not OS-cold IO", "warmCondition": "30 sequential loopback HTTP requests on reused client/store immediately after first request; authentication, contract request validation and JSON serialization included; generation/login/query-plan collection excluded"}
	for key, cmd := range map[string][]string{"node": {"node", "--version"}, "cpuModel": {"sysctl", "-n", "machdep.cpu.brand_string"}, "memoryBytes": {"sysctl", "-n", "hw.memsize"}} {
		if b, e := exec.Command(cmd[0], cmd[1:]...).Output(); e == nil {
			environment[key] = strings.TrimSpace(string(b))
		}
	}
	report["environment"] = environment
	datasets := []any{}
	for _, files := range []int{1000, 10000, 50000} {
		t.Run(strconv.Itoa(files), func(t *testing.T) {
			data, root := t.TempDir(), t.TempDir()
			ctx := context.Background()
			s, e := store.Open(ctx, data)
			if e != nil {
				t.Fatal(e)
			}
			start := time.Now()
			store.PopulateHTTPFixture(t, s, files)
			generation := time.Since(start).Milliseconds()
			plans := store.HTTPQueryPlans(t, s)
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
			results := []any{}
			for _, path := range []string{"/api/home", "/api/catalog?libraryId=lib-00", "/api/catalog/groups", "/api/catalog?view=recent", "/api/catalog?libraryId=lib-00&q=000", "/api/media/file-00000", "/api/series/series-00000", "/api/people/person-000/works", "/api/people/person-000"} {
				s, e = store.Open(ctx, data)
				if e != nil {
					t.Fatal(e)
				}
				fs, e := mediafs.New([]string{root}, data)
				if e != nil {
					t.Fatal(e)
				}
				identity, e := identity.New(ctx, s, "family", "synthetic-only-password")
				if e != nil {
					t.Fatal(e)
				}
				c, e := catalog.New(ctx, s, fs)
				if e != nil {
					t.Fatal(e)
				}
				m, e := metadata.New(ctx, s)
				if e != nil {
					t.Fatal(e)
				}
				handler, e := transport.New(s, identity, c, m, []string{root}, false)
				if e != nil {
					t.Fatal(e)
				}
				server := httptest.NewServer(handler)
				jar, _ := cookiejar.New(nil)
				client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
				resp, e := client.Post(server.URL+"/api/login", "application/json", strings.NewReader(`{"username":"family","password":"synthetic-only-password"}`))
				if e != nil {
					t.Fatal(e)
				}
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 {
					t.Fatal("login", resp.StatusCode)
				}
				request := func() (float64, []byte) {
					start := time.Now()
					resp, e := client.Get(server.URL + path)
					if e != nil {
						t.Fatal(e)
					}
					b, e := io.ReadAll(resp.Body)
					resp.Body.Close()
					if e != nil || resp.StatusCode != 200 {
						t.Fatal(path, resp.StatusCode, string(b), e)
					}
					return float64(time.Since(start).Microseconds()) / 1000, b
				}
				cold, b := request()
				times := []float64{}
				for i := 0; i < 30; i++ {
					ms, _ := request()
					times = append(times, ms)
				}
				sort.Float64s(times)
				var doc map[string]json.RawMessage
				json.Unmarshal(b, &doc)
				rows := map[string]int{}
				for _, key := range []string{"items", "libraries", "continueWatching", "recent", "seasons"} {
					if raw, ok := doc[key]; ok {
						var a []any
						json.Unmarshal(raw, &a)
						rows[key] = len(a)
					}
				}
				p95 := times[int(math.Ceil(float64(len(times))*.95))-1]
				results = append(results, map[string]any{"path": path, "firstReadMs": cold, "warmP50Ms": times[14], "warmP95Ms": p95, "responseBytes": len(b), "returnedRows": rows})
				t.Logf("%d %s cold=%.2f p95=%.2f bytes=%d", files, path, cold, p95, len(b))
				if p95 > 200 {
					t.Errorf("hot p95 exceeds 200ms: %s %.2f", path, p95)
				}
				if path == "/api/home" || strings.HasPrefix(path, "/api/catalog?") {
					if len(b) > 256<<10 {
						t.Error("response exceeds 256KiB")
					}
				}
				server.Close()
				client.CloseIdleConnections()
				if e = c.Close(); e != nil {
					t.Fatal(e)
				}
				m.Close()
				if e = s.Close(); e != nil {
					t.Fatal(e)
				}
			}
			datasets = append(datasets, map[string]any{"files": files, "subjects": min(files, 5000), "libraries": 10, "catalogRows": files + min(files, 5000), "creditsPerSubject": 150, "generationMs": generation, "queryPlans": plans, "results": results})
		})
	}
	report["datasets"] = datasets
	b, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(*httpBenchmarkOutput, append(b, '\n'), 0600); e != nil {
		t.Fatal(e)
	}
}
