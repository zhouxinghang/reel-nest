package http

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"reelnest/internal/model"
	"strconv"
	"strings"
	"time"
)

type remoteDTO struct {
	Source           string `json:"source"`
	CatalogID        string `json:"catalogId"`
	Namespace        string `json:"namespace"`
	Kind             string `json:"kind"`
	RemoteID         string `json:"remoteId"`
	ProviderRevision int64  `json:"providerRevision"`
}

func remote(r model.RemoteRef) remoteDTO {
	return remoteDTO{r.Source, r.CatalogID, r.Namespace, r.Kind, r.RemoteID, r.ProviderRevision}
}
func (r remoteDTO) value() model.RemoteRef {
	return model.RemoteRef{Source: r.Source, CatalogID: r.CatalogID, Namespace: r.Namespace, Kind: r.Kind, RemoteID: r.RemoteID, ProviderRevision: r.ProviderRevision}
}

type artworkDTO struct {
	Primary  *string `json:"primary"`
	Thumb    *string `json:"thumb"`
	Backdrop *string `json:"backdrop"`
}

func stringPointer(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
func artwork(a map[string]string) artworkDTO {
	return artworkDTO{stringPointer(a["primary"]), stringPointer(a["thumb"]), stringPointer(a["backdrop"])}
}

type summaryDTO struct {
	Title         string     `json:"title"`
	OriginalTitle string     `json:"originalTitle"`
	Year          *int       `json:"year"`
	Source        string     `json:"source"`
	Artwork       artworkDTO `json:"artwork"`
	ExpiresAt     int64      `json:"expiresAt"`
}

func summary(m model.Match) summaryDTO {
	t := m.Title
	return summaryDTO{t.Title, t.OriginalTitle, t.Year, t.Ref.Source, artwork(m.Artwork), t.ExpiresAt}
}

type creditDTO struct {
	PersonID   string   `json:"personId"`
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	Characters []string `json:"characters"`
	Photo      *string  `json:"photoAssetId"`
}
type creditsDTO struct {
	State     string      `json:"state"`
	Actors    []creditDTO `json:"actors"`
	Directors []creditDTO `json:"directors"`
	RetryAt   *int64      `json:"retryAt"`
}
type detailDTO struct {
	Summary        summaryDTO `json:"summary"`
	Remote         remoteDTO  `json:"remote"`
	Identifier     string     `json:"identifier"`
	Overview       string     `json:"overview"`
	SourceURL      string     `json:"sourceUrl"`
	FetchedAt      int64      `json:"fetchedAt"`
	Revision       int64      `json:"revision"`
	Credits        creditsDTO `json:"credits"`
	ArtworkState   string     `json:"artworkState"`
	ArtworkRetryAt *int64     `json:"artworkRetryAt"`
}
type selectionDTO struct {
	Remote   remoteDTO `json:"remote"`
	Mode     string    `json:"mode"`
	Revision int64     `json:"revision"`
}
type matchDTO struct {
	SubjectID       string        `json:"subjectId"`
	SubjectRevision int64         `json:"subjectRevision"`
	BindingRevision int64         `json:"bindingRevision"`
	Selection       *selectionDTO `json:"selection"`
	CacheState      string        `json:"cacheState"`
	AttemptState    string        `json:"attemptState"`
	AttemptedAt     *int64        `json:"attemptedAt"`
	ErrorCode       *string       `json:"errorCode"`
	Metadata        *detailDTO    `json:"metadata"`
}

func match(m model.Match) matchDTO {
	result := matchDTO{SubjectID: m.Target.ID, SubjectRevision: m.Target.SubjectRevision, BindingRevision: m.Target.BindingRevision, CacheState: m.CacheState, AttemptState: m.AttemptState, AttemptedAt: m.AttemptedAt, ErrorCode: m.ErrorCode}
	if m.Target.Selection != "" {
		result.Selection = &selectionDTO{remote(m.Target.Ref), m.Target.Selection, m.Target.BindingRevision}
	}
	if m.Title != nil {
		t := m.Title
		c := creditsDTO{t.CreditsState, []creditDTO{}, []creditDTO{}, t.CreditsRetryAt}
		for _, v := range m.Credits {
			x := creditDTO{v.PersonID, v.Name, v.Role, v.Characters, v.PhotoAssetID}
			if v.Role == "actor" {
				c.Actors = append(c.Actors, x)
			} else {
				c.Directors = append(c.Directors, x)
			}
		}
		result.Metadata = &detailDTO{summary(m), remote(t.Ref), t.Identifier, t.Overview, t.SourceURL, t.FetchedAt, t.Revision, c, t.ArtworkState, t.ArtworkRetryAt}
	}
	return result
}

type candidateDTO struct {
	Ref           remoteDTO `json:"ref"`
	Title         string    `json:"title"`
	OriginalTitle string    `json:"originalTitle"`
	Year          *int      `json:"year"`
	Overview      string    `json:"overview"`
}

func (t *Transport) metadataCandidates(w http.ResponseWriter, r *http.Request) {
	target, page, e := t.metadata.Candidates(r.Context(), r.PathValue("id"), r.URL.Query().Get("source"), r.URL.Query().Get("q"))
	if e != nil {
		t.fail(w, e)
		return
	}
	result := struct {
		Items           []candidateDTO `json:"items"`
		Complete        bool           `json:"complete"`
		SubjectRevision int64          `json:"subjectRevision"`
		BindingRevision int64          `json:"bindingRevision"`
	}{[]candidateDTO{}, page.Complete, target.SubjectRevision, target.BindingRevision}
	for _, c := range page.Items {
		result.Items = append(result.Items, candidateDTO{remote(c.Ref), c.Title, c.OriginalTitle, c.Year, c.Overview})
	}
	respond(w, 200, result)
}
func (t *Transport) metadataSelection(w http.ResponseWriter, r *http.Request) {
	var input struct {
		SubjectRevision  int64     `json:"subjectRevision"`
		ExpectedRevision int64     `json:"expectedRevision"`
		Ref              remoteDTO `json:"ref"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	m, e := t.metadata.Select(r.Context(), r.PathValue("id"), input.SubjectRevision, input.ExpectedRevision, input.Ref.value())
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, match(m))
}
func (t *Transport) metadataRefresh(w http.ResponseWriter, r *http.Request) {
	var input struct {
		LibraryID string `json:"libraryId"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		t.fail(w, model.ErrInvalid)
		return
	}
	n, e := t.metadata.Refresh(r.Context(), input.LibraryID)
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 202, struct {
		Queued int64 `json:"queued"`
	}{n})
}

