package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// MetaTube uses an opaque (provider, id) pair, not a numeric catalog ID.
// Persist a local surrogate so existing matching/cache APIs remain compatible;
// never hash, parse as a number, or discard the upstream provider namespace.
type metaTubeClient struct {
	*metadataHTTP
	imageHTTP *metadataHTTP
	db        *sql.DB
	token     string
	enabled   bool
}

func newMetaTube(config Config, db *sql.DB) (*metaTubeClient, error) {
	base := strings.TrimRight(strings.TrimSpace(config.MetaTubeURL), "/")
	enabled := base != ""
	if !enabled {
		base = "https://metatube.invalid" // Disabled clients never contact this origin.
	}
	// Accept either the documented base URL or its /v1 API prefix.
	base = strings.TrimSuffix(base, "/v1")
	timeout := config.MetaTubeTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client, err := newMetadataHTTPWithPolicy("MetaTube", base, base, timeout, true)
	if err != nil {
		return nil, err
	}
	images, err := newMetadataHTTPWithPolicy("MetaTube 图片", base, base, timeout, true)
	if err != nil {
		return nil, err
	}
	return &metaTubeClient{metadataHTTP: client, imageHTTP: images, db: db, token: strings.TrimSpace(config.MetaTubeToken), enabled: enabled}, nil
}

func (c *metaTubeClient) configured() bool { return c.enabled }

