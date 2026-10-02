package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TVDB v4 uses an API key (+ optional subscriber PIN) to obtain a monthly JWT.
// Renew conservatively once a day; a rejected token is discarded, never retried
// in a loop. Only the server holds credentials, including the JWT.
type tvdbClient struct {
	*metadataHTTP
	apiKey, pin  string
	authMu       sync.Mutex
	token        string
	tokenExpires time.Time
	photoHTTP    *metadataHTTP
	imageHTTP    *metadataHTTP
	artworkMu    sync.Mutex
	artworkTypes map[int64]string
}

func newTVDB(config Config) (*tvdbClient, error) {
	apiURL, imageURL := config.TVDBAPIURL, config.TVDBImageURL
	if apiURL == "" {
		apiURL = "https://api4.thetvdb.com/v4"
	}
	if imageURL == "" {
		imageURL = "https://artworks.thetvdb.com"
	}
	client, err := newMetadataHTTP("TVDB", apiURL, imageURL, config.TVDBTimeout)
	if err != nil {
		return nil, err
	}
	photoHTTP, err := newMetadataHTTP("TVDB 人物照片", apiURL, imageURL, config.TVDBTimeout)
	if err != nil {
		return nil, err
	}
	imageHTTP, err := newMetadataHTTP("TVDB 影片图片", apiURL, imageURL, config.TVDBTimeout)
	if err != nil {
		return nil, err
	}
	return &tvdbClient{metadataHTTP: client, apiKey: strings.TrimSpace(config.TVDBAPIKey), pin: strings.TrimSpace(config.TVDBPIN), photoHTTP: photoHTTP, imageHTTP: imageHTTP}, nil
}
func (c *tvdbClient) configured() bool { return c.apiKey != "" }

func (c *tvdbClient) authenticate(ctx context.Context) (string, error) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	if !c.configured() {
		return "", errors.New("未配置 TVDB 凭据；本地浏览与播放不受影响")
	}
	if c.token != "" && time.Now().Before(c.tokenExpires) {
		return c.token, nil
	}
	credentials := map[string]string{"apikey": c.apiKey}
	if c.pin != "" {
		credentials["pin"] = c.pin
	}
	body, _ := json.Marshal(credentials)
	data, _, err := c.request(ctx, "POST", c.apiURL+"/login", "", bytes.NewReader(body), 1<<20)
	if err != nil {
		return "", err
	}
	var result struct {
		Status string `json:"status"`
		Data   struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &result) != nil || result.Status != "success" || strings.TrimSpace(result.Data.Token) == "" {
		return "", errors.New("TVDB 返回无效的登录结果")
	}
	c.token, c.tokenExpires = result.Data.Token, time.Now().Add(24*time.Hour)
	return c.token, nil
}

func (c *tvdbClient) json(ctx context.Context, endpoint string, query url.Values, target any) error {
	token, err := c.authenticate(ctx)
	if err != nil {
		return err
	}
	body, _, err := c.request(ctx, "GET", c.apiURL+endpoint+"?"+query.Encode(), token, nil, 1<<20)
	if err != nil {
		if errors.Is(err, errMetadataUnauthorized) {
			c.authMu.Lock()
			if c.token == token {
				c.token = ""
			}
			c.authMu.Unlock()
		}
		return err
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Status != "success" || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("TVDB 返回无效的资料")
	}
	// Decode the envelope too, since search needs its pagination links.
	if json.Unmarshal(body, target) != nil {
		return errors.New("TVDB 返回无效的资料")
	}
	return nil
}

func tvdbKind(kind string) string {
	if kind == "series" {
		return "series"
	}
	return "movies"
}
func tvdbMetadata(kind string, id int64, slug string) TitleMetadata {
	sourceURL := fmt.Sprintf("https://www.thetvdb.com/dereferrer/%s/%d", kind, id)
	if slug != "" {
		sourceURL = "https://www.thetvdb.com/" + tvdbKind(kind) + "/" + url.PathEscape(slug)
	}
	return TitleMetadata{Source: "TVDB", SourceID: id, Kind: kind, SourceURL: sourceURL}
}

