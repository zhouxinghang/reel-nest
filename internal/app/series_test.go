package app_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"reelnest/internal/app"
)

type episodeReference struct {
	SeriesID      string `json:"seriesId"`
	SeriesTitle   string `json:"seriesTitle"`
	SeasonNumber  int    `json:"seasonNumber"`
	EpisodeNumber int    `json:"episodeNumber"`
}

type seriesSummary struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	DirectoryID  string `json:"directoryId"`
	SeasonCount  int    `json:"seasonCount"`
	EpisodeCount int    `json:"episodeCount"`
	FileCount    int    `json:"fileCount"`
}

type seriesDetail struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Seasons []struct {
		Number   int `json:"number"`
		Episodes []struct {
			Number int         `json:"number"`
			Files  []mediaFile `json:"files"`
		} `json:"episodes"`
	} `json:"seasons"`
}

func addSeriesDirectory(t *testing.T, a *runningApp, path string) directory {
	t.Helper()
	body, _ := request(t, a.client, "POST", a.server.URL+"/api/directories", map[string]string{"path": path, "kind": "series"}, 201)
	var result directory
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	awaitScan(t, a, result.ID, "completed")
	return result
}

func seriesLibrary(t *testing.T, a *runningApp) ([]mediaFile, []seriesSummary) {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/library?kind=series", nil, 200)
	var result struct {
		Items  []mediaFile     `json:"items"`
		Series []seriesSummary `json:"series"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result.Items, result.Series
}

func readSeries(t *testing.T, a *runningApp, id string) seriesDetail {
	t.Helper()
	body, _ := request(t, a.client, "GET", a.server.URL+"/api/series/"+id, nil, 200)
	var result seriesDetail
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSeriesSeasonNavigationSortsNumericallyAndKeepsVersions(t *testing.T) {
	root := t.TempDir()
	files := []string{
		"flat/回家.S10E2.mp4",
		"flat/回家.S2E10.mkv",
		"edition-a/回家.S2E2.1080p.mkv",
		"edition-b/回家.S02E02.4K.mkv",
	}
	for _, path := range files {
		writeMedia(t, filepath.Join(root, path), "isolated episode fixture")
	}
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	addSeriesDirectory(t, a, root)
	items, series := seriesLibrary(t, a)
	if len(items) != 4 || len(series) != 1 {
		t.Fatalf("expected four files in one series, got %d files, %+v", len(items), series)
	}
	if series[0].Title != "回家" || series[0].SeasonCount != 2 || series[0].EpisodeCount != 3 || series[0].FileCount != 4 {
		t.Fatalf("wrong series summary: %+v", series[0])
	}
	detail := readSeries(t, a, series[0].ID)
	if len(detail.Seasons) != 2 || detail.Seasons[0].Number != 2 || detail.Seasons[1].Number != 10 {
		t.Fatalf("seasons not numerically sorted: %+v", detail)
	}
	episodes := detail.Seasons[0].Episodes
	if len(episodes) != 2 || episodes[0].Number != 2 || episodes[1].Number != 10 {
		t.Fatalf("episodes not numerically sorted: %+v", episodes)
	}
	if len(episodes[0].Files) != 2 || episodes[0].Files[0].ID == episodes[0].Files[1].ID {
		t.Fatalf("file versions were merged: %+v", episodes[0])
	}
	ids := map[string]bool{}
	for _, season := range detail.Seasons {
		for _, episode := range season.Episodes {
			for _, file := range episode.Files {
				if ids[file.ID] {
					t.Fatal("one file appears as multiple episodes")
				}
				ids[file.ID] = true
				body, _ := request(t, a.client, "GET", a.server.URL+"/api/media/"+file.ID, nil, 200)
				var actual mediaFile
				if err := json.Unmarshal(body, &actual); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual.Episode, &episodeReference{SeriesID: detail.ID, SeriesTitle: "回家", SeasonNumber: season.Number, EpisodeNumber: episode.Number}) {
					t.Fatalf("file does not link to its series/season/episode: %s", body)
				}
				if actual.Matched {
					t.Fatal("local organization falsely claims title metadata matching")
				}
			}
		}
	}
	if len(ids) != 4 {
		t.Fatal("series navigation lost a file")
	}
}

func TestEpisodeRecognitionIsConservativeAndIndependentOfFolderLayout(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		path, title     string
		season, episode int
	}{
		{"flat/North.Star.s01e02.1080p.mkv", "North Star", 1, 2},
		{"flat/North_Star.S2E03.4K.mp4", "North Star", 2, 3},
		{"random/北极星S01E02.mp4", "北极星", 1, 2},
		{"城南往事/Season 10/S10E03.mp4", "城南往事", 10, 3},
		{"城南往事/Season 1/S01E001.webm", "城南往事", 1, 1},
		{"城南往事/S00/S00E01.extra.mp4", "城南往事", 0, 1},
		{"城南往事/Season 02/S01E05.mp4", "城南往事", 1, 5},
		{"unrelated/folders/城南往事.S2E3.mp4", "城南往事", 2, 3},
		{"太平年/01.mp4", "太平年", 1, 1},
		{"太平年/第2季/03.mp4", "太平年", 2, 3},
		{"multi/回家.S01E01E02.mkv", "", 0, 0},
		{"multi/回家.S01E01-02.mkv", "", 0, 0},
		{"multi/回家.S01E01-E02.mkv", "", 0, 0},
		{"multi/回家.S01E01+E02.mkv", "", 0, 0},
		{"multi/回家.S01E01 & E02.mkv", "", 0, 0},
		{"multi/回家.S01E01.S01E02.mp4", "", 0, 0},
		{"multi/回家.S01E01.E02.mp4", "", 0, 0},
		{"multi/回家.S01E01.02.mp4", "", 0, 0},
		{"multi/回家.S01E01~02.mp4", "", 0, 0},
		{"multi/回家.S01-S02E01.mp4", "", 0, 0},
		{"unknown/readme.mp4", "", 0, 0},
		{"S01E01.mp4", "", 0, 0},
		{"TV/Season 1/S01E01.mp4", "", 0, 0},
		{"unknown/回家.S01E00.mp4", "", 0, 0},
		{"unknown/回家.S999999E1.mp4", "", 0, 0},
		{"unknown/S1000/01.mp4", "", 0, 0},
		{"unknown/回家.S01E01abc.mp4", "", 0, 0},
		{"unknown/NEWS01E01.mp4", "", 0, 0},
	}
	for _, tc := range cases {
		writeMedia(t, filepath.Join(root, "tv", tc.path), "isolated naming fixture")
	}
	writeMedia(t, filepath.Join(root, "movies", "城南往事.S01E01.mp4"), "a movie, not an episode")
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	addSeriesDirectory(t, a, filepath.Join(root, "tv"))
	movieDirectory := addDirectory(t, a, filepath.Join(root, "movies"))
	awaitScan(t, a, movieDirectory.ID, "completed")
	items, series := seriesLibrary(t, a)
	if len(items) != len(cases) {
		t.Fatalf("unrecognized files were dropped: %d != %d", len(items), len(cases))
	}
	byPath := map[string]mediaFile{}
	for _, file := range items {
		byPath[file.Path] = file
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			file, ok := byPath[filepath.Join(root, "tv", tc.path)]
			if !ok {
				t.Fatal("file missing")
			}
			if file.Matched {
				t.Fatal("filename parsing must not pretend to be metadata matching")
			}
			if tc.title == "" {
				if file.Episode != nil || file.OrganizationReason == "" {
					t.Fatalf("ambiguous file forced into an episode: %+v", file)
				}
			} else if file.Episode == nil || file.Episode.SeriesTitle != tc.title || file.Episode.SeasonNumber != tc.season || file.Episode.EpisodeNumber != tc.episode || file.OrganizationReason != "" {
				t.Fatalf("expected %s S%dE%d, got %+v (reason %q)", tc.title, tc.season, tc.episode, file.Episode, file.OrganizationReason)
			}
		})
	}
	if len(series) != 4 {
		t.Fatalf("unexpected guessed series: %+v", series)
	}
	for _, file := range library(t, a) {
		if file.Kind == "movie" && (file.Episode != nil || file.OrganizationReason != "") {
			t.Fatal("movie was incorrectly organized as an episode")
		}
	}
}

func TestNumericEpisodeFilenameUsesSeriesFolderAndEmptySeriesIsRemoved(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "太平年", "01.mp4")
	writeMedia(t, path, "isolated episode fixture")
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	d := addSeriesDirectory(t, a, root)
	items, shows := seriesLibrary(t, a)
	if len(items) != 1 || len(shows) != 1 || items[0].Episode == nil {
		t.Fatalf("numeric episode was not organized: items=%+v series=%+v", items, shows)
	}
	if ref := items[0].Episode; ref.SeriesTitle != "太平年" || ref.SeasonNumber != 1 || ref.EpisodeNumber != 1 {
		t.Fatalf("wrong numeric episode organization: %+v", ref)
	}
	seriesID := shows[0].ID
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	rescan(t, a, d.ID)
	awaitScan(t, a, d.ID, "completed")
	items, shows = seriesLibrary(t, a)
	if len(items) != 0 || len(shows) != 0 {
		t.Fatalf("missing episode or empty series remained: items=%+v series=%+v", items, shows)
	}
	request(t, a.client, "GET", a.server.URL+"/api/series/"+seriesID, nil, 404)
}

func TestSeriesOrganizationSurvivesRescanAndOfflineRestart(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	sources := []string{"version-a/一家人.S01E01.mp4", "version-b/一家人.S01E01.mp4", "一家人/Season 2/S02E01.mp4", "unrecognized.mp4"}
	for _, path := range sources {
		writeMedia(t, filepath.Join(root, path), "do not change this source")
	}
	a := start(t, app.Config{DataDir: data, Username: "family", Password: password, MediaRoots: []string{root}})
	login(t, a)
	dir := addSeriesDirectory(t, a, root)
	beforeItems, beforeSeries := seriesLibrary(t, a)
	if len(beforeSeries) != 1 {
		t.Fatalf("expected one series: %+v", beforeSeries)
	}
	beforeDetail := readSeries(t, a, beforeSeries[0].ID)
	rescan(t, a, dir.ID)
	awaitScan(t, a, dir.ID, "completed")
	items, series := seriesLibrary(t, a)
	if !reflect.DeepEqual(items, beforeItems) || !reflect.DeepEqual(series, beforeSeries) || !reflect.DeepEqual(readSeries(t, a, series[0].ID), beforeDetail) {
		t.Fatal("rescan changed organization or file identities")
	}
	for _, path := range sources {
		contents, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || string(contents) != "do not change this source" {
			t.Fatal("organization modified or moved a source file")
		}
	}
	a.close()
	// This is an externally simulated unavailable mount, not an application rename.
	offline := root + "-offline"
	if err := os.Rename(root, offline); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(offline, root) })
	b := start(t, app.Config{DataDir: data, MediaRoots: []string{root}})
	login(t, b)
	items, series = seriesLibrary(t, b)
	if !reflect.DeepEqual(items, beforeItems) || !reflect.DeepEqual(series, beforeSeries) || !reflect.DeepEqual(readSeries(t, b, series[0].ID), beforeDetail) {
		t.Fatal("restart lost persistent organization or required source files")
	}
}

func TestSeriesAuthorizationAndSameTitleDirectoryIsolation(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"first", "second"} {
		writeMedia(t, filepath.Join(root, dir, "同名作品.S01E01.mp4"), "different files")
	}
	a := start(t, app.Config{DataDir: t.TempDir(), Username: "family", Password: password, MediaRoots: []string{root}})
	request(t, a.client, "GET", a.server.URL+"/api/series/unknown", nil, 401)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request(t, client, "GET", a.server.URL+"/series/unknown", nil, 303)
	login(t, a)
	addSeriesDirectory(t, a, filepath.Join(root, "first"))
	addSeriesDirectory(t, a, filepath.Join(root, "second"))
	items, series := seriesLibrary(t, a)
	if len(items) != 2 || len(series) != 2 || series[0].ID == series[1].ID || series[0].DirectoryID == series[1].DirectoryID {
		t.Fatalf("unrelated directories were forced into one series: %+v", series)
	}
	for _, show := range series {
		detail := readSeries(t, a, show.ID)
		if len(detail.Seasons) != 1 || len(detail.Seasons[0].Episodes) != 1 || len(detail.Seasons[0].Episodes[0].Files) != 1 {
			t.Fatalf("wrong directory-scoped series: %+v", detail)
		}
	}
	request(t, a.client, "GET", a.server.URL+"/api/series/does-not-exist", nil, 404)
	request(t, a.client, "GET", a.server.URL+"/api/series/"+items[0].ID, nil, 404)
}
