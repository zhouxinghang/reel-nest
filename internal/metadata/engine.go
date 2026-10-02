package metadata

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"reelnest/internal/metadata/providers"
	"reelnest/internal/model"
	"reelnest/internal/store"
)

type Options struct{ Transport http.RoundTripper }
type engine struct {
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	wg          sync.WaitGroup
	started     bool
	transport   http.RoundTripper
	adapters    [3]providers.Adapter
	revisions   [3]int64
	api, images [3]providers.Gate
	flights     map[string]*flight
	global      chan struct{}
	perSource   [3]chan struct{}
}
type flight struct {
	done chan struct{}
	err  error
}

func (m *Service) initEngine(o Options) {
	e := &engine{transport: o.Transport, flights: map[string]*flight{}, global: make(chan struct{}, 4)}
	e.ctx, e.cancel = context.WithCancel(context.Background())
	for i := range e.perSource {
		e.perSource[i] = make(chan struct{}, 2)
	}
	m.engine = e
}
func NewWithOptions(ctx context.Context, s *store.Store, o Options) (*Service, error) {
	m, e := New(ctx, s)
	if e == nil {
		m.engine.transport = o.Transport
	}
	return m, e
}
func (m *Service) adapter(p ProviderSnapshot) providers.Adapter {
	e := m.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	i := sourceIndex(p.Source())
	if e.adapters[i] == nil || e.revisions[i] != p.Revision() {
		e.adapters[i] = providers.New(p, e.transport, &e.api[i], &e.images[i])
		e.revisions[i] = p.Revision()
	}
	return e.adapters[i]
}
func providerVersions(settings []ProviderSnapshot) []model.ProviderVersion {
	out := make([]model.ProviderVersion, 0, len(settings))
	for _, p := range settings {
		out = append(out, model.ProviderVersion{Source: p.Source(), CatalogID: p.CatalogID(), Revision: p.Revision()})
	}
	return out
}

// Start is explicit so isolated settings consumers do not start background work.
// Runtime starts exactly two workers after all configuration has been validated.
func (m *Service) Start() error {
	e := m.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if m.closed.Load() {
		return model.ErrClosed
	}
	if e.started {
		return nil
	}
	if err := m.store.RecoverMetadata(e.ctx); err != nil {
		return err
	}
	e.started = true
	for i := 0; i < 2; i++ {
		e.wg.Add(1)
		go func() { defer e.wg.Done(); m.worker() }()
	}
	return nil
}
func (m *Service) worker() {
	e := m.engine
	timer := time.NewTicker(200 * time.Millisecond)
	defer timer.Stop()
	for {
		if e.ctx.Err() != nil {
			return
		}
		settings, err := m.Settings()
		if err != nil {
			return
		}
		t, err := m.store.ClaimMetadata(e.ctx, providerVersions(settings), time.Now().UnixMilli())
		if err == nil {
			m.run(t, settings)
			continue
		}
		select {
		case <-e.ctx.Done():
			return
		case <-timer.C:
		}
	}
}
func (m *Service) run(t model.MetadataTarget, settings []ProviderSnapshot) {
	ctx, cancel := context.WithTimeout(m.engine.ctx, 65*time.Second)
	defer cancel()
	result := model.MetadataResult{Target: t, ErrorCode: "METADATA_UNAVAILABLE", RetryAt: time.Now().Add(time.Minute).UnixMilli()}
	order := t.Sources
	if t.Selection == "manual" {
		order = []string{t.Ref.Source}
	}
	for _, source := range order {
		p := settings[sourceIndex(source)]
		if !p.Configured() || (t.Kind == "series" && source == "metatube") {
			continue
		}
		if t.Selection == "manual" && p.CatalogID() != t.Ref.CatalogID {
			continue
		}
		providerCtx, stop := context.WithTimeout(ctx, 20*time.Second)
		a := m.adapter(p)
		ref := t.Ref
		ref.ProviderRevision = p.Revision()
		var err error
		if t.Selection != "manual" {
			q, y := matchQuery(t.Query)
			var candidates model.SearchResult
			candidates, err = a.Search(providerCtx, t.Kind, q)
			if err == nil {
				ref, err = unique(candidates, q, y)
			}
		}
		if err == nil {
			var title model.Title
			title, err = m.fetch(providerCtx, a, ref)
			if err == nil {
				result.Title = &title
				result.ErrorCode = ""
				result.Selection = t.Selection
				if result.Selection == "" {
					result.Selection = "automatic"
				}
				result.RetryAt = optionalRetry(title)
				stop()
				break
			}
		}
		stop()
		if ctx.Err() != nil {
			break
		}
		if until := m.engine.api[sourceIndex(source)].Until(); until != nil && *until > result.RetryAt {
			result.RetryAt = *until
		}
	}
	if m.engine.ctx.Err() != nil {
		return
	} // recovery retains consumed attempt on shutdown
	commitCtx, stop := context.WithTimeout(m.engine.ctx, 5*time.Second)
	defer stop()
	_ = m.complete(commitCtx, result)
}
func optionalRetry(t model.Title) int64 {
	var n int64
	for _, v := range []*int64{t.CreditsRetryAt, t.ArtworkRetryAt} {
		if v != nil && (n == 0 || *v < n) {
			n = *v
		}
	}
	return n
}
func due(state string, retry *int64, now int64) bool {
	return state == "missing" || (state == "failed" && (retry == nil || *retry <= now))
}
func (m *Service) fetch(ctx context.Context, a providers.Adapter, ref model.RemoteRef) (model.Title, error) {
	now := time.Now().UnixMilli()
	cached, err := m.store.CachedTitle(ctx, ref, now)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return model.Title{}, err
	}
	parts := providers.Parts{Basic: err != nil, Credits: true, Artwork: true}
	if err == nil {
		parts.Credits = due(cached.CreditsState, cached.CreditsRetryAt, now)
		parts.Artwork = due(cached.ArtworkState, cached.ArtworkRetryAt, now)
		if !parts.Credits && !parts.Artwork {
			return cached, nil
		}
	}
	title, err := a.FetchTitle(ctx, ref, parts)
	if parts.Basic {
		return title, err
	}
	if err != nil {
		title = cached
		retry := time.Now().Add(24 * time.Hour).UnixMilli()
		if parts.Credits {
			title.CreditsState = "failed"
			title.CreditsRetryAt = &retry
		}
		if parts.Artwork {
			title.ArtworkState = "failed"
			title.ArtworkRetryAt = &retry
		}
	}
	title.Partial = true
	title.UpdateCredits = parts.Credits
	title.UpdateArtwork = parts.Artwork
	title.Revision = cached.Revision
	title.FetchedAt = cached.FetchedAt
	title.ExpiresAt = cached.ExpiresAt
	return title, nil
}

