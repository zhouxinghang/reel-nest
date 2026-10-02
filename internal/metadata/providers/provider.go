// Package providers implements upstream protocols only. It cannot access the
// database and never generates local identities or local HTTP routes.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"reelnest/internal/model"
)

type Config interface {
	Source() string
	CatalogID() string
	BaseURL() string
	Credential() string
	PIN() string
	Revision() int64
	Configured() bool
}
type Capabilities struct{ Movies, Series, ReliablePeople bool }
type Parts struct{ Basic, Credits, Artwork bool }
type Adapter interface {
	Capabilities() Capabilities
	Search(context.Context, string, string) (model.SearchResult, error)
	FetchTitle(context.Context, model.RemoteRef, Parts) (model.Title, error)
	FetchImage(context.Context, string, string) ([]byte, string, error)
	SearchActors(context.Context, string) (model.ActorSearch, error)
	FetchActor(context.Context, model.ActorRef) (model.ActorProfile, error)
	FetchActorImage(context.Context, model.ActorRef, string, bool) ([]byte, string, error)
}

// Gate reserves request start times, not whole network operations. Each source
// owns independent API and image gates; configuration rotation cannot bypass it.
type Gate struct {
	mu             sync.Mutex
	next, cooldown time.Time
}

func (g *Gate) Until() *int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.cooldown.After(time.Now()) {
		return nil
	}
	n := g.cooldown.UnixMilli()
	return &n
}
func (g *Gate) wait(ctx context.Context) error {
	g.mu.Lock()
	now := time.Now()
	if g.cooldown.After(now) {
		g.mu.Unlock()
		return model.ErrProvider
	}
	at := g.next
	if at.Before(now) {
		at = now
	}
	g.next = at.Add(250 * time.Millisecond)
	g.mu.Unlock()
	timer := time.NewTimer(time.Until(at))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	g.mu.Lock()
	cooling := g.cooldown.After(time.Now())
	g.mu.Unlock()
	if cooling {
		return model.ErrProvider
	}
	return nil
}
func (g *Gate) fail(h string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	until := time.Now().Add(time.Minute)
	if n, e := strconv.ParseInt(h, 10, 64); e == nil && n > 0 && n < 31536000 {
		until = time.Now().Add(time.Duration(n) * time.Second)
	} else if d, e := http.ParseTime(h); e == nil && d.After(until) {
		until = d
	}
	if until.Before(time.Now().Add(time.Minute)) {
		until = time.Now().Add(time.Minute)
	}
	if until.After(g.cooldown) {
		g.cooldown = until
	}
}

type client struct {
	config      Config
	http        *http.Client
	api, images *Gate
	tokenMu     sync.Mutex
	token       string
	tokenUntil  time.Time
}

func New(c Config, transport http.RoundTripper, api, images *Gate) Adapter {
	if transport == nil {
		transport = http.DefaultTransport
	}
	if api == nil {
		api = &Gate{}
	}
	if images == nil {
		images = &Gate{}
	}
	return &client{config: c, http: &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, api: api, images: images}
}
func (c *client) Capabilities() Capabilities {
	return Capabilities{true, c.config.Source() != "metatube", c.config.Source() != "metatube"}
}
func (c *client) ref(kind, id, ns string) model.RemoteRef {
	return model.RemoteRef{Source: c.config.Source(), CatalogID: c.config.CatalogID(), Namespace: ns, Kind: kind, RemoteID: id, ProviderRevision: c.config.Revision()}
}

var digits = regexp.MustCompile(`^[1-9][0-9]*$`)
var namespace = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)

