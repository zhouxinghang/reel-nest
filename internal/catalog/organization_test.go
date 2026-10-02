package catalog_test

import (
	"reelnest/internal/catalog"
	"testing"
)

func TestConservativeEpisodeNamingRegression(t *testing.T) {
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
		t.Run(tc.path, func(t *testing.T) {
			got := catalog.IdentifyEpisode(tc.path)
			if got.Title != tc.title || got.Season != tc.season || got.Episode != tc.episode {
				t.Fatalf("%+v", got)
			}
			if (tc.title == "") != (got.Reason != "") {
				t.Fatalf("reason mismatch: %+v", got)
			}
		})
	}
}
