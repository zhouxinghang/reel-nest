package http

import (
	"bytes"
	"compress/flate"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"reelnest/internal/model"
)

type episodeDTO struct {
	SeriesID    string `json:"seriesId"`
	SeriesTitle string `json:"seriesTitle"`
	Season      int    `json:"seasonNumber"`
	Episode     int    `json:"episodeNumber"`
}

func card(c model.CatalogCard) any {
	var metadata *summaryDTO
	if c.Match.Title != nil {
		v := summary(c.Match)
		metadata = &v
	}
	favorite := favoriteDTO{c.Favorite, c.FavoritedAt}
	if c.SeriesID != "" {
		return seriesCardDTO{"series", c.SeriesID, c.EntryKey, c.LibraryID, "series", c.Title, c.AddedAt, c.SeasonCount, c.EpisodeCount, c.FileCount, c.AvailableFileCount, favorite, metadata}
	}
	var progress *progressDTO
	if p := c.Progress; p != nil {
		progress = &progressDTO{p.Position, p.Duration, p.Completed, p.UpdatedAt}
	}
	var episode *episodeDTO
	if c.ParentSeriesID != "" {
		episode = &episodeDTO{c.ParentSeriesID, c.ParentSeriesTitle, c.Season, c.Episode}
	}
	return mediaCardDTO{"media", c.MediaFileID, c.EntryKey, c.LibraryID, c.Kind, c.Title, c.Filename, c.State, c.AddedAt, favorite, progress, episode, metadata}
}
func cards(values []model.CatalogCard) []any {
	out := []any{}
	for _, v := range values {
		out = append(out, card(v))
	}
	return out
}

type catalogCursor struct {
	Scope             string
	Position          model.CatalogPosition
	Revision, Expires int64
	Limit             int
}

func catalogQuery(r *http.Request, group bool) (model.CatalogQuery, error) {
	v := r.URL.Query()
	q := model.CatalogQuery{LibraryID: v.Get("libraryId"), Kind: v.Get("kind"), View: v.Get("view"), Sort: v.Get("sort"), Query: v.Get("q")}
	if q.View == "" {
		q.View = "browse"
	}
	if q.Sort == "" {
		q.Sort = "title"
	}
	switch q.View {
	case "recent":
		q.Sort = "recent"
	case "favorites":
		q.Sort = "favorite"
	case "continue":
		q.Sort = "progress"
	}
	if q.View != "favorites" && q.Sort == "favorite" || q.View != "continue" && q.Sort == "progress" {
		return q, model.ErrInvalid
	}
	if group && (q.View == "continue" || q.View == "recent") || !group && q.LibraryID == "" && q.View != "continue" && q.View != "recent" {
		return q, model.ErrInvalid
	}
	return q, nil
}
func catalogScope(kind string, q model.CatalogQuery) string {
	b, _ := json.Marshal(q)
	return kind + ":" + string(b)
}
func (t *Transport) catalogSignature(s string) []byte {
	m := hmac.New(sha256.New, t.cursorKey[:])
	m.Write([]byte("reelnest:catalog-page:v1:" + s))
	return m.Sum(nil)
}
func (t *Transport) catalogCursor(r *http.Request, scope string, limit int) (catalogCursor, error) {
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	c := catalogCursor{Scope: scope, Limit: limit, Expires: time.Now().Add(5 * time.Minute).UnixMilli()}
	if limit < 1 || limit > 100 {
		return c, model.ErrInvalid
	}
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return c, nil
	}
	payload, signature, ok := strings.Cut(raw, ".")
	if !ok {
		return c, model.ErrCursor
	}
	sig, e := base64.RawURLEncoding.DecodeString(signature)
	if e != nil || !hmac.Equal(sig, t.catalogSignature(payload)) {
		return c, model.ErrCursor
	}
	b, e := base64.RawURLEncoding.DecodeString(payload)
	if e != nil {
		return c, model.ErrCursor
	}
	reader := flate.NewReader(bytes.NewReader(b))
	defer reader.Close()
	b, e = io.ReadAll(io.LimitReader(reader, 16385))
	if e != nil || len(b) > 16384 || json.Unmarshal(b, &c) != nil || c.Scope != scope || c.Limit != limit || c.Expires <= time.Now().UnixMilli() {
		return c, model.ErrCursor
	}
	return c, nil
}
func (t *Transport) catalogPage(c catalogCursor) pageInfo {
	b, _ := json.Marshal(c)
	var compressed bytes.Buffer
	writer, _ := flate.NewWriter(&compressed, flate.BestSpeed)
	_, _ = writer.Write(b)
	_ = writer.Close()
	p := base64.RawURLEncoding.EncodeToString(compressed.Bytes())
	raw := p + "." + base64.RawURLEncoding.EncodeToString(t.catalogSignature(p))
	return pageInfo{&raw, &c.Expires}
}
func (t *Transport) catalogEntries(w http.ResponseWriter, r *http.Request) {
	q, e := catalogQuery(r, false)
	if e != nil {
		t.fail(w, e)
		return
	}
	c, e := t.catalogCursor(r, catalogScope("entries", q), 50)
	if e != nil {
		t.fail(w, e)
		return
	}
	p, e := t.store.Catalog(r.Context(), q, c.Position, c.Limit, c.Revision, time.Now().UnixMilli())
	if e != nil {
		t.fail(w, e)
		return
	}
	c.Revision = p.Revision
	c.Expires = min(c.Expires, p.Expires)
	if c.Expires <= time.Now().UnixMilli() {
		t.fail(w, model.ErrCursor)
		return
	}
	page := pageInfo{}
	if p.More {
		c.Position = p.Last
		page = t.catalogPage(c)
	}
	respond(w, 200, struct {
		Items []any    `json:"items"`
		Page  pageInfo `json:"page"`
	}{cards(p.Items), page})
}

type librarySummaryDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func librarySummary(l model.Library) librarySummaryDTO {
	return librarySummaryDTO{l.ID, l.Name, l.Kind}
}
func (t *Transport) catalogGroups(w http.ResponseWriter, r *http.Request) {
	q, e := catalogQuery(r, true)
	if e != nil {
		t.fail(w, e)
		return
	}
	c, e := t.catalogCursor(r, catalogScope("groups", q), 50)
	if e != nil {
		t.fail(w, e)
		return
	}
	p, e := t.store.CatalogGroups(r.Context(), q, c.Position, c.Limit, c.Revision, time.Now().UnixMilli())
	if e != nil {
		t.fail(w, e)
		return
	}
	c.Revision = p.Revision
	c.Expires = min(c.Expires, p.Expires)
	if c.Expires <= time.Now().UnixMilli() {
		t.fail(w, model.ErrCursor)
		return
	}
	type group struct {
		Library       librarySummaryDTO `json:"library"`
		Total         int64             `json:"total"`
		EntriesCursor *string           `json:"entriesCursor"`
	}
	out := struct {
		Items []group  `json:"items"`
		Page  pageInfo `json:"page"`
	}{Items: []group{}}
	for _, g := range p.Items {
		child := q
		child.LibraryID = g.Library.ID
		token := t.catalogPage(catalogCursor{Scope: catalogScope("entries", child), Limit: c.Limit, Revision: c.Revision, Expires: c.Expires})
		out.Items = append(out.Items, group{librarySummary(g.Library), g.Total, token.Next})
	}
	if p.More {
		c.Position = p.Items[len(p.Items)-1].Position
		out.Page = t.catalogPage(c)
	}
	respond(w, 200, out)
}
func (t *Transport) catalogMedia(w http.ResponseWriter, r *http.Request) {
	d, e := t.store.MediaDetail(r.Context(), r.PathValue("id"), time.Now().UnixMilli())
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, struct {
		Card       any      `json:"card"`
		Relative   string   `json:"relativePath"`
		Path       string   `json:"path"`
		Size       int64    `json:"size"`
		ModifiedAt int64    `json:"modifiedAt"`
		Reason     string   `json:"organizationReason"`
		Match      matchDTO `json:"match"`
	}{card(d.Card), d.File.Relative, filepath.Join(d.File.RootPath, d.File.Relative), d.File.Size, d.File.ModifiedAt, d.File.Reason, match(d.Match)})
}
func (t *Transport) catalogSeries(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, e := t.catalogCursor(r, "seasons:"+id, 50)
	if e != nil {
		t.fail(w, e)
		return
	}
	after := -1
	if c.Position.ID != "" {
		after, _ = strconv.Atoi(c.Position.ID)
	}
	d, e := t.store.SeriesDetail(r.Context(), id, after, c.Limit, c.Revision, time.Now().UnixMilli())
	if e != nil {
		t.fail(w, e)
		return
	}
	c.Revision = d.Revision
	c.Expires = min(c.Expires, d.Expires)
	if c.Expires <= time.Now().UnixMilli() {
		t.fail(w, model.ErrCursor)
		return
	}
	type season struct {
		Number   int `json:"number"`
		Episodes int `json:"episodeCount"`
		Files    int `json:"fileCount"`
	}
	out := struct {
		Card    any      `json:"card"`
		Match   matchDTO `json:"match"`
		Seasons []season `json:"seasons"`
		Page    pageInfo `json:"page"`
	}{Card: card(d.Card), Match: match(d.Match), Seasons: []season{}}
	for _, s := range d.Seasons {
		out.Seasons = append(out.Seasons, season{s.Number, s.EpisodeCount, s.FileCount})
	}
	if d.More {
		c.Position.ID = strconv.Itoa(d.Seasons[len(d.Seasons)-1].Number)
		out.Page = t.catalogPage(c)
	}
	respond(w, 200, out)
}
func (t *Transport) catalogEpisodes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	season, _ := strconv.Atoi(r.PathValue("number"))
	c, e := t.catalogCursor(r, "episodes:"+id+":"+strconv.Itoa(season), 50)
	if e != nil {
		t.fail(w, e)
		return
	}
	after, _ := strconv.Atoi(c.Position.Key)
	p, e := t.store.Episodes(r.Context(), id, season, after, c.Position.ID, c.Limit, c.Revision, time.Now().UnixMilli())
	if e != nil {
		t.fail(w, e)
		return
	}
	c.Revision = p.Revision
	c.Expires = min(c.Expires, p.Expires)
	if c.Expires <= time.Now().UnixMilli() {
		t.fail(w, model.ErrCursor)
		return
	}
	type episode struct {
		Number int `json:"episodeNumber"`
		File   any `json:"file"`
	}
	out := struct {
		Items []episode `json:"items"`
		Page  pageInfo  `json:"page"`
	}{Items: []episode{}}
	for _, v := range p.Items {
		out.Items = append(out.Items, episode{v.Episode, card(v)})
	}
	if p.More {
		v := p.Items[len(p.Items)-1]
		c.Position = model.CatalogPosition{Key: strconv.Itoa(v.Episode), ID: v.MediaFileID}
		out.Page = t.catalogPage(c)
	}
	respond(w, 200, out)
}
func (t *Transport) catalogHome(w http.ResponseWriter, r *http.Request) {
	c, e := t.catalogCursor(r, "home", 20)
	if e != nil {
		t.fail(w, e)
		return
	}
	h, e := t.store.Home(r.Context(), c.Position.ID, c.Revision, time.Now().UnixMilli())
	if e != nil {
		t.fail(w, e)
		return
	}
	c.Revision = h.Revision
	c.Expires = min(c.Expires, h.Expires)
	if c.Expires <= time.Now().UnixMilli() {
		t.fail(w, model.ErrCursor)
		return
	}
	type preview struct {
		Library librarySummaryDTO `json:"library"`
		Items   []any             `json:"items"`
	}
	out := struct {
		Libraries []preview `json:"libraries"`
		Page      pageInfo  `json:"librariesPage"`
		Continue  []any     `json:"continueWatching"`
		Recent    []any     `json:"recent"`
	}{Libraries: []preview{}, Continue: cards(h.Continue), Recent: cards(h.Recent)}
	for _, l := range h.Libraries {
		out.Libraries = append(out.Libraries, preview{librarySummary(l.Library), cards(l.Items)})
	}
	if h.More {
		c.Position.ID = h.Libraries[len(h.Libraries)-1].Library.ID
		out.Page = t.catalogPage(c)
	}
	respond(w, 200, out)
}
func (t *Transport) catalogFavorite(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Favorite bool `json:"favorite"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	at, e := t.store.Favorite(r.Context(), r.PathValue("kind"), r.PathValue("id"), input.Favorite)
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, favoriteDTO{at != nil, at})
}
