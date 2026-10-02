package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var baselineOutput = flag.String("baseline-output", "", "write synthetic schema-probe report (not production HTTP benchmarks)")

// populateSynthetic is shared by schema/query tests. It writes only a Store
// created by the harness in t.TempDir(), never reads source media or credentials.
// Each series has one subject, multiple independent file versions/episodes; this
// makes 50,000 files + exactly 5,000 local metadata subjects, not orphan targets.
func populateSynthetic(t testing.TB, s *Store, files, subjects, credits int) {
	t.Helper()
	if subjects > files || subjects < 10 || files > 50000 || credits > 150 {
		t.Fatal("invalid synthetic scale")
	}
	tx, err := s.writer.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	statements := map[string]*sql.Stmt{}
	run := func(q string, args ...any) {
		stmt := statements[q]
		if stmt == nil {
			var err error
			stmt, err = tx.Prepare(q)
			if err != nil {
				t.Fatal(err)
			}
			statements[q] = stmt
		}
		if _, err := stmt.Exec(args...); err != nil {
			t.Fatalf("fixture %s: %v", q, err)
		}
	}
	defer func() {
		for _, stmt := range statements {
			stmt.Close()
		}
	}()
	const epoch = int64(1790800000000)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("lib-%02d", i)
		run("INSERT INTO libraries VALUES(?,?,?,?, 'series',1,1,?)", id, "合成媒体库 "+strconv.Itoa(i), "/synthetic-only/"+id, "synthetic:"+id, epoch)
		run("INSERT INTO library_sources VALUES(?,'tmdb',0)", id)
	}
	for i := 0; i < credits; i++ {
		run("INSERT INTO people VALUES(?,'tmdb',?)", fmt.Sprintf("person-%03d", i), fmt.Sprintf("remote:%d", i))
	}
	for i := 0; i < subjects; i++ {
		id := fmt.Sprintf("series-%05d", i)
		lib := fmt.Sprintf("lib-%02d", i%10)
		remote := "remote-" + id
		subject := "subject-" + id
		title := fmt.Sprintf("合成电视剧 %05d", i)
		run("INSERT INTO series VALUES(?,?,?,?)", id, lib, title, title)
		run("INSERT INTO metadata_subjects VALUES(?,?,'series',NULL,?,1)", subject, lib, id)
		run("INSERT INTO remote_titles VALUES(?,'tmdb','tmdb','series',?)", remote, fmt.Sprintf("Raw-%05d", i))
		run("INSERT INTO metadata_bindings VALUES(?,'series',?,'manual',1,?,'succeeded',NULL)", subject, remote, epoch)
		run("INSERT INTO metadata_snapshots VALUES(?,?,?,'',2026,?,?,?, ?,1,'ready','ready',NULL,NULL)", remote, title, title, strings.Repeat("合成简介。", 80), "https://example.invalid/"+id, epoch, epoch+86400000)
		for j := 0; j < credits; j++ {
			role := "actor"
			ordinal := j
			if j >= 100 {
				role = "director"
				ordinal = j - 100
			}
			run("INSERT INTO title_credits VALUES(?,?,'tmdb',?,?,?,?,?)", fmt.Sprintf("credit-%05d-%03d", i, j), remote, fmt.Sprintf("person-%03d", j), role, ordinal, fmt.Sprintf("合成人物 %03d", j), `["角色"]`)
		}
		run("INSERT INTO assets VALUES(?,?,'tmdb',NULL,'primary','/synthetic.jpg',1,1,'missing',NULL,NULL,NULL,NULL)", "asset-"+id, remote)
		run("INSERT INTO catalog_entries VALUES(?,?,NULL,?,?,'series',?,?,?, ?,1,0,1,1)", "series:"+id, lib, id, subject, title, title, title, epoch+int64(i))
	}
	for i := 0; i < files; i++ {
		id := fmt.Sprintf("file-%05d", i)
		n := i % subjects
		series := fmt.Sprintf("series-%05d", n)
		lib := fmt.Sprintf("lib-%02d", n%10)
		episode := i/subjects + 1
		title := fmt.Sprintf("%s/Season 01/%03d.webm", series, episode)
		run("INSERT INTO media_files VALUES(?,?,?,?,1000,?,?,'available','')", id, lib, title, "synthetic:"+id, epoch, epoch+int64(i))
		run("INSERT INTO file_organization VALUES(?,?,?,1,?)", id, lib, series, episode)
		run("INSERT INTO catalog_entries VALUES(?,?,?,NULL,?,'series',?,?,?, ?,0,0,1,1)", "media:"+id, lib, id, "subject-"+series, title, title, title, epoch+int64(i))
		if i%100 == 0 {
			run("INSERT INTO watch_progress VALUES(?,30,60,0,?)", id, epoch+int64(i))
			run("INSERT INTO media_favorites VALUES(?,?)", id, epoch+int64(i))
		}
	}
	run(`UPDATE catalog_entries SET file_count=(SELECT count(*) FROM file_organization o WHERE o.series_id=catalog_entries.series_id),available_file_count=(SELECT count(*) FROM file_organization o WHERE o.series_id=catalog_entries.series_id),episode_count=(SELECT count(*) FROM file_organization o WHERE o.series_id=catalog_entries.series_id),added_at=(SELECT max(m.added_at) FROM file_organization o JOIN media_files m ON m.id=o.media_file_id WHERE o.series_id=catalog_entries.series_id) WHERE series_id IS NOT NULL`)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.writer.Exec("ANALYZE"); err != nil {
		t.Fatal(err)
	}
}

