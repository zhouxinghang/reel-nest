package app

import (
	"net/http"
	"sort"
	"strings"
)

type SeriesSummary struct {
	Match              metadataAssociation `json:"match"`
	DirectoryPath      string              `json:"directoryPath"`
	ID                 string              `json:"id"`
	Title              string              `json:"title"`
	DirectoryID        string              `json:"directoryId"`
	SeasonCount        int                 `json:"seasonCount"`
	EpisodeCount       int                 `json:"episodeCount"`
	FileCount          int                 `json:"fileCount"`
	AvailableFileCount int                 `json:"availableFileCount"`
	Matched            bool                `json:"matched"`
}

type Episode struct {
	Number int         `json:"number"`
	Files  []MediaFile `json:"files"`
}

type Season struct {
	Number   int       `json:"number"`
	Episodes []Episode `json:"episodes"`
}

type SeriesDetail struct {
	SeriesSummary
	Seasons []Season `json:"seasons"`
}

func seriesSummaries(files []MediaFile) []SeriesSummary {
	type collection struct {
		summary SeriesSummary
		seasons map[int]map[int]bool
	}
	groups := map[string]*collection{}
	for _, file := range files {
		ref := file.Episode
		if ref == nil {
			continue
		}
		group := groups[ref.SeriesID]
		if group == nil {
			group = &collection{summary: SeriesSummary{ID: ref.SeriesID, Title: ref.SeriesTitle, Match: file.Match, Matched: file.Matched, DirectoryID: file.DirectoryID, DirectoryPath: file.DirectoryPath}, seasons: map[int]map[int]bool{}}
			groups[ref.SeriesID] = group
		}
		group.summary.FileCount++
		if file.Available {
			group.summary.AvailableFileCount++
		}
		if group.seasons[ref.SeasonNumber] == nil {
			group.seasons[ref.SeasonNumber] = map[int]bool{}
		}
		group.seasons[ref.SeasonNumber][ref.EpisodeNumber] = true
	}
	result := []SeriesSummary{}
	for _, group := range groups {
		group.summary.SeasonCount = len(group.seasons)
		for _, episodes := range group.seasons {
			group.summary.EpisodeCount += len(episodes)
		}
		result = append(result, group.summary)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i].Title), strings.ToLower(result[j].Title)
		if left == right {
			return result[i].ID < result[j].ID
		}
		return left < right
	})
	return result
}

func (a *App) series(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.QueryContext(r.Context(), "SELECT "+mediaColumns+mediaFrom+" WHERE o.series_id=? ORDER BY o.season_number,o.episode_number,f.filename,f.relative_path,f.id", r.PathValue("id"))
	if err != nil {
		fail(w, 500, "无法读取电视剧")
		return
	}
	defer rows.Close()
	files := []MediaFile{}
	for rows.Next() {
		file, err := scanMedia(rows)
		if err != nil {
			fail(w, 500, "无法读取电视剧")
			return
		}
		files = append(files, file)
	}
	if rows.Err() != nil {
		fail(w, 500, "无法读取电视剧")
		return
	}
	if len(files) == 0 {
		fail(w, 404, "电视剧不存在")
		return
	}
	rows.Close()
	if err := a.attachMetadata(r.Context(), files); err != nil {
		fail(w, 500, "无法读取影片资料")
		return
	}
	detail := SeriesDetail{SeriesSummary: seriesSummaries(files)[0], Seasons: []Season{}}
	// The query's numeric ordering makes the hierarchy deterministic; file IDs,
	// not the episode number, remain the eventual playback/deletion targets.
	for _, file := range files {
		ref := file.Episode
		if len(detail.Seasons) == 0 || detail.Seasons[len(detail.Seasons)-1].Number != ref.SeasonNumber {
			detail.Seasons = append(detail.Seasons, Season{Number: ref.SeasonNumber, Episodes: []Episode{}})
		}
		season := &detail.Seasons[len(detail.Seasons)-1]
		if len(season.Episodes) == 0 || season.Episodes[len(season.Episodes)-1].Number != ref.EpisodeNumber {
			season.Episodes = append(season.Episodes, Episode{Number: ref.EpisodeNumber, Files: []MediaFile{}})
		}
		episode := &season.Episodes[len(season.Episodes)-1]
		episode.Files = append(episode.Files, file)
	}
	respond(w, 200, detail)
}
