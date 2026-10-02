// Package catalog owns library configuration, conservative organization, bounded
// scan workers and per-library mutation leases. It performs no upstream I/O.
package catalog

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"reelnest/internal/mediafs"
	"reelnest/internal/model"
	"reelnest/internal/store"
)

type Catalog struct {
	store     *store.Store
	fs        *mediafs.FS
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	closed    bool
	busy      map[string]bool
	lastErr   error
	wg        sync.WaitGroup
	wake      chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func New(ctx context.Context, s *store.Store, fs *mediafs.FS) (*Catalog, error) {
	if e := s.InterruptScans(ctx); e != nil {
		return nil, e
	}
	life, cancel := context.WithCancel(ctx)
	c := &Catalog{store: s, fs: fs, ctx: life, cancel: cancel, busy: map[string]bool{}, wake: make(chan struct{}, 2)}
	c.wg.Add(2)
	for n := 0; n < 2; n++ {
		go c.worker()
	}
	return c, nil
}
func (c *Catalog) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed && c.ctx.Err() == nil && c.lastErr == nil
}
func (c *Catalog) begin(ctx context.Context) (context.Context, func(), error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.ctx.Err() != nil {
		return nil, nil, model.ErrClosed
	}
	c.wg.Add(1)
	joined, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	return joined, func() { stop(); cancel(); c.wg.Done() }, nil
}
func (c *Catalog) notify() {
	for n := 0; n < 2; n++ {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}
func validName(name string) bool {
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 80 && strings.IndexFunc(name, unicode.IsControl) < 0
}
func validSources(s []string) bool {
	if len(s) > 3 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range s {
		if seen[v] || v != "tmdb" && v != "tvdb" && v != "metatube" {
			return false
		}
		seen[v] = true
	}
	return true
}
func (c *Catalog) Create(ctx context.Context, name, path, kind string, sources []string) (model.Library, error) {
	ctx, done, e := c.begin(ctx)
	if e != nil {
		return model.Library{}, e
	}
	defer done()
	name = strings.TrimSpace(name)
	if !validName(name) || kind != "movie" && kind != "series" || !validSources(sources) {
		return model.Library{}, model.ErrInvalid
	}
	if sources == nil {
		sources = []string{"tmdb", "tvdb"}
	}
	root, e := c.fs.Inspect(path)
	if e != nil {
		return model.Library{}, errors.Join(mediafs.ErrPath, e)
	}
	l := model.Library{ID: model.NewID(), Name: name, Path: root.Path, Identity: root.Identity, Kind: kind, Sources: append([]string{}, sources...)}
	// Reserve the new library before a worker can act on its committed scan.
	c.mu.Lock()
	c.busy[l.ID] = true
	c.mu.Unlock()
	l, e = c.store.CreateLibrary(ctx, l)
	if e == nil {
		c.notify()
	} else {
		c.release(l.ID)
	}
	return l, e
}
func (c *Catalog) Update(ctx context.Context, id string, rev int64, name *string, sources *[]string) (model.Library, error) {
	ctx, done, e := c.begin(ctx)
	if e != nil {
		return model.Library{}, e
	}
	defer done()
	if name == nil && sources == nil {
		return model.Library{}, model.ErrInvalid
	}
	if name != nil {
		n := strings.TrimSpace(*name)
		name = &n
		if !validName(n) {
			return model.Library{}, model.ErrInvalid
		}
	}
	if sources != nil && !validSources(*sources) {
		return model.Library{}, model.ErrInvalid
	}
	return c.store.UpdateLibrary(ctx, id, rev, name, sources)
}
func (c *Catalog) StartScan(ctx context.Context, library string) (model.Scan, error) {
	ctx, done, e := c.begin(ctx)
	if e != nil {
		return model.Scan{}, e
	}
	defer done()
	c.mu.Lock()
	if c.busy[library] {
		c.mu.Unlock()
		return model.Scan{}, model.ErrBusy
	}
	c.busy[library] = true
	c.mu.Unlock()
	scan, e := c.store.QueueScan(ctx, library)
	if e != nil {
		c.release(library)
	} else {
		c.notify()
	}
	return scan, e
}
func (c *Catalog) release(id string) { c.mu.Lock(); delete(c.busy, id); c.mu.Unlock() }

// WithMutation fails immediately for a queued/running scan or another mutation.
// Close cancels pre-unlink work and waits for the callback, including any bounded
// post-unlink reconciliation. No lock is taken by reads or progress writes.
func (c *Catalog) WithMutation(ctx context.Context, library string, fn func(context.Context) error) error {
	ctx, done, e := c.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	c.mu.Lock()
	if c.busy[library] {
		c.mu.Unlock()
		return model.ErrBusy
	}
	c.busy[library] = true
	c.mu.Unlock()
	defer c.release(library)
	if _, e = c.store.Library(ctx, library); e != nil {
		return e
	}
	return fn(ctx)
}
func (c *Catalog) OpenVideo(ctx context.Context, id string) (*os.File, error) {
	file, e := c.store.MediaFile(ctx, id)
	if e != nil {
		return nil, e
	}
	if file.State != "available" {
		return nil, model.ErrConflict
	}
	return c.fs.OpenVideo(Reference(file))
}
func Reference(f model.MediaFile) mediafs.Ref {
	return mediafs.Ref{Root: mediafs.Root{Path: f.RootPath, Identity: f.RootIdentity}, Relative: f.Relative, Fingerprint: f.Fingerprint}
}
func (c *Catalog) recordError(e error) {
	if e == nil {
		return
	}
	c.mu.Lock()
	c.lastErr = e
	c.mu.Unlock()
	slog.Error("catalog persistence failure", "category", "scan_state")
}
func (c *Catalog) worker() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.wake:
		}
		for c.ctx.Err() == nil {
			scan, e := c.store.ClaimScan(c.ctx)
			if errors.Is(e, model.ErrNotFound) {
				break
			}
			if e != nil {
				if c.ctx.Err() != nil {
					return
				}
				slog.Error("scan claim failed; retrying", "category", "database_unavailable")
				select {
				case <-c.ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
				}
				continue
			}
			// Reservation precedes queue publication, including the initial scan.
			c.mu.Lock()
			c.busy[scan.LibraryID] = true
			c.mu.Unlock()
			started := time.Now()
			e = c.scan(scan)
			if e != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				c.recordError(c.store.FailScan(ctx, scan.ID, c.ctx.Err() != nil))
				cancel()
			}
			c.release(scan.LibraryID)
			slog.Info("scan finished", "scan_id", scan.ID, "library_id", scan.LibraryID, "published", e == nil, "elapsed_ms", time.Since(started).Milliseconds())
		}
	}
}
func (c *Catalog) scan(scan model.Scan) error {
	l, e := c.store.Library(c.ctx, scan.LibraryID)
	if e != nil {
		return e
	}
	e = c.fs.Walk(c.ctx, mediafs.Root{Path: l.Path, Identity: l.Identity}, func(files []mediafs.Entry) error {
		batch := make([]model.ScanEntry, 0, len(files))
		for _, f := range files {
			entry := model.ScanEntry{Relative: f.Relative, Fingerprint: f.Fingerprint, Size: f.Size, ModifiedAt: f.ModifiedAt}
			if l.Kind == "series" {
				entry.Organization = IdentifyEpisode(f.Relative)
			}
			batch = append(batch, entry)
		}
		return c.store.StageScan(c.ctx, scan.ID, batch)
	})
	if e != nil {
		return e
	}
	return c.store.CommitScan(c.ctx, scan.ID)
}
func (c *Catalog) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.cancel()
		c.mu.Unlock()
		c.wg.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c.closeErr = c.store.InterruptScans(ctx)
	})
	return c.closeErr
}
