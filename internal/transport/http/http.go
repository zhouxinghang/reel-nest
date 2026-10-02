// Package http provides explicit contract DTOs and authentication adapters.
// This assembly is not exposed by cmd/reelnest until the remaining routes and
// UI land. It mounts identity/library/scan, metadata, people and asset routes.
package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"reelnest/api"
	"reelnest/internal/catalog"
	"reelnest/internal/identity"
	"reelnest/internal/mediafs"
	"reelnest/internal/metadata"
	"reelnest/internal/model"
	"reelnest/internal/store"
)

type Transport struct {
	store     *store.Store
	identity  *identity.Identity
	catalog   *catalog.Catalog
	metadata  *metadata.Service
	contract  *api.Contract
	roots     []string
	secure    bool
	cursorKey [32]byte
}

func New(s *store.Store, i *identity.Identity, c *catalog.Catalog, m *metadata.Service, roots []string, secure bool) (http.Handler, error) {
	contract, e := api.Load()
	if e != nil {
		return nil, e
	}
	t := &Transport{store: s, identity: i, catalog: c, metadata: m, contract: contract, roots: append([]string{}, roots...), secure: secure, cursorKey: s.Keys().Stream}
	mux := http.NewServeMux()
	register := func(pattern string, auth bool, handler http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if auth {
				if _, e := i.Session(r.Context(), cookie(r)); e != nil {
					t.fail(w, e)
					return
				}
			}
			if e := contract.ValidateRequest(r.Context(), r); e != nil {
				t.fail(w, model.ErrInvalid)
				return
			}
			handler(w, r)
		})
	}
	register("GET /healthz", false, func(w http.ResponseWriter, r *http.Request) { respond(w, 200, health{"ok"}) })
	register("GET /readyz", false, func(w http.ResponseWriter, r *http.Request) {
		if c.Ready() && s.Ready(r.Context()) {
			respond(w, 200, health{"ok"})
		} else {
			respond(w, 503, health{"not_ready"})
		}
	})
	register("POST /api/login", false, t.login)
	register("POST /api/logout", true, t.logout)
	register("GET /api/session", true, func(w http.ResponseWriter, r *http.Request) {
		name, e := i.Session(r.Context(), cookie(r))
		if e != nil {
			t.fail(w, e)
			return
		}
		respond(w, 200, session{name})
	})
	register("GET /api/libraries", true, t.libraries)
	register("GET /api/settings/metadata", true, t.metadataSettings)
	register("GET /api/metadata/subjects/{id}/candidates", true, t.metadataCandidates)
	register("PUT /api/metadata/subjects/{id}/selection", true, t.metadataSelection)
	register("POST /api/metadata/refresh", true, t.metadataRefresh)
	register("GET /api/metadata/status", true, t.metadataStatus)
	register("GET /api/people/{id}", true, t.metadataPerson)
	register("GET /api/people/{id}/works", true, t.metadataPersonWorks)
	register("GET /api/assets/{id}/content", true, t.metadataAsset)
	register("PATCH /api/settings/metadata/{source}", true, t.updateMetadataSettings)
	register("POST /api/libraries", true, t.createLibrary)
	register("PATCH /api/libraries/{id}", true, t.updateLibrary)
	register("POST /api/libraries/{id}/scans", true, func(w http.ResponseWriter, r *http.Request) {
		scan, e := c.StartScan(r.Context(), r.PathValue("id"))
		if e != nil {
			t.fail(w, e)
			return
		}
		respond(w, 202, scanDTO(scan))
	})
	register("GET /api/scans/{id}", true, func(w http.ResponseWriter, r *http.Request) {
		scan, e := s.Scan(r.Context(), r.PathValue("id"))
		if e != nil {
			t.fail(w, e)
			return
		}
		respond(w, 200, scanDTO(scan))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { t.fail(w, model.ErrNotFound) })
	return t.sameOrigin(mux), nil
}
func cookie(r *http.Request) string {
	c, e := r.Cookie(identity.CookieName)
	if e != nil {
		return ""
	}
	return c.Value
}
func respond(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type health struct {
	Status string `json:"status"`
}
type session struct {
	Username string `json:"username"`
}
type okResponse struct {
	OK bool `json:"ok"`
}
type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	RequestID string `json:"requestId"`
}
type errorResponse struct {
	Error errorBody `json:"error"`
}