func (c *metaTubeClient) json(ctx context.Context, endpoint string, query url.Values, target any, emptyOK bool) error {
	if !c.configured() {
		return errors.New("未配置 MetaTube 服务地址；本地浏览与播放不受影响")
	}
	body, _, err := c.request(ctx, "GET", c.apiURL+"/v1"+endpoint+"?"+query.Encode(), c.token, nil, 1<<20)
	if err != nil {
		return err
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil || (len(envelope.Error) > 0 && string(envelope.Error) != "null") {
		return errors.New("MetaTube 返回无效的资料")
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		if emptyOK && strings.TrimSpace(string(body)) != "null" {
			return nil // Empty data may be omitted by the documented envelope.
		}
		return errors.New("MetaTube 缺少作品资料")
	}
	if json.Unmarshal(envelope.Data, target) != nil {
		return errors.New("MetaTube 返回无效的资料")
	}
	return nil
}

type metaTubeMovie struct {
	ID          string   `json:"id"`
	Provider    string   `json:"provider"`
	Number      string   `json:"number"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary"`
	Homepage    string   `json:"homepage"`
	Director    string   `json:"director"`
	Actors      []string `json:"actors"`
	BigThumbURL string   `json:"big_thumb_url"`
	CoverURL    string   `json:"cover_url"`
	BigCoverURL string   `json:"big_cover_url"`
	ReleaseDate string   `json:"release_date"`
}

func validMetaTubePart(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 1024 && value != "." && value != ".." && !strings.ContainsFunc(value, unicode.IsControl)
}

func (c *metaTubeClient) localID(ctx context.Context, provider, remoteID string) (int64, error) {
	_, err := c.db.ExecContext(ctx, `INSERT INTO metatube_titles(provider,remote_id) VALUES(?,?) ON CONFLICT(provider,remote_id) DO NOTHING`, strings.ToLower(provider), remoteID)
	var id int64
	if err == nil {
		err = c.db.QueryRowContext(ctx, `SELECT id FROM metatube_titles WHERE provider=? AND remote_id=?`, strings.ToLower(provider), remoteID).Scan(&id)
	}
	if err != nil {
		return 0, errors.New("无法保存 MetaTube 作品标识")
	}
	return id, nil
}

func (c *metaTubeClient) remoteID(ctx context.Context, id int64) (string, string, error) {
	var provider, remoteID string
	err := c.db.QueryRowContext(ctx, `SELECT provider,remote_id FROM metatube_titles WHERE id=?`, id).Scan(&provider, &remoteID)
	if err != nil || !validMetaTubePart(provider) || !validMetaTubePart(remoteID) {
		return "", "", errors.New("MetaTube 作品标识无效，请重新搜索候选")
	}
	return provider, remoteID, nil
}

func metaTubeMetadata(movie metaTubeMovie, id int64) TitleMetadata {
	m := TitleMetadata{Source: "MetaTube", SourceID: id, Kind: "movie", Title: strings.TrimSpace(movie.Title), OriginalTitle: strings.TrimSpace(movie.Title), Number: movie.Number, Provider: movie.Provider, Overview: movie.Summary}
	if u, err := url.Parse(movie.Homepage); err == nil && u.Host != "" && u.User == nil && (u.Scheme == "https" || u.Scheme == "http") {
		m.SourceURL = u.String()
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if date, err := time.Parse(layout, movie.ReleaseDate); err == nil && date.Year() > 1 {
			m.Year = strconv.Itoa(date.Year())
			break
		}
	}
	return m
}

func (c *metaTubeClient) search(ctx context.Context, kind, query string) ([]TitleMetadata, bool, error) {
	items := []TitleMetadata{}
	if kind != "movie" {
		return items, true, errors.New("MetaTube 仅支持电影资料，不支持电视剧季集")
	}
	var movies []metaTubeMovie
	err := c.json(ctx, "/movies/search", url.Values{"q": {query}, "fallback": {"true"}}, &movies, true)
	if errors.Is(err, errMetadataNotFound) {
		return items, true, nil // Search's documented empty-result response.
	}
	if err != nil {
		return nil, false, err
	}
	seen := map[string]bool{}
	complete := true
	for _, movie := range movies {
		if !validMetaTubePart(movie.Provider) || !validMetaTubePart(movie.ID) || strings.TrimSpace(movie.Title) == "" {
			complete = false // A discarded row cannot prove a unique automatic match.
			continue
		}
		key := strings.ToLower(movie.Provider) + "\x00" + movie.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		if len(items) >= 20 {
			complete = false
			break
		}
		id, err := c.localID(ctx, movie.Provider, movie.ID)
		if err != nil {
			return nil, false, err
		}
		items = append(items, metaTubeMetadata(movie, id))
	}
	return items, complete, nil
}

func (c *metaTubeClient) movie(ctx context.Context, kind string, id int64) (metaTubeMovie, error) {
	var movie metaTubeMovie
	if kind != "movie" {
		return movie, errors.New("MetaTube 仅支持电影资料，不支持电视剧季集")
	}
	provider, remoteID, err := c.remoteID(ctx, id)
	if err != nil {
		return movie, err
	}
	err = c.json(ctx, "/movies/"+url.PathEscape(provider)+"/"+url.PathEscape(remoteID), url.Values{"lazy": {"true"}}, &movie, false)
	if err != nil {
		return movie, err
	}
	if movie.ID != remoteID || !strings.EqualFold(movie.Provider, provider) || strings.TrimSpace(movie.Title) == "" {
		return movie, errors.New("MetaTube 返回的作品标识不符或缺少标题")
	}
	return movie, nil
}

func metaTubeCredits(movie metaTubeMovie) TitleCredits {
	entries := []creditEntry{{CreditPerson: CreditPerson{Name: movie.Director}, Director: true}}
	for _, name := range movie.Actors {
		entries = append(entries, creditEntry{CreditPerson: CreditPerson{Name: name}})
	}
	// MovieInfo has names only. Never infer actor IDs/photos from a name search.
	credits := normalizeCredits(entries)
	credits.Version, credits.Status, credits.FetchedAt = 1, "available", time.Now().Unix()
	return credits
}

func metaTubeArtwork(movie metaTubeMovie, id int64) TitleArtwork {
	art := TitleArtwork{Version: 1, Status: "available", FetchedAt: time.Now().Unix()}
	if first(movie.BigCoverURL, movie.CoverURL) != "" {
		// Store only our local ID, not upstream image URLs. Download exclusively
		// through the configured service's documented image endpoints.
		art.ThumbPath, art.BackdropPath = strconv.FormatInt(id, 10), strconv.FormatInt(id, 10)
	}
	return art
}

func (c *metaTubeClient) details(ctx context.Context, kind string, id int64) (TitleMetadata, []byte, string, string, error) {
	movie, err := c.movie(ctx, kind, id)
	if err != nil {
		return TitleMetadata{}, nil, "", "", err
	}
	m := metaTubeMetadata(movie, id)
	m.FetchedAt, m.ExpiresAt = time.Now().Unix(), time.Now().Add(30*24*time.Hour).Unix()
	credits, art := metaTubeCredits(movie), metaTubeArtwork(movie, id)
	m.Credits, m.Artwork = &credits, &art
	if first(movie.BigThumbURL, movie.CoverURL) == "" {
		return m, nil, "", "", nil
	}
	poster, typ, err := c.downloadImage(ctx, id, "primary")
	typ = strings.Split(typ, ";")[0]
	if err != nil || !validImage(poster, typ) {
		return m, nil, "", "MetaTube 海报暂时不可用，使用占位图", nil
	}
	m.PosterURL = fmt.Sprintf("/api/metadata/posters/metatube/movie/%d", id)
	return m, poster, typ, "", nil
}

func (c *metaTubeClient) credits(ctx context.Context, kind string, id int64) (TitleCredits, error) {
	movie, err := c.movie(ctx, kind, id)
	return metaTubeCredits(movie), err
}

func (c *metaTubeClient) artwork(ctx context.Context, kind string, id int64) (TitleArtwork, error) {
	movie, err := c.movie(ctx, kind, id)
	return metaTubeArtwork(movie, id), err
}

func (c *metaTubeClient) downloadImage(ctx context.Context, id int64, imageType string) ([]byte, string, error) {
	if !c.configured() {
		return nil, "", errors.New("未配置 MetaTube 服务地址")
	}
	provider, remoteID, err := c.remoteID(ctx, id)
	if err != nil {
		return nil, "", err
	}
	endpoint := c.apiURL + "/v1/images/" + imageType + "/" + url.PathEscape(provider) + "/" + url.PathEscape(remoteID)
	return c.imageHTTP.request(ctx, "GET", endpoint, "", nil, 5<<20)
}

func (c *metaTubeClient) image(ctx context.Context, ref, imageType string) ([]byte, string, error) {
	id, err := strconv.ParseInt(ref, 10, 64)
	if err != nil || id <= 0 || (imageType != "thumb" && imageType != "backdrop") {
		return nil, "", errors.New("MetaTube 图片标识或类型无效")
	}
	return c.downloadImage(ctx, id, imageType)
}

func (c *metaTubeClient) photo(context.Context, string) ([]byte, string, error) {
	return nil, "", errors.New("MetaTube 影片资料未提供可验证的人物照片标识")
}
