package app

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"sync"

	"reelnest/internal/catalog"
	"reelnest/internal/identity"
	"reelnest/internal/mediafs"
	"reelnest/internal/metadata"
	"reelnest/internal/playback"
	"reelnest/internal/store"
	transport "reelnest/internal/transport/http"
	"reelnest/internal/web"
)

// RuntimeConfig assembles the single supported runtime. ProviderTransport is an
// optional external HTTP seam; production uses the default controlled client.
type RuntimeConfig struct {
	DataDir, Username, Password string
	MediaRoots                  []string
	CookieSecure                bool
	Assets                      fs.FS
	ProviderTransport           http.RoundTripper
}
type Runtime struct {
	handler  http.Handler
	catalog  *catalog.Catalog
	metadata *metadata.Service
	store    *store.Store
	once     sync.Once
	err      error
}

func NewRuntime(ctx context.Context, config RuntimeConfig) (_ *Runtime, err error) {
	if len(config.MediaRoots) > 100 {
		return nil, errors.New("at most 100 MEDIA_ROOTS are supported")
	}
	s, e := store.Open(ctx, config.DataDir)
	if e != nil {
		return nil, e
	}
	r := &Runtime{store: s}
	defer func() {
		if err != nil {
			_ = r.Close()
		}
	}()
	fs, e := mediafs.New(config.MediaRoots, config.DataDir)
	if e != nil {
		return nil, e
	}
	i, e := identity.New(ctx, s, config.Username, config.Password)
	if e != nil {
		return nil, e
	}
	r.metadata, e = metadata.NewWithOptions(ctx, s, metadata.Options{Transport: config.ProviderTransport})
	if e != nil {
		return nil, e
	}
	r.catalog, e = catalog.New(ctx, s, fs)
	if e != nil {
		return nil, e
	}
	if e = r.metadata.Start(); e != nil {
		return nil, e
	}
	r.handler, e = transport.New(s, i, r.catalog, r.metadata, playback.New(s, fs, r.catalog), config.MediaRoots, config.CookieSecure)
	if e != nil {
		return nil, e
	}
	if config.Assets != nil {
		r.handler = web.Handler(config.Assets, r.handler)
	}
	return r, nil
}
func (r *Runtime) Handler() http.Handler { return r.handler }

// HTTP server Shutdown should precede Close. Catalog atomically stops accepting
// work, cancels scans and drains all mutation/reconciliation leases before DB close.
func (r *Runtime) Close() error {
	r.once.Do(func() {
		if r.catalog != nil {
			r.err = r.catalog.Close()
		}
		if r.metadata != nil {
			r.metadata.Close()
		}
		r.err = errors.Join(r.err, r.store.Close())
	})
	return r.err
}
