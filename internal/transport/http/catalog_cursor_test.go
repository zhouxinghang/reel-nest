package http

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"reelnest/internal/model"
)

func TestCatalogCursorBoundsTamperExpiryAndScope(t *testing.T) {
	tr := &Transport{cursorKey: [32]byte{1, 2, 3}}
	// Long collation keys are compressed, authenticated, bounded before decode,
	// and bound to normalized scope, page size, algorithm version and installation.
	c := catalogCursor{Scope: strings.Repeat("搜", 256), Position: model.CatalogPosition{Key: strings.Repeat("019aff43", 1024), ID: "media:fixture"}, Revision: 12, Expires: time.Now().Add(time.Minute).UnixMilli(), Limit: 50}
	p := tr.catalogPage(c)
	if len(*p.Next) > 4096 {
		t.Fatal("cursor exceeds contract")
	}
	r := httptest.NewRequest("GET", "/api/catalog?cursor="+url.QueryEscape(*p.Next), nil)
	got, e := tr.catalogCursor(r, c.Scope, 50)
	if e != nil || got != c {
		t.Fatal(got, e)
	}
	if _, e = tr.catalogCursor(r, "different scope", 50); e != model.ErrCursor {
		t.Fatal(e)
	}
	if _, e = tr.catalogCursor(r, c.Scope, 1); e != model.ErrCursor {
		t.Fatal(e)
	}
	other := &Transport{cursorKey: [32]byte{7}}
	if _, e = other.catalogCursor(r, c.Scope, 50); e != model.ErrCursor {
		t.Fatal(e)
	}
	r = httptest.NewRequest("GET", "/api/catalog?cursor="+url.QueryEscape(*p.Next+"x"), nil)
	if _, e = tr.catalogCursor(r, c.Scope, 50); e != model.ErrCursor {
		t.Fatal(e)
	}
	c.Expires = time.Now().UnixMilli()
	p = tr.catalogPage(c)
	r = httptest.NewRequest("GET", "/api/catalog?cursor="+url.QueryEscape(*p.Next), nil)
	if _, e = tr.catalogCursor(r, c.Scope, 50); e != model.ErrCursor {
		t.Fatal(e)
	}
	c.Expires = time.Now().Add(time.Minute).UnixMilli()
	c.Position.Key = strings.Repeat("a", 20000)
	p = tr.catalogPage(c)
	r = httptest.NewRequest("GET", "/api/catalog?cursor="+url.QueryEscape(*p.Next), nil)
	if _, e = tr.catalogCursor(r, c.Scope, 50); e != model.ErrCursor {
		t.Fatal("unbounded inflate", e)
	}
}