func (c *tvdbClient) search(ctx context.Context, kind, query string) ([]TitleMetadata, bool, error) {
	var result struct {
		Data []struct {
			ID           string            `json:"tvdb_id"`
			Type         string            `json:"type"`
			Name         string            `json:"name"`
			Year         string            `json:"year"`
			Slug         string            `json:"slug"`
			Overview     string            `json:"overview"`
			Translations map[string]string `json:"translations"`
			Overviews    map[string]string `json:"overviews"`
		} `json:"data"`
		Links struct {
			Next json.RawMessage `json:"next"`
		} `json:"links"`
	}
	// language filters the original language on TVDB; it must NOT be set to zho,
	// otherwise non-Chinese productions disappear from search results.
	err := c.json(ctx, "/search", url.Values{"query": {query}, "type": {kind}, "limit": {"21"}, "offset": {"0"}}, &result)
	if err != nil {
		return nil, false, err
	}
	candidates := []TitleMetadata{}
	seen := map[int64]bool{}
	for _, title := range result.Data {
		id, err := strconv.ParseInt(title.ID, 10, 64)
		if err != nil || id <= 0 || title.Type != kind || seen[id] {
			continue
		}
		seen[id] = true
		m := tvdbMetadata(kind, id, title.Slug)
		m.Title = first(title.Translations["zho"], title.Translations["eng"], title.Name)
		m.OriginalTitle, m.Year = title.Name, title.Year
		m.Overview = first(title.Overviews["zho"], title.Overviews["eng"], title.Overview)
		if m.Title != "" && len(candidates) < 20 {
			candidates = append(candidates, m)
		}
	}
	next := string(result.Links.Next)
	return candidates, len(result.Data) <= 20 && (next == "" || next == "null" || next == `""`), nil
}

type tvdbTranslation struct{ Language, Name, Overview string }

func translated(values []tvdbTranslation, overview bool) string {
	for _, language := range []string{"zho", "eng"} {
		for _, value := range values {
			if value.Language != language {
				continue
			}
			text := value.Name
			if overview {
				text = value.Overview
			}
			if strings.TrimSpace(text) != "" {
				return text
			}
		}
	}
	return ""
}

func (c *tvdbClient) details(ctx context.Context, kind string, id int64) (TitleMetadata, []byte, string, string, error) {
	var result struct {
		Data struct {
			ID                                int64 `json:"id"`
			Name, Slug, Year, Overview, Image string
			FirstAired                        string `json:"firstAired"`
			Translations                      struct {
				Names     []tvdbTranslation `json:"nameTranslations"`
				Overviews []tvdbTranslation `json:"overviewTranslations"`
			} `json:"translations"`
		} `json:"data"`
	}
	err := c.json(ctx, fmt.Sprintf("/%s/%d/extended", tvdbKind(kind), id), url.Values{"meta": {"translations"}, "short": {"true"}}, &result)
	if err != nil {
		return TitleMetadata{}, nil, "", "", err
	}
	title := result.Data
	if title.ID != id {
		return TitleMetadata{}, nil, "", "", errors.New("TVDB 返回的作品标识不符")
	}
	m := tvdbMetadata(kind, id, title.Slug)
	m.Title = first(translated(title.Translations.Names, false), title.Name)
	m.OriginalTitle, m.Year = title.Name, title.Year
	if m.Year == "" && len(title.FirstAired) >= 4 {
		m.Year = title.FirstAired[:4]
	}
	m.Overview = first(translated(title.Translations.Overviews, true), title.Overview)
	if m.Title == "" {
		return m, nil, "", "", errors.New("TVDB 缺少可用作品标题")
	}
	m.FetchedAt, m.ExpiresAt = time.Now().Unix(), time.Now().Add(30*24*time.Hour).Unix()
	var poster []byte
	typ, warning := "", ""
	if title.Image != "" {
		endpoint := c.artworkURL(title.Image)
		if endpoint != "" {
			data, contentType, err := c.request(ctx, "GET", endpoint, "", nil, 5<<20)
			contentType = strings.Split(contentType, ";")[0]
			if err == nil && validImage(data, contentType) {
				poster, typ = data, contentType
				m.PosterURL = fmt.Sprintf("/api/metadata/posters/tvdb/%s/%d", kind, id)
			}
		}
		if len(poster) == 0 {
			warning = "TVDB 海报暂时不可用，使用占位图"
		}
	}
	return m, poster, typ, warning, nil
}

