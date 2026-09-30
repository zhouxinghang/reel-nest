package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Only this adapter knows TMDB's wire format. Requests are bounded, paced and
// never retried automatically; the local library never waits on this client.
type tmdbClient struct {
	token, apiURL, imageURL string
	client                  *http.Client
	gate                    chan struct{}
	mu                      sync.Mutex
	next, cooldown          time.Time
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

type TitleMetadata struct {
	Source        string `json:"source"`
	TMDBID        int64  `json:"tmdbId"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	OriginalTitle string `json:"originalTitle"`
	Year          string `json:"year"`
	Overview      string `json:"overview"`
	PosterURL     string `json:"posterUrl"`
	SourceURL     string `json:"sourceUrl"`
	FetchedAt     int64  `json:"fetchedAt"`
	ExpiresAt     int64  `json:"expiresAt"`
}

func newTMDB(config Config) (*tmdbClient, error) {
	apiURL, imageURL := config.TMDBAPIURL, config.TMDBImageURL
	if apiURL == "" {
		apiURL = "https://api.themoviedb.org/3"
	}
	if imageURL == "" {
		imageURL = "https://image.tmdb.org/t/p/w500"
	}
	for _, endpoint := range []string{apiURL, imageURL} {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
			return nil, errors.New("TMDB 服务地址必须是 HTTPS，或仅用于隔离测试的本机 HTTP 地址")
		}
	}
	timeout := config.TMDBTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &tmdbClient{token: strings.TrimSpace(config.TMDBToken), apiURL: strings.TrimRight(apiURL, "/"), imageURL: strings.TrimRight(imageURL, "/"), gate: make(chan struct{}, 1), client: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *tmdbClient) get(ctx context.Context, endpoint string, authenticated bool, limit int64) ([]byte, string, error) {
	if c.token == "" {
		return nil, "", errors.New("未配置 TMDB 凭据；本地浏览与播放不受影响")
	}
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return nil, "", errors.New("TMDB 请求已取消或超时")
	}
	c.mu.Lock()
	cooldown, next := c.cooldown, c.next
	c.mu.Unlock()
	if time.Now().Before(cooldown) {
		return nil, "", errors.New("TMDB 请求处于冷却期，请稍后重试；本地播放不受影响")
	}
	if delay := time.Until(next); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, "", errors.New("TMDB 请求已取消或超时")
		}
	}
	c.mu.Lock()
	c.next = time.Now().Add(250 * time.Millisecond)
	c.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, "", errors.New("TMDB 请求地址无效")
	}
	if authenticated {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if authenticated {
		req.Header.Set("Accept", "application/json")
	} else {
		req.Header.Set("Accept", "image/jpeg, image/png, image/webp")
	}
	response, err := c.client.Do(req)
	if err != nil {
		return nil, "", errors.New("TMDB 网络不可用或请求超时；本地播放不受影响")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		if response.StatusCode == 429 || response.StatusCode >= 500 || response.StatusCode == 401 || response.StatusCode == 403 {
			delay := time.Minute
			if response.StatusCode == 429 {
				if seconds, err := strconv.ParseInt(response.Header.Get("Retry-After"), 10, 64); err == nil && seconds >= 0 {
					// Avoid integer overflow; never send another request sooner than advised.
					if seconds > int64((time.Duration(1<<63-1))/time.Second) {
						seconds = int64((time.Duration(1<<63 - 1)) / time.Second)
					}
					if time.Duration(seconds)*time.Second > delay {
						delay = time.Duration(seconds) * time.Second
					}
				} else if date, err := http.ParseTime(response.Header.Get("Retry-After")); err == nil && time.Until(date) > delay {
					delay = time.Until(date)
				}
			}
			c.mu.Lock()
			c.cooldown = time.Now().Add(delay)
			c.mu.Unlock()
		}
		switch response.StatusCode {
		case 401, 403:
			return nil, "", errors.New("TMDB 凭据无效或无权限，请部署者检查设置")
		case 429:
			return nil, "", errors.New("TMDB 请求受限，已进入冷却期，请稍后重试")
		case 404:
			return nil, "", errors.New("TMDB 未找到该作品")
		default:
			return nil, "", errors.New("TMDB 服务错误；本地浏览与播放不受影响")
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return nil, "", errors.New("TMDB 响应不完整或过大")
	}
	return body, response.Header.Get("Content-Type"), nil
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
	return TitleMetadata{Source: "TMDB", TMDBID: t.ID, Kind: kind, Title: first(t.Title, t.Name, t.OriginalTitle, t.OriginalName), OriginalTitle: first(t.OriginalTitle, t.OriginalName), Year: year, Overview: t.Overview, SourceURL: fmt.Sprintf("https://www.themoviedb.org/%s/%d", tmdbKind(kind), t.ID)}
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
		if err == nil && (strings.Split(typ, ";")[0] == "image/jpeg" || strings.Split(typ, ";")[0] == "image/png" || strings.Split(typ, ";")[0] == "image/webp") {
			poster, contentType = data, strings.Split(typ, ";")[0]
			m.PosterURL = fmt.Sprintf("/api/metadata/posters/%s/%d", kind, id)
		} else {
			warning = first(warning, "海报暂时不可用，使用占位图")
		}
	}
	return m, poster, contentType, warning, nil
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
