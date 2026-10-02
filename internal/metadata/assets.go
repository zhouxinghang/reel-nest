package metadata

import (
	"context"
	"errors"
	"fmt"
	"reelnest/internal/model"
	"time"
)

func ready(a model.Asset, now int64) bool {
	return a.State == "ready" && a.ExpiresAt != nil && *a.ExpiresAt > now
}

// Asset shares an app-lifetime download, not the first HTTP waiter's lifetime.
// Hits consult SQLite directly and never acquire any download semaphore.
func (m *Service) Asset(ctx context.Context, id string) (model.Asset, error) {
	if m.closed.Load() {
		return model.Asset{}, model.ErrClosed
	}
	now := time.Now().UnixMilli()
	a, err := m.store.MetadataAsset(ctx, id, now)
	if err != nil {
		return a, err
	}
	if ready(a, now) {
		return a, nil
	}
	if a.RetryAt != nil && *a.RetryAt > now {
		return a, model.ErrAsset
	}
	p, err := m.Snapshot(a.Source)
	if err != nil {
		return a, err
	}
	if !p.Configured() || p.CatalogID() != a.CatalogID || p.Revision() != a.ProviderRevision {
		return a, model.ErrAsset
	}
	e := m.engine
	key := fmt.Sprintf("%s:%d:%d:%d", a.ID, a.Revision, a.SnapshotRevision, a.ProviderRevision)
	e.mu.Lock()
	if m.closed.Load() {
		e.mu.Unlock()
		return a, model.ErrClosed
	}
	f := e.flights[key]
	if f == nil {
		f = &flight{done: make(chan struct{})}
		e.flights[key] = f
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			m.download(a, p, f)
			e.mu.Lock()
			delete(e.flights, key)
			close(f.done)
			e.mu.Unlock()
		}()
	}
	e.mu.Unlock()
	select {
	case <-ctx.Done():
		return model.Asset{}, ctx.Err()
	case <-f.done:
		if f.err != nil {
			return model.Asset{}, f.err
		}
		return m.store.MetadataAsset(ctx, id, time.Now().UnixMilli())
	}
}
func acquire(ctx context.Context, ch chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case ch <- struct{}{}:
		return nil
	}
}
func (m *Service) download(a model.Asset, p ProviderSnapshot, f *flight) {
	ctx, cancel := context.WithTimeout(m.engine.ctx, 15*time.Second)
	defer cancel()
	e := m.engine
	// Acquire the narrower source budget first so a slow source cannot occupy all
	// global slots while waiting for its own two slots.
	source := e.perSource[sourceIndex(a.Source)]
	if err := acquire(ctx, source); err != nil {
		f.err = err
		return
	}
	defer func() { <-source }()
	if err := acquire(ctx, e.global); err != nil {
		f.err = err
		return
	}
	defer func() { <-e.global }()
	// A previous flight may have completed between the caller read and registration.
	fresh, err := m.store.MetadataAsset(ctx, a.ID, time.Now().UnixMilli())
	if err != nil {
		f.err = err
		return
	}
	if ready(fresh, time.Now().UnixMilli()) {
		return
	}
	body, mime, err := m.adapter(p).FetchImage(ctx, a.Kind, a.Reference)
	if e.ctx.Err() != nil || errors.Is(err, context.Canceled) {
		f.err = err
		return
	}
	commitCtx, stop := context.WithTimeout(e.ctx, 5*time.Second)
	defer stop()
	commitErr := m.store.CompleteAsset(commitCtx, a, body, mime, err != nil, time.Now().UnixMilli())
	if commitErr != nil {
		f.err = commitErr
	} else if err != nil {
		f.err = model.ErrAsset
	}
}