func ValidRef(r model.RemoteRef) bool {
	if r.CatalogID == "" || r.ProviderRevision < 1 || (r.Kind != "movie" && r.Kind != "series") || strings.TrimSpace(r.RemoteID) == "" || len(r.RemoteID) > 1024 || !utf8.ValidString(r.RemoteID) || strings.ContainsFunc(r.RemoteID, unicode.IsControl) {
		return false
	}
	switch r.Source {
	case "tmdb", "tvdb":
		return r.Namespace == r.Source && digits.MatchString(r.RemoteID)
	case "metatube":
		return r.Kind == "movie" && namespace.MatchString(r.Namespace) && r.RemoteID != "." && r.RemoteID != ".."
	}
	return false
}
func (c *client) validate(r model.RemoteRef) bool {
	return ValidRef(r) && r.Source == c.config.Source() && r.CatalogID == c.config.CatalogID() && r.ProviderRevision == c.config.Revision()
}
func (c *client) request(ctx context.Context, method, endpoint string, body []byte, token string, gate *Gate, max int64) ([]byte, http.Header, int, error) {
	if e := gate.wait(ctx); e != nil {
		return nil, nil, 0, e
	}
	req, e := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if e != nil {
		return nil, nil, 0, model.ErrProvider
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := c.http.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, nil, 0, ctx.Err()
		}
		return nil, nil, 0, model.ErrProvider
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 || resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode >= 500 {
		gate.fail(resp.Header.Get("Retry-After"))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header, resp.StatusCode, model.ErrProvider
	}
	if resp.ContentLength > max {
		return nil, nil, resp.StatusCode, model.ErrProvider
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if e != nil || int64(len(b)) > max {
		if ctx.Err() != nil {
			return nil, nil, 0, ctx.Err()
		}
		return nil, nil, 0, model.ErrProvider
	}
	return b, resp.Header, resp.StatusCode, nil
}
func (c *client) json(ctx context.Context, path string, out any) (int, error) {
	token := c.config.Credential()
	if c.config.Source() == "tvdb" {
		var e error
		token, e = c.login(ctx)
		if e != nil {
			return 0, e
		}
	}
	b, _, status, e := c.request(ctx, "GET", c.config.BaseURL()+path, nil, token, c.api, 1<<20)
	if status == 401 && c.config.Source() == "tvdb" {
		c.tokenMu.Lock()
		c.token = ""
		c.tokenMu.Unlock()
	}
	if e != nil {
		return status, e
	}
	if json.Unmarshal(b, out) != nil {
		return status, model.ErrProvider
	}
	return status, nil
}
func (c *client) login(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token != "" && time.Now().Before(c.tokenUntil) {
		return c.token, nil
	}
	b, _ := json.Marshal(struct {
		APIKey string `json:"apikey"`
		PIN    string `json:"pin,omitempty"`
	}{c.config.Credential(), c.config.PIN()})
	data, _, _, e := c.request(ctx, "POST", c.config.BaseURL()+"/login", b, "", c.api, 1<<20)
	if e != nil {
		return "", e
	}
	var reply struct {
		Status string `json:"status"`
		Data   struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if json.Unmarshal(data, &reply) != nil || reply.Status != "success" || reply.Data.Token == "" {
		return "", model.ErrProvider
	}
	c.token = reply.Data.Token
	c.tokenUntil = time.Now().Add(24 * time.Hour)
	return c.token, nil
}
func (c *client) Search(ctx context.Context, kind, q string) (model.SearchResult, error) {
	if !c.config.Configured() || (kind != "movie" && kind != "series") || (kind == "series" && !c.Capabilities().Series) {
		return model.SearchResult{}, model.ErrProvider
	}
	switch c.config.Source() {
	case "tmdb":
		return c.tmdbSearch(ctx, kind, q)
	case "tvdb":
		return c.tvdbSearch(ctx, kind, q)
	default:
		return c.metaSearch(ctx, q)
	}
}
func (c *client) FetchTitle(ctx context.Context, r model.RemoteRef, p Parts) (model.Title, error) {
	if !c.config.Configured() || !c.validate(r) {
		return model.Title{}, model.ErrCandidate
	}
	var t model.Title
	var e error
	switch c.config.Source() {
	case "tmdb":
		t, e = c.tmdbTitle(ctx, r, p)
	case "tvdb":
		t, e = c.tvdbTitle(ctx, r, p)
	default:
		t, e = c.metaTitle(ctx, r, p)
	}
	if e != nil {
		return t, e
	}
	t.Ref = r
	t.Partial = !p.Basic
	t.UpdateCredits = p.Credits
	t.UpdateArtwork = p.Artwork
	if p.Basic && strings.TrimSpace(t.Title) == "" {
		return t, model.ErrMetadata
	}
	t.Title = clip(t.Title, 512)
	t.OriginalTitle = clip(t.OriginalTitle, 512)
	t.Identifier = clip(t.Identifier, 512)
	t.Overview = clip(t.Overview, 32768)
	t.SourceURL = safePage(t.SourceURL)
	now := time.Now()
	t.FetchedAt = now.UnixMilli()
	t.ExpiresAt = now.Add(30 * 24 * time.Hour).UnixMilli()
	retry := now.Add(24 * time.Hour).UnixMilli()
	if t.CreditsState == "failed" {
		t.CreditsRetryAt = &retry
	}
	if t.ArtworkState == "failed" {
		t.ArtworkRetryAt = &retry
	}
	return t, nil
}
func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
func first(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func year(s string) *int {
	if len(s) < 4 {
		return nil
	}
	n, e := strconv.Atoi(s[:4])
	if e != nil || n < 1 || n > 9999 {
		return nil
	}
	return &n
}
func safePage(s string) string {
	u, e := url.Parse(s)
	if e != nil || len(s) > 2048 || u.User != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return s
}
func addCandidate(result *model.SearchResult, item model.Candidate) {
	if !ValidRef(item.Ref) || strings.TrimSpace(item.Title) == "" {
		result.Complete = false
		return
	}
	for _, x := range result.Items {
		if x.Ref == item.Ref {
			result.Complete = false
			return
		}
	}
	if len(result.Items) >= 100 {
		result.Complete = false
		return
	}
	item.Title = clip(item.Title, 512)
	item.OriginalTitle = clip(item.OriginalTitle, 512)
	item.Overview = clip(item.Overview, 4096)
	result.Items = append(result.Items, item)
}

var imagePath = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpg|jpeg|png|webp)$`)

func tmdbImage(s string) string {
	if imagePath.MatchString(s) {
		return s
	}
	return ""
}
func tvdbImage(s string) string {
	u, e := url.Parse(s)
	if e != nil || u.Scheme != "https" || u.Host != "artworks.thetvdb.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.RawPath != "" || !strings.HasPrefix(u.Path, "/banners/") {
		return ""
	}
	for _, p := range strings.Split(u.Path, "/") {
		if p == "." || p == ".." || strings.ContainsAny(p, "\\%") {
			return ""
		}
	}
	return s
}
func (c *client) FetchImage(ctx context.Context, kind, reference string) ([]byte, string, error) {
	var endpoint string
	switch c.config.Source() {
	case "tmdb":
		path := tmdbImage(reference)
		size := map[string]string{"primary": "w500", "photo": "w185", "thumb": "w780", "backdrop": "w1280"}[kind]
		if path != "" && size != "" {
			endpoint = "https://image.tmdb.org/t/p/" + size + path
		}
	case "tvdb":
		endpoint = tvdbImage(reference)
	case "metatube":
		// The reference is a structured identity, never an upstream-returned URL.
		var r model.RemoteRef
		decodeErr := json.Unmarshal([]byte(reference), &r)
		r.ProviderRevision = c.config.Revision()
		if decodeErr == nil && c.validate(r) && (kind == "primary" || kind == "thumb" || kind == "backdrop") {
			endpoint = c.config.BaseURL() + "/images/" + kind + "/" + url.PathEscape(r.Namespace) + "/" + url.PathEscape(r.RemoteID)
		}
	}
	if endpoint == "" {
		return nil, "", model.ErrAsset
	}
	return c.fetchImage(ctx, endpoint)
}
func (c *client) fetchImage(ctx context.Context, endpoint string) ([]byte, string, error) {
	b, h, _, e := c.request(ctx, "GET", endpoint, nil, "", c.images, 5<<20)
	if e != nil {
		return nil, "", e
	}
	mimeType, _, e := mime.ParseMediaType(h.Get("Content-Type"))
	if e != nil {
		return nil, "", model.ErrAsset
	}
	detected := http.DetectContentType(b)
	if mimeType != detected || (mimeType != "image/jpeg" && mimeType != "image/png" && mimeType != "image/webp" && mimeType != "image/gif") {
		return nil, "", model.ErrAsset
	}
	return b, mimeType, nil
}
func optional(err error) string {
	if err == nil {
		return "ready"
	}
	return "failed"
}
func isCanceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