var queryYear = regexp.MustCompile(`^(.*?)\s*\(((?:19|20)[0-9]{2})\)$`)

func matchQuery(s string) (string, int) {
	s = filepath.Base(s)
	switch strings.ToLower(filepath.Ext(s)) {
	case ".mp4", ".m4v", ".mkv", ".mov", ".webm", ".avi", ".ts", ".m2ts", ".mpg", ".mpeg", ".wmv", ".flv":
		s = strings.TrimSuffix(s, filepath.Ext(s))
	}
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	if parts := queryYear.FindStringSubmatch(s); parts != nil {
		n, _ := strconv.Atoi(parts[2])
		return strings.TrimSpace(parts[1]), n
	}
	return strings.TrimSpace(s), 0
}
func normalized(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
func unique(result model.SearchResult, q string, y int) (model.RemoteRef, error) {
	var ref model.RemoteRef
	n := 0
	if !result.Complete {
		return ref, model.ErrMetadata
	}
	q = normalized(q)
	if q == "" {
		return ref, model.ErrMetadata
	}
	for _, c := range result.Items {
		if (q == normalized(c.Title) || q == normalized(c.OriginalTitle) || q == normalized(c.Identifier)) && (y == 0 || (c.Year != nil && *c.Year == y)) {
			ref = c.Ref
			n++
		}
	}
	if n != 1 {
		return model.RemoteRef{}, model.ErrMetadata
	}
	return ref, nil
}
func (m *Service) operation(ctx context.Context) (context.Context, func(), error) {
	e := m.engine
	e.mu.Lock()
	defer e.mu.Unlock()
	if m.closed.Load() {
		return nil, nil, model.ErrClosed
	}
	e.wg.Add(1)
	op, cancel := context.WithTimeout(ctx, 65*time.Second)
	stop := context.AfterFunc(e.ctx, cancel)
	return op, func() { stop(); cancel(); e.wg.Done() }, nil
}
func (m *Service) Candidates(ctx context.Context, id, source, query string) (model.MetadataTarget, model.SearchResult, error) {
	ctx, done, err := m.operation(ctx)
	if err != nil {
		return model.MetadataTarget{}, model.SearchResult{}, err
	}
	defer done()
	t, err := m.store.MetadataTarget(ctx, id)
	if err != nil {
		return t, model.SearchResult{}, err
	}
	result := model.SearchResult{Items: []model.Candidate{}, Complete: true}
	order := t.Sources
	if source != "" {
		if sourceIndex(source) < 0 {
			return t, result, model.ErrInvalid
		}
		order = []string{source}
	}
	if query == "" {
		query, _ = matchQuery(t.Query)
	}
	if len([]rune(query)) > 512 || strings.TrimSpace(query) == "" {
		return t, result, model.ErrInvalid
	}
	for _, source := range order {
		p, err := m.Snapshot(source)
		if err != nil {
			return t, result, err
		}
		if !p.Configured() || (t.Kind == "series" && source == "metatube") {
			result.Complete = false
			continue
		}
		pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		r, err := m.adapter(p).Search(pctx, t.Kind, query)
		cancel()
		if err != nil {
			result.Complete = false
			continue
		}
		result.Complete = result.Complete && r.Complete
		for _, c := range r.Items {
			if len(result.Items) < 100 {
				result.Items = append(result.Items, c)
			} else {
				result.Complete = false
			}
		}
	}
	return t, result, nil
}
func (m *Service) Select(ctx context.Context, id string, subject, binding int64, ref model.RemoteRef) (model.Match, error) {
	ctx, done, err := m.operation(ctx)
	if err != nil {
		return model.Match{}, err
	}
	defer done()
	if !providers.ValidRef(ref) {
		return model.Match{}, model.ErrInvalid
	}
	p, err := m.Snapshot(ref.Source)
	if err != nil {
		return model.Match{}, err
	}
	if !p.Configured() || p.Revision() != ref.ProviderRevision || p.CatalogID() != ref.CatalogID {
		return model.Match{}, model.ErrCandidate
	}
	t, err := m.store.BeginSelection(ctx, id, subject, binding, providerVersions([]ProviderSnapshot{p}), ref)
	if err != nil {
		return model.Match{}, err
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	title, fetchErr := m.fetch(pctx, m.adapter(p), ref)
	cancel()
	result := model.MetadataResult{Target: t, Selection: "manual", Title: &title, Rebind: true, RetryAt: optionalRetry(title)}
	if fetchErr != nil {
		result.Title = nil
		result.ErrorCode = "METADATA_UNAVAILABLE"
	}
	// Persist cancellation/failure only if this generation is still current.
	commitCtx, stop := context.WithTimeout(m.engine.ctx, 5*time.Second)
	defer stop()
	if err = m.complete(commitCtx, result); err != nil {
		return model.Match{}, err
	}
	if fetchErr != nil {
		return model.Match{}, model.ErrMetadata
	}
	return m.store.MetadataMatch(ctx, id, time.Now().UnixMilli())
}
func (m *Service) Refresh(ctx context.Context, library string) (int64, error) {
	if m.closed.Load() {
		return 0, model.ErrClosed
	}
	return m.store.RefreshMetadata(ctx, library, time.Now().UnixMilli())
}
func (m *Service) Cooldown(source string) *int64 {
	if i := sourceIndex(source); i >= 0 {
		return m.engine.api[i].Until()
	}
	return nil
}

// A transactional content failure must not strand a running generation. If the
// database itself is unavailable the bounded recovery path remains restartable.
func (m *Service) complete(ctx context.Context, result model.MetadataResult) error {
	err := m.store.CompleteMetadata(ctx, result, time.Now().UnixMilli())
	if err != nil && !errors.Is(err, model.ErrConflict) && ctx.Err() == nil {
		_ = m.store.CompleteMetadata(ctx, model.MetadataResult{Target: result.Target, ErrorCode: "DATABASE_UNAVAILABLE", RetryAt: time.Now().Add(time.Minute).UnixMilli()}, time.Now().UnixMilli())
	}
	outcome := "succeeded"
	if result.ErrorCode != "" {
		outcome = "failed"
	}
	if err != nil {
		outcome = "commit_failed"
	}
	if errors.Is(err, model.ErrConflict) {
		outcome = "superseded"
	}
	slog.Info("metadata job finished", "subject_id", result.Target.ID, "generation", result.Target.Generation, "outcome", outcome)
	return err
}
