package catalog

import (
	"path/filepath"
	"reelnest/internal/model"
	"regexp"
	"strconv"
	"strings"
)

type episodeName struct {
	title   string
	season  int
	episode int
	reason  string
}

var (
	episodeMarker  = regexp.MustCompile(`(?i)s([0-9]+)e([0-9]+)`)
	numericEpisode = regexp.MustCompile(`^[0-9]+$`)
	extraEpisode   = regexp.MustCompile(`(?i)^[\s._-]*e[0-9]+`)
	episodeRange   = regexp.MustCompile(`(?i)^\s*[-+&~～至到.,_]+\s*e?[0-9]+(?:$|[^a-z0-9])`)
	seasonRange    = regexp.MustCompile(`(?i)s[0-9]+\s*[-+&~]\s*s?[0-9]+e[0-9]+`)
	seasonFolder   = regexp.MustCompile(`(?i)^(?:(?:season[ ._-]*|s)([0-9]+)|第([0-9]+)季)$`)
)

func asciiLetterOrDigit(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func titleFromFolder(relativePath string) string {
	parent := filepath.Dir(relativePath)
	if seasonFolder.MatchString(filepath.Base(parent)) {
		parent = filepath.Dir(parent)
	}
	if parent == "." {
		return ""
	}
	title := localTitle(filepath.Base(parent))
	switch strings.ToLower(title) {
	case "tv", "tv shows", "tvshows", "shows", "series", "season", "episodes", "media", "videos", "downloads", "unknown", "misc", "电视剧", "视频", "下载", "未命名":
		return ""
	}
	return title
}

func localTitle(value string) string {
	value = strings.NewReplacer(".", " ", "_", " ").Replace(value)
	return strings.Join(strings.Fields(strings.Trim(value, " -")), " ")
}

func seasonFromFolder(value string) (int, bool) {
	match := seasonFolder.FindStringSubmatch(value)
	if match == nil {
		return 0, false
	}
	number := match[1]
	if number == "" {
		number = match[2]
	}
	season, err := strconv.Atoi(number)
	if err != nil || season > 999 {
		return -1, true
	}
	return season, true
}

func identifyEpisode(relativePath string) episodeName {
	filename := filepath.Base(relativePath)
	stem := strings.TrimSuffix(filename, filepath.Ext(filename))
	markers := episodeMarker.FindAllStringSubmatchIndex(stem, -1)
	if len(markers) == 0 && numericEpisode.MatchString(stem) {
		episode, err := strconv.Atoi(stem)
		if err != nil || episode < 1 || episode > 9999 {
			return episodeName{reason: "季集编号超出支持范围"}
		}
		season := 1
		if folderSeason, ok := seasonFromFolder(filepath.Base(filepath.Dir(relativePath))); ok {
			if folderSeason < 0 {
				return episodeName{reason: "季集编号超出支持范围"}
			}
			season = folderSeason
		}
		title := titleFromFolder(relativePath)
		if title == "" {
			return episodeName{reason: "无法可靠识别电视剧名"}
		}
		return episodeName{title: title, season: season, episode: episode}
	}
	if len(markers) != 1 {
		return episodeName{reason: "未识别到唯一的 S01E01 季集标记"}
	}
	marker := markers[0]
	suffix := stem[marker[1]:]
	if extraEpisode.MatchString(suffix) || episodeRange.MatchString(suffix) || seasonRange.MatchString(stem) {
		return episodeName{reason: "疑似多集合一文件，保留为一个未组织的媒体文件"}
	}
	if (marker[0] > 0 && asciiLetterOrDigit(stem[marker[0]-1])) || (len(suffix) > 0 && asciiLetterOrDigit(suffix[0])) {
		return episodeName{reason: "季集标记边界不明确，未自动组织"}
	}
	season, seasonErr := strconv.Atoi(stem[marker[2]:marker[3]])
	episode, episodeErr := strconv.Atoi(stem[marker[4]:marker[5]])
	if seasonErr != nil || episodeErr != nil || season > 999 || episode < 1 || episode > 9999 {
		return episodeName{reason: "季集编号超出支持范围"}
	}
	title := localTitle(stem[:marker[0]])
	if title == "" {
		title = titleFromFolder(relativePath)
	}
	if title == "" || seasonFolder.MatchString(title) {
		return episodeName{reason: "无法可靠识别电视剧名"}
	}
	return episodeName{title: title, season: season, episode: episode}
}

func IdentifyEpisode(path string) model.Organization {
	n := identifyEpisode(path)
	return model.Organization{Title: n.title, Key: strings.ToLower(n.title), Reason: n.reason, Season: n.season, Episode: n.episode}
}