func (c *tvdbClient) credits(ctx context.Context, kind string, id int64) (TitleCredits, error) {
	var result struct {
		Data struct {
			ID         int64 `json:"id"`
			Characters []struct {
				PeopleID     int64  `json:"peopleId"`
				PersonName   string `json:"personName"`
				PersonImgURL string `json:"personImgURL"`
				Name         string `json:"name"`
				PeopleType   string `json:"peopleType"`
				Sort         int64  `json:"sort"`
			} `json:"characters"`
		} `json:"data"`
	}
	// The short record deliberately omits characters. Fetch the full record
	// separately (without translations), within the same 1 MiB/request budget;
	// failure must not throw away the already-fetched title and poster.
	err := c.json(ctx, fmt.Sprintf("/%s/%d/extended", tvdbKind(kind), id), url.Values{"short": {"false"}}, &result)
	if err != nil {
		return TitleCredits{}, err
	}
	if result.Data.ID != id {
		return TitleCredits{}, errors.New("TVDB 返回的演职员作品标识不符")
	}
	characters := result.Data.Characters
	sort.SliceStable(characters, func(i, j int) bool { return characters[i].Sort < characters[j].Sort })
	entries := []creditEntry{}
	for _, person := range characters {
		kind := strings.ToLower(strings.TrimSpace(person.PeopleType))
		if kind != "actor" && kind != "director" {
			continue
		}
		// name is the fictional character, personName is the real person. Do
		// not guess a profession from undocumented numeric type values.
		// The character's image can be a fictional role; only use personImgURL.
		photo := ""
		if len(person.PersonImgURL) <= 2048 {
			photo = c.artworkURL(person.PersonImgURL)
		}
		if strings.TrimSpace(person.PersonImgURL) == "" {
			photo = ""
		}
		entries = append(entries, creditEntry{CreditPerson: CreditPerson{ID: person.PeopleID, Name: person.PersonName, PhotoPath: photo}, Roles: []string{person.Name}, Director: kind == "director"})
	}
	return normalizeCredits(entries), nil
}

func (c *tvdbClient) photo(ctx context.Context, ref string) ([]byte, string, error) {
	endpoint := c.artworkURL(ref)
	if endpoint == "" || ref == "" {
		return nil, "", errors.New("人物照片地址无效")
	}
	return c.photoHTTP.request(ctx, "GET", endpoint, "", nil, 5<<20)
}

// Only the configured artwork origin is trusted. Never fetch arbitrary URLs
// from provider data (nor forward an API token to artwork servers).
func (c *tvdbClient) artworkURL(value string) string {
	base, _ := url.Parse(c.imageURL)
	u, err := url.Parse(value)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	if u.IsAbs() || u.Host != "" {
		if u.Scheme != base.Scheme || u.Host != base.Host {
			return ""
		}
	} else {
		u = base.ResolveReference(&url.URL{Path: strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(u.Path, "/")})
	}
	if path.Clean(u.Path) != u.Path || !strings.HasPrefix(u.Path, strings.TrimRight(base.Path, "/")+"/") {
		return ""
	}
	return u.String()
}