func TestSyntheticFixtureRelationships(t *testing.T) {
	s := openTestStore(t)
	populateSynthetic(t, s, 100, 20, 3)
	if count(t, s, "media_files") != 100 || count(t, s, "metadata_subjects") != 20 || count(t, s, "title_credits") != 60 {
		t.Fatal("bad fixture scale")
	}
	rows, err := s.reader.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("fixture FK violation")
	}
}

type probe struct {
	Name string `json:"name"`
	SQL  string `json:"sql"`
	Args []any  `json:"args"`
}

var probes = []probe{
	{"detail-target", `SELECT m.id,m.relative_path,m.size,b.selection,s.title,s.overview FROM media_files m JOIN file_organization o ON o.media_file_id=m.id JOIN metadata_subjects sub ON sub.series_id=o.series_id LEFT JOIN metadata_bindings b ON b.subject_id=sub.id LEFT JOIN metadata_snapshots s ON s.remote_title_id=b.remote_title_id AND s.expires_at>? WHERE m.id=?`, []any{int64(1790800000100), "file-00000"}},
	{"library-first-page", `SELECT e.entry_key,e.local_title,e.added_at,e.file_count,s.title FROM catalog_entries e LEFT JOIN metadata_bindings b ON b.subject_id=e.subject_id LEFT JOIN metadata_snapshots s ON s.remote_title_id=b.remote_title_id AND s.expires_at>? WHERE e.library_id=? AND e.series_id IS NOT NULL ORDER BY e.title_sort_key,e.entry_key LIMIT 50`, []any{int64(1790800000100), "lib-00"}},
	{"person-50-local-works", `SELECT DISTINCT r.id,s.title FROM title_credits c JOIN remote_titles r ON r.id=c.remote_title_id JOIN metadata_snapshots s ON s.remote_title_id=r.id AND s.expires_at>? JOIN metadata_bindings b ON b.remote_title_id=r.id JOIN metadata_subjects sub ON sub.id=b.subject_id WHERE c.catalog_id='tmdb' AND c.person_id=? ORDER BY r.id LIMIT 50`, []any{int64(1790800000100), "person-000"}},
	{"substring-search", `SELECT entry_key,local_title FROM catalog_entries WHERE local_search_key LIKE ? AND series_id IS NOT NULL ORDER BY title_sort_key,entry_key LIMIT 50`, []any{"%000%"}},
}

type probeResult struct {
	probe
	Rows          int      `json:"rowsReturned"`
	ResponseBytes int      `json:"rowJSONBytes"`
	FirstMS       float64  `json:"firstReadMs"`
	P50MS         float64  `json:"warmP50Ms"`
	P95MS         float64  `json:"warmP95Ms"`
	Plan          []string `json:"queryPlan"`
}

func runProbe(t testing.TB, s *Store, p probe) (int, int) {
	t.Helper()
	rows, err := s.reader.QueryContext(context.Background(), p.SQL, p.Args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		row := map[string]any{}
		for i, c := range cols {
			row[c] = vals[i]
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return len(out), len(b)
}

// Opt-in, repeatable measurement tool. It measures SQL schema probes, not final
// HTTP requests or OS-cold IO. Stage 04 must replace/add real production queries.
func TestSyntheticQueryBaseline(t *testing.T) {
	if *baselineOutput == "" {
		t.Skip("opt-in: -args -baseline-output=/absolute/report.json")
	}
	report := struct {
		Environment map[string]any   `json:"environment"`
		Datasets    []map[string]any `json:"datasets"`
	}{Environment: map[string]any{"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpuCount": runtime.NumCPU(), "generatedAt": time.Now().UTC().Format(time.RFC3339), "samples": 30, "note": "schema probes only; row JSON is not HTTP DTO; first read after reopening SQLite with OS cache uncontrolled; no end-to-end performance claims"}}
	for key, command := range map[string][]string{"node": {"node", "--version"}, "cpuModel": {"sysctl", "-n", "machdep.cpu.brand_string"}, "memoryBytes": {"sysctl", "-n", "hw.memsize"}} {
		if output, err := exec.Command(command[0], command[1:]...).Output(); err == nil {
			report.Environment[key] = strings.TrimSpace(string(output))
		} else {
			report.Environment[key] = "not measured"
		}
	}
	for _, files := range []int{1000, 10000, 50000} {
		t.Run(strconv.Itoa(files), func(t *testing.T) {
			s := openTestStore(t)
			subjects := min(files, 5000)
			start := time.Now()
			populateSynthetic(t, s, files, subjects, 150)
			generation := time.Since(start)
			path := s.directory.Name()
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			var err error
			s, err = Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			results := []probeResult{}
			for _, p := range probes {
				start := time.Now()
				n, size := runProbe(t, s, p)
				first := float64(time.Since(start).Microseconds()) / 1000
				times := make([]float64, 30)
				for i := range times {
					start := time.Now()
					runProbe(t, s, p)
					times[i] = float64(time.Since(start).Microseconds()) / 1000
				}
				sort.Float64s(times)
				rows, err := s.reader.Query("EXPLAIN QUERY PLAN "+p.SQL, p.Args...)
				if err != nil {
					t.Fatal(err)
				}
				plan := []string{}
				for rows.Next() {
					var a, b, c int
					var detail string
					if err = rows.Scan(&a, &b, &c, &detail); err != nil {
						t.Fatal(err)
					}
					plan = append(plan, detail)
				}
				if err = rows.Err(); err != nil {
					t.Fatal(err)
				}
				rows.Close()
				results = append(results, probeResult{p, n, size, first, times[14], times[int(math.Ceil(float64(len(times))*.95))-1], plan})
			}
			report.Datasets = append(report.Datasets, map[string]any{"files": files, "subjects": subjects, "creditsPerSubject": 150, "libraries": 10, "generationMs": generation.Milliseconds(), "results": results})
		})
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(*baselineOutput, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