type metadataCursor struct {
	Scope, After      string
	Revision, Expires int64
	Limit             int
}

func (t *Transport) metadataSignature(s string) []byte {
	m := hmac.New(sha256.New, t.cursorKey[:])
	m.Write([]byte("reelnest:metadata-page:v1:" + s))
	return m.Sum(nil)
}
func (t *Transport) metadataCursor(r *http.Request, scope string) (metadataCursor, error) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	c := metadataCursor{Scope: scope, Limit: limit, Expires: time.Now().Add(5 * time.Minute).UnixMilli()}
	if limit < 1 || limit > 100 {
		return c, model.ErrInvalid
	}
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return c, nil
	}
	p, sig, ok := strings.Cut(raw, ".")
	if !ok {
		return c, model.ErrCursor
	}
	b, e := base64.RawURLEncoding.DecodeString(sig)
	if e != nil || !hmac.Equal(b, t.metadataSignature(p)) {
		return c, model.ErrCursor
	}
	b, e = base64.RawURLEncoding.DecodeString(p)
	if e != nil || json.Unmarshal(b, &c) != nil || c.Scope != scope || c.Limit != limit || c.Expires <= time.Now().UnixMilli() {
		return c, model.ErrCursor
	}
	return c, nil
}
func (t *Transport) metadataPage(c metadataCursor) pageInfo {
	b, _ := json.Marshal(c)
	p := base64.RawURLEncoding.EncodeToString(b)
	raw := p + "." + base64.RawURLEncoding.EncodeToString(t.metadataSignature(p))
	return pageInfo{&raw, &c.Expires}
}

