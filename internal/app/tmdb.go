package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Only this adapter knows TMDB's wire format. Requests are bounded, paced and
// never retried automatically; the local library never waits on this client.
type tmdbClient struct {
	*metadataHTTP
	token        string
	photoHTTP    *metadataHTTP
	thumbHTTP    *metadataHTTP
	backdropHTTP *metadataHTTP
}

type tmdbTitle struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	Name          string `json:"name"`
	OriginalTitle string `json:"original_title"`
	OriginalName  string `json:"original_name"`
	Overview      string `json:"overview"`
	ReleaseDate   string `json:"release_date"`
	FirstAirDate  string `json:"first_air_date"`
	PosterPath    string `json:"poster_path"`
}

func newTMDB(config Config) (*tmdbClient, error) {
	apiURL, imageURL := config.TMDBAPIURL, config.TMDBImageURL
	if apiURL == "" {
		apiURL = "https://api.themoviedb.org/3"
	}
	if imageURL == "" {
		imageURL = "https://image.tmdb.org/t/p/w500"
	}
	client, err := newMetadataHTTP("TMDB", apiURL, imageURL, config.TMDBTimeout)
	if err != nil {
		return nil, err
	}
	profileURL := config.TMDBImageURL
	if profileURL == "" {
		profileURL = "https://image.tmdb.org/t/p/w185"
	}
	photoHTTP, err := newMetadataHTTP("TMDB 人物照片", apiURL, profileURL, config.TMDBTimeout)
	if err != nil {
		return nil, err
	}
	thumbURL, backdropURL := config.TMDBImageURL, config.TMDBImageURL
	if thumbURL == "" {
		thumbURL, backdropURL = "https://image.tmdb.org/t/p/w780", "https://image.tmdb.org/t/p/w1280"
	}
	thumbHTTP, err := newMetadataHTTP("TMDB 横版封面", apiURL, thumbURL, config.TMDBTimeout)
	if err != nil {
		return nil, err
	}
	backdropHTTP, err := newMetadataHTTP("TMDB 背景图", apiURL, backdropURL, config.TMDBTimeout)
	if err != nil {
		return nil, err
	}
	return &tmdbClient{metadataHTTP: client, token: strings.TrimSpace(config.TMDBToken), photoHTTP: photoHTTP, thumbHTTP: thumbHTTP, backdropHTTP: backdropHTTP}, nil
}

func (c *tmdbClient) configured() bool { return c.token != "" }
func (c *tmdbClient) get(ctx context.Context, endpoint string, authenticated bool, limit int64) ([]byte, string, error) {
	if !c.configured() {
		return nil, "", errors.New("未配置 TMDB 凭据；本地浏览与播放不受影响")
	}
	token := ""
	if authenticated {
		token = c.token
	}
	return c.request(ctx, "GET", endpoint, token, nil, limit)
}