func (t *Transport) fail(w http.ResponseWriter, e error) {
	code, status, message := "DATABASE_UNAVAILABLE", 500, "操作失败，请稍后重试"
	switch {
	case errors.Is(e, identity.ErrUnauthorized):
		code, status, message = "UNAUTHENTICATED", 401, "请使用家庭共享账号登录"
	case errors.Is(e, identity.ErrLimited):
		code, status, message = "RATE_LIMITED", 429, "登录过于频繁，请稍后重试"
		w.Header().Set("Retry-After", "300")
	case errors.Is(e, model.ErrCandidate):
		code, status, message = "CANDIDATE_STALE", 409, "候选已失效，请重新搜索"
	case errors.Is(e, model.ErrProvider):
		code, status, message = "PROVIDER_UNAVAILABLE", 503, "资料源暂不可用"
	case errors.Is(e, model.ErrMetadata):
		code, status, message = "METADATA_UNAVAILABLE", 503, "资料暂不可用，已有选择保留"
	case errors.Is(e, model.ErrAsset):
		code, status, message = "ASSET_UNAVAILABLE", 503, "图片暂不可用"
	case errors.Is(e, model.ErrInvalid):
		code, status, message = "INVALID_REQUEST", 400, "请求内容无效"
	case errors.Is(e, model.ErrNotFound):
		code, status, message = "NOT_FOUND", 404, "资源不存在"
	case errors.Is(e, model.ErrOverlap):
		code, status, message = "LIBRARY_OVERLAP", 409, "媒体目录不能重复或重叠"
	case errors.Is(e, model.ErrConflict):
		code, status, message = "REVISION_CONFLICT", 409, "配置已变化，请重新加载"
	case errors.Is(e, model.ErrBusy):
		code, status, message = "LIBRARY_BUSY", 409, "该媒体库正在扫描或修改"
	case errors.Is(e, model.ErrClosed):
		code, status, message = "NOT_READY", 503, "应用正在停止"
	case errors.Is(e, model.ErrCursor):
		code, status, message = "CURSOR_STALE", 409, "分页已失效，请重新加载"
	case errors.Is(e, mediafs.ErrPath), errors.Is(e, mediafs.ErrChanged):
		code, status, message = "INVALID_MEDIA_PATH", 400, "媒体目录路径或身份无效"
	default:
		var path *os.PathError
		if errors.As(e, &path) {
			code, status, message = "INVALID_MEDIA_PATH", 400, "媒体目录不可访问"
		}
	}
	respond(w, status, errorResponse{errorBody{code, message, status >= 500, model.NewID()}})
}
func (t *Transport) sameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS" {
			invalid := r.Header.Get("Sec-Fetch-Site") == "cross-site"
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				scheme := "http"
				if t.secure || r.TLS != nil {
					scheme = "https"
				}
				invalid = invalid || e != nil || u.Scheme != scheme || u.Host != r.Host || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery
			}
			if invalid {
				respond(w, 403, errorResponse{errorBody{"FORBIDDEN", "拒绝跨站请求", false, model.NewID()}})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (t *Transport) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	token, expiry, e := t.identity.Login(r.Context(), input.Username, input.Password, cookie(r))
	if e != nil {
		t.fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: identity.CookieName, Value: token, Path: "/", HttpOnly: true, Secure: t.secure, SameSite: http.SameSiteStrictMode, Expires: expiry, MaxAge: int(identity.Lifetime.Seconds())})
	respond(w, 200, session{input.Username})
}
func (t *Transport) logout(w http.ResponseWriter, r *http.Request) {
	if e := t.identity.Logout(r.Context(), cookie(r)); e != nil {
		t.fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: identity.CookieName, Path: "/", HttpOnly: true, Secure: t.secure, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
	respond(w, 200, okResponse{true})
}

type libraryDTO struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Path           string   `json:"path"`
	Kind           string   `json:"kind"`
	Revision       int64    `json:"revision"`
	PolicyRevision int64    `json:"policyRevision"`
	Sources        []string `json:"sources"`
	CreatedAt      int64    `json:"createdAt"`
	LatestScanID   *string  `json:"latestScanId"`
}

func library(l model.Library) libraryDTO {
	return libraryDTO{l.ID, l.Name, l.Path, l.Kind, l.Revision, l.PolicyRevision, l.Sources, l.CreatedAt, l.LatestScanID}
}