type jobDTO struct {
	SubjectID           string  `json:"subjectId"`
	State               string  `json:"state"`
	RequestedGeneration int64   `json:"requestedGeneration"`
	ClaimedGeneration   *int64  `json:"claimedGeneration"`
	Attempts            int     `json:"attempts"`
	RequestedAt         int64   `json:"requestedAt"`
	StartedAt           *int64  `json:"startedAt"`
	FinishedAt          *int64  `json:"finishedAt"`
	NextAttemptAt       *int64  `json:"nextAttemptAt"`
	ErrorCode           *string `json:"errorCode"`
}

func (t *Transport) metadataStatus(w http.ResponseWriter, r *http.Request) {
	library := r.URL.Query().Get("libraryId")
	c, e := t.metadataCursor(r, "status:"+library)
	if e != nil {
		t.fail(w, e)
		return
	}
	status, e := t.store.MetadataStatus(r.Context(), library, c.After, c.Limit+1, c.Revision)
	if e != nil {
		t.fail(w, e)
		return
	}
	result := struct {
		Sources    []providerStatus `json:"sources"`
		Queued     int64            `json:"queued"`
		Running    int64            `json:"running"`
		Succeeded  int64            `json:"succeeded"`
		Failed     int64            `json:"failed"`
		Superseded int64            `json:"superseded"`
		Revision   int64            `json:"revision"`
		Failures   []jobDTO         `json:"failures"`
		Page       pageInfo         `json:"page"`
	}{Sources: []providerStatus{}, Failures: []jobDTO{}, Queued: status.Counts["queued"], Running: status.Counts["running"], Succeeded: status.Counts["succeeded"], Failed: status.Counts["failed"], Superseded: status.Counts["superseded"], Revision: status.Revision}
	settings, e := t.metadata.Settings()
	if e != nil {
		t.fail(w, e)
		return
	}
	for _, p := range settings {
		result.Sources = append(result.Sources, providerStatus{p.Source(), providerName(p.Source()), p.Configured(), t.metadata.Cooldown(p.Source())})
	}
	if len(status.Failures) > c.Limit {
		status.Failures = status.Failures[:c.Limit]
		c.After = status.Failures[len(status.Failures)-1].SubjectID
		c.Revision = status.Revision
		result.Page = t.metadataPage(c)
	}
	for _, j := range status.Failures {
		result.Failures = append(result.Failures, jobDTO{j.SubjectID, j.State, j.RequestedGeneration, j.ClaimedGeneration, j.Attempts, j.RequestedAt, j.StartedAt, j.FinishedAt, j.NextAttemptAt, j.ErrorCode})
	}
	respond(w, 200, result)
}
func (t *Transport) metadataPerson(w http.ResponseWriter, r *http.Request) {
	p, e := t.store.MetadataPerson(r.Context(), r.PathValue("id"), time.Now().UnixMilli())
	if e != nil {
		t.fail(w, e)
		return
	}
	respond(w, 200, struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		Source    string  `json:"source"`
		CatalogID string  `json:"catalogId"`
		Photo     *string `json:"photoAssetId"`
	}{p.ID, p.Name, p.Source, p.CatalogID, p.PhotoAssetID})
}
func (t *Transport) metadataAsset(w http.ResponseWriter, r *http.Request) {
	a, e := t.metadata.Asset(r.Context(), r.PathValue("id"))
	if e != nil {
		t.fail(w, e)
		return
	}
	w.Header().Set("Content-Type", a.MIME)
	w.Header().Set("Content-Length", strconv.Itoa(len(a.Bytes)))
	w.WriteHeader(200)
	if r.Method != "HEAD" {
		_, _ = w.Write(a.Bytes)
	}
}