func tmdbKind(kind string) string {
	if kind == "series" {
		return "tv"
	}
	return "movie"
}
func first(values ...string) string {
	for _, s := range values {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
func (t tmdbTitle) metadata(kind string) TitleMetadata {
	date := first(t.ReleaseDate, t.FirstAirDate)
	year := ""
	if len(date) >= 4 {
		year = date[:4]
	}
	return TitleMetadata{Source: "TMDB", SourceID: t.ID, TMDBID: t.ID, Kind: kind, Title: first(t.Title, t.Name, t.OriginalTitle, t.OriginalName), OriginalTitle: first(t.OriginalTitle, t.OriginalName), Year: year, Overview: t.Overview, SourceURL: fmt.Sprintf("https://www.themoviedb.org/%s/%d", tmdbKind(kind), t.ID)}
}

func (c *tmdbClient) json(ctx context.Context, path string, query url.Values, target any) error {
	body, _, err := c.get(ctx, c.apiURL+path+"?"+query.Encode(), true, 1<<20)
	if err != nil {
		return err
	}
	if json.Unmarshal(body, target) != nil {
		return errors.New("TMDB 返回无效的资料")
	}
	return nil
}

func (c *tmdbClient) search(ctx context.Context, kind, query string) ([]TitleMetadata, bool, error) {
	var result struct {
		Results      []tmdbTitle `json:"results"`
		TotalPages   int         `json:"total_pages"`
		TotalResults int         `json:"total_results"`
	}
	err := c.json(ctx, "/search/"+tmdbKind(kind), url.Values{"query": {query}, "language": {"zh-CN"}, "include_adult": {"false"}, "page": {"1"}}, &result)
	if err != nil {
		return nil, false, err
	}
	candidates := []TitleMetadata{}
	for _, title := range result.Results {
		m := title.metadata(kind)
		if m.TMDBID > 0 && m.Title != "" {
			candidates = append(candidates, m)
		}
		if len(candidates) >= 20 {
			break
		}
	}
	return candidates, result.TotalPages <= 1 && result.TotalResults <= 20, nil
}

var posterPath = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpg|jpeg|png|webp)$`)

func (c *tmdbClient) details(ctx context.Context, kind string, id int64) (TitleMetadata, []byte, string, string, error) {
	var title tmdbTitle
	path := fmt.Sprintf("/%s/%d", tmdbKind(kind), id)
	err := c.json(ctx, path, url.Values{"language": {"zh-CN"}}, &title)
	if err != nil {
		return TitleMetadata{}, nil, "", "", err
	}
	if title.ID != id {
		return TitleMetadata{}, nil, "", "", errors.New("TMDB 返回的作品标识不符")
	}
	warning := ""
	if first(title.Title, title.Name) == "" || title.Overview == "" || title.PosterPath == "" {
		var fallback tmdbTitle
		if err := c.json(ctx, path, url.Values{"language": {"en-US"}}, &fallback); err == nil && fallback.ID == id {
			title.OriginalTitle = first(title.OriginalTitle, fallback.OriginalTitle)
			title.OriginalName = first(title.OriginalName, fallback.OriginalName)
			title.Title = first(title.Title, fallback.Title)
			title.Name = first(title.Name, fallback.Name)
			title.Overview = first(title.Overview, fallback.Overview)
			title.PosterPath = first(title.PosterPath, fallback.PosterPath)
			title.ReleaseDate = first(title.ReleaseDate, fallback.ReleaseDate)
			title.FirstAirDate = first(title.FirstAirDate, fallback.FirstAirDate)
		} else {
			warning = "部分中文资料缺失，其他语言资料暂时无法获取"
		}
	}
	m := title.metadata(kind)
	if m.Title == "" {
		return m, nil, "", "", errors.New("TMDB 缺少可用作品标题")
	}
	m.FetchedAt = time.Now().Unix()
	m.ExpiresAt = time.Now().Add(30 * 24 * time.Hour).Unix()
	var poster []byte
	contentType := ""
	if posterPath.MatchString(title.PosterPath) {
		data, typ, err := c.get(ctx, c.imageURL+title.PosterPath, false, 5<<20)
		if err == nil && validImage(data, strings.Split(typ, ";")[0]) {
			poster, contentType = data, strings.Split(typ, ";")[0]
			m.PosterURL = fmt.Sprintf("/api/metadata/posters/%s/%d", kind, id)
		} else {
			warning = first(warning, "海报暂时不可用，使用占位图")
		}
	}
	return m, poster, contentType, warning, nil
}

func (c *tmdbClient) credits(ctx context.Context, kind string, id int64) (TitleCredits, error) {
	var result struct {
		ID   int64 `json:"id"`
		Cast []struct {
			ID           int64                        `json:"id"`
			Name         string                       `json:"name"`
			OriginalName string                       `json:"original_name"`
			ProfilePath  string                       `json:"profile_path"`
			Character    string                       `json:"character"`
			Order        int                          `json:"order"`
			Roles        []struct{ Character string } `json:"roles"`
		} `json:"cast"`
		Crew []struct {
			ProfilePath string                 `json:"profile_path"`
			ID          int64                  `json:"id"`
			Name        string                 `json:"name"`
			Job         string                 `json:"job"`
			Jobs        []struct{ Job string } `json:"jobs"`
		} `json:"crew"`
	}
	endpoint := "credits"
	if kind == "series" {
		endpoint = "aggregate_credits"
	}
	if err := c.json(ctx, fmt.Sprintf("/%s/%d/%s", tmdbKind(kind), id, endpoint), url.Values{"language": {"zh-CN"}}, &result); err != nil {
		return TitleCredits{}, err
	}
	if result.ID != id || (result.Cast == nil && result.Crew == nil) {
		return TitleCredits{}, errors.New("TMDB 返回无效的演职员资料")
	}
	sort.SliceStable(result.Cast, func(i, j int) bool { return result.Cast[i].Order < result.Cast[j].Order })
	entries := []creditEntry{}
	for _, person := range result.Cast {
		roles := []string{person.Character}
		for _, role := range person.Roles {
			roles = append(roles, role.Character)
		}
		photo := ""
		if posterPath.MatchString(person.ProfilePath) {
			photo = person.ProfilePath
		}
		entries = append(entries, creditEntry{CreditPerson: CreditPerson{ID: person.ID, Name: first(person.Name, person.OriginalName), PhotoPath: photo}, Roles: roles})
	}
	for _, person := range result.Crew {
		director := person.Job == "Director"
		for _, job := range person.Jobs {
			director = director || job.Job == "Director"
		}
		if director {
			photo := ""
			if posterPath.MatchString(person.ProfilePath) {
				photo = person.ProfilePath
			}
			entries = append(entries, creditEntry{CreditPerson: CreditPerson{ID: person.ID, Name: person.Name, PhotoPath: photo}, Director: true})
		}
	}
	return normalizeCredits(entries), nil
}

func (c *tmdbClient) photo(ctx context.Context, ref string) ([]byte, string, error) {
	if !posterPath.MatchString(ref) {
		return nil, "", errors.New("人物照片地址无效")
	}
	return c.photoHTTP.request(ctx, "GET", c.photoHTTP.imageURL+ref, "", nil, 5<<20)
}

func normalizedTitle(title string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, title)
}

var titleYear = regexp.MustCompile(`^(.*?)\s*\(((?:19|20)[0-9]{2})\)$`)

func matchQuery(title string) (string, string) {
	if parts := titleYear.FindStringSubmatch(title); parts != nil {
		return strings.TrimSpace(parts[1]), parts[2]
	}
	return strings.TrimSpace(title), ""
}