type scanResponse struct {
	ID              string  `json:"id"`
	LibraryID       string  `json:"libraryId"`
	State           string  `json:"state"`
	RequestedAt     int64   `json:"requestedAt"`
	StartedAt       *int64  `json:"startedAt"`
	FinishedAt      *int64  `json:"finishedAt"`
	DiscoveredCount int64   `json:"discoveredCount"`
	ErrorCode       *string `json:"errorCode"`
}

func scanDTO(s model.Scan) scanResponse {
	return scanResponse{s.ID, s.LibraryID, s.State, s.RequestedAt, s.StartedAt, s.FinishedAt, s.DiscoveredCount, s.ErrorCode}
}
func (t *Transport) createLibrary(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name    string   `json:"name"`
		Path    string   `json:"path"`
		Kind    string   `json:"kind"`
		Sources []string `json:"sources"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	l, e := t.catalog.Create(r.Context(), input.Name, input.Path, input.Kind, input.Sources)
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 201, library(l))
}
func (t *Transport) updateLibrary(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int64     `json:"revision"`
		Name     *string   `json:"name"`
		Sources  *[]string `json:"sources"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	l, e := t.catalog.Update(r.Context(), r.PathValue("id"), input.Revision, input.Name, input.Sources)
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, library(l))
}

// Library cursors are scoped, authenticated and expire within five minutes.
// No metadata TTL applies to this configuration-only listing.
type libraryCursor struct {
	After             string
	Revision, Expires int64
	Limit             int
}

func (t *Transport) sign(payload string) []byte {
	mac := hmac.New(sha256.New, t.cursorKey[:])
	mac.Write([]byte("reelnest:libraries:v1:" + payload))
	return mac.Sum(nil)
}
func (t *Transport) encodeCursor(c libraryCursor) string {
	b, _ := json.Marshal(c)
	p := base64.RawURLEncoding.EncodeToString(b)
	return p + "." + base64.RawURLEncoding.EncodeToString(t.sign(p))
}
func (t *Transport) decodeCursor(raw string, limit int) (libraryCursor, error) {
	var c libraryCursor
	p, sig, ok := strings.Cut(raw, ".")
	if !ok {
		return c, model.ErrCursor
	}
	b, e := base64.RawURLEncoding.DecodeString(sig)
	if e != nil || !hmac.Equal(b, t.sign(p)) {
		return c, model.ErrCursor
	}
	b, e = base64.RawURLEncoding.DecodeString(p)
	if e != nil || json.Unmarshal(b, &c) != nil || c.Limit != limit || c.Expires <= time.Now().UnixMilli() {
		return c, model.ErrCursor
	}
	return c, nil
}

type pageInfo struct {
	Next    *string `json:"nextCursor"`
	Expires *int64  `json:"expiresAt"`
}
type providerStatus struct {
	Source     string `json:"source"`
	Name       string `json:"name"`
	Configured bool   `json:"configured"`
	Cooldown   *int64 `json:"cooldownUntil"`
}
type libraryPage struct {
	Items   []libraryDTO     `json:"items"`
	Page    pageInfo         `json:"page"`
	Roots   []string         `json:"allowedRoots"`
	Sources []providerStatus `json:"sources"`
}

func (t *Transport) libraries(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	cursor := libraryCursor{Limit: limit, Expires: time.Now().Add(5 * time.Minute).UnixMilli()}
	var e error
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, e = t.decodeCursor(raw, limit)
		if e != nil {
			t.fail(w, e)
			return
		}
	}
	items, rev, e := t.store.Libraries(r.Context(), cursor.After, limit+1, cursor.Revision)
	if e != nil {
		t.fail(w, e)
		return
	}
	result := libraryPage{Items: []libraryDTO{}, Roots: t.roots, Sources: []providerStatus{}}
	if len(items) > limit {
		items = items[:limit]
		cursor.After = items[len(items)-1].ID
		cursor.Revision = rev
		raw := t.encodeCursor(cursor)
		result.Page = pageInfo{&raw, &cursor.Expires}
	}
	for _, l := range items {
		result.Items = append(result.Items, library(l))
	}
	settings, e := t.metadata.Settings()
	if e != nil {
		t.fail(w, e)
		return
	}
	for _, p := range settings {
		result.Sources = append(result.Sources, providerStatus{Source: p.Source(), Name: providerName(p.Source()), Configured: p.Configured()})
	}
	respond(w, 200, result)
}