type favoriteDTO struct {
	Favorite    bool   `json:"favorite"`
	FavoritedAt *int64 `json:"favoritedAt"`
}
type progressDTO struct {
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
	Completed bool    `json:"completed"`
	UpdatedAt int64   `json:"updatedAt"`
}
type mediaCardDTO struct {
	EntryType string       `json:"entryType"`
	ID        string       `json:"id"`
	EntryKey  string       `json:"entryKey"`
	LibraryID string       `json:"libraryId"`
	Kind      string       `json:"kind"`
	Title     string       `json:"title"`
	Filename  string       `json:"filename"`
	State     string       `json:"state"`
	AddedAt   int64        `json:"addedAt"`
	Favorite  favoriteDTO  `json:"favorite"`
	Progress  *progressDTO `json:"progress"`
	Episode   *struct{}    `json:"episode"`
	Metadata  summaryDTO   `json:"metadata"`
}
type seriesCardDTO struct {
	EntryType          string      `json:"entryType"`
	ID                 string      `json:"id"`
	EntryKey           string      `json:"entryKey"`
	LibraryID          string      `json:"libraryId"`
	Kind               string      `json:"kind"`
	Title              string      `json:"title"`
	AddedAt            int64       `json:"addedAt"`
	SeasonCount        int         `json:"seasonCount"`
	EpisodeCount       int         `json:"episodeCount"`
	FileCount          int         `json:"fileCount"`
	AvailableFileCount int         `json:"availableFileCount"`
	Favorite           favoriteDTO `json:"favorite"`
	Metadata           summaryDTO  `json:"metadata"`
}
type personWorkDTO struct {
	Card       any      `json:"card"`
	Roles      []string `json:"roles"`
	Characters []string `json:"characters"`
}

func (t *Transport) metadataPersonWorks(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, e := t.metadataCursor(r, "person:"+id)
	if e != nil {
		t.fail(w, e)
		return
	}
	items, revision, expiry, e := t.store.MetadataPersonWorks(r.Context(), id, c.After, c.Limit+1, c.Revision, time.Now().UnixMilli())
	if e != nil {
		t.fail(w, e)
		return
	}
	result := struct {
		Items []personWorkDTO `json:"items"`
		Page  pageInfo        `json:"page"`
	}{Items: []personWorkDTO{}}
	if len(items) > c.Limit {
		items = items[:c.Limit]
		c.After = items[len(items)-1].RemoteTitleID
		c.Revision = revision
		c.Expires = min(c.Expires, expiry)
		result.Page = t.metadataPage(c)
	}
	for _, v := range items {
		var card any
		if v.SeriesID != "" {
			card = seriesCardDTO{"series", v.SeriesID, v.EntryKey, v.LibraryID, "series", v.Title, v.AddedAt, v.SeasonCount, v.EpisodeCount, v.FileCount, v.AvailableFileCount, favoriteDTO{v.Favorite, v.FavoritedAt}, summary(v.Match)}
		} else {
			var progress *progressDTO
			if v.Progress != nil {
				p := v.Progress
				progress = &progressDTO{p.Position, p.Duration, p.Completed, p.UpdatedAt}
			}
			card = mediaCardDTO{"media", v.MediaFileID, v.EntryKey, v.LibraryID, v.Kind, v.Title, v.Filename, v.State, v.AddedAt, favoriteDTO{v.Favorite, v.FavoritedAt}, progress, nil, summary(v.Match)}
		}
		result.Items = append(result.Items, personWorkDTO{card, v.Roles, v.Characters})
	}
	respond(w, 200, result)
}
