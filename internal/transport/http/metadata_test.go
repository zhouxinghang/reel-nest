package http_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reelnest/internal/model"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMetadataHTTPContractsSelectionPeopleAndAssets(t *testing.T) {
	var details, images atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/movies/search"):
			fmt.Fprint(w, `{"data":[{"id":"Raw-AbC","provider":"Test","title":"电影","number":"ABC-001"}]}`)
		case strings.Contains(r.URL.Path, "/movies/test/Raw-AbC"):
			details.Add(1)
			fmt.Fprint(w, `{"data":{"id":"Raw-AbC","provider":"TEST","title":"电影","number":"ABC-001","actors":["同名演员"],"director":"导演","homepage":"https://example.invalid/title"}}`)
		case strings.Contains(r.URL.Path, "/images/"):
			images.Add(1)
			if r.Header.Get("Authorization") != "" {
				t.Error("image bearer leak")
			}
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\nfixture"))
		default:
			t.Error(r.URL)
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	h := start(t)
	h.request(t, "GET", "/api/metadata/status", "", 401)
	h.request(t, "POST", "/api/metadata/refresh", `{}`, 401)
	h.login(t)
	writeFile(t, filepath.Join(h.root, "movie", "movie.mp4"), "fixture")
	library, scan := h.create(t, filepath.Join(h.root, "movie"), "movie")
	h.await(t, scan, "completed")
	files, e := h.store.MediaFiles(context.Background(), library, "", 100)
	if e != nil || len(files) != 1 {
		t.Fatal(files, e)
	}
	subject := files[0].SubjectID
	h.request(t, "PATCH", "/api/settings/metadata/metatube", fmt.Sprintf(`{"revision":1,"url":%q,"credential":"fixture-token"}`, upstream.URL), 200)
	body := h.request(t, "GET", "/api/metadata/subjects/"+subject+"/candidates?source=metatube&q=ABC-001", "", 200)
	var candidates struct {
		Items                            []struct{ Ref json.RawMessage }
		SubjectRevision, BindingRevision int64
		Complete                         bool
	}
	if e = json.Unmarshal(body, &candidates); e != nil || !candidates.Complete || len(candidates.Items) != 1 {
		t.Fatal(string(body), e)
	}
	before, e := h.store.MetadataMatch(context.Background(), subject, time.Now().UnixMilli())
	if e != nil || before.Target.Selection != "" {
		t.Fatal("search wrote binding", before, e)
	}
	selection := fmt.Sprintf(`{"subjectRevision":%d,"expectedRevision":%d,"ref":%s}`, candidates.SubjectRevision, candidates.BindingRevision, candidates.Items[0].Ref)
	body = h.request(t, "PUT", "/api/metadata/subjects/"+subject+"/selection", selection, 200)
	var match struct {
		BindingRevision int64
		Selection       struct{ Mode string }
		Metadata        struct {
			Summary struct{ Artwork struct{ Primary string } }
			Credits struct{ Actors []struct{ PersonID string } }
		}
	}
	if e = json.Unmarshal(body, &match); e != nil || match.Selection.Mode != "manual" || match.BindingRevision != 1 || len(match.Metadata.Credits.Actors) != 1 {
		t.Fatal(string(body), e)
	}
	person := match.Metadata.Credits.Actors[0].PersonID
	h.request(t, "GET", "/api/people/"+person, "", 200)
	h.request(t, "GET", "/api/people/"+person+"/works?limit=1", "", 200)
	asset := match.Metadata.Summary.Artwork.Primary
	h.request(t, "GET", "/api/assets/"+asset+"/content", "", 200)
	h.request(t, "HEAD", "/api/assets/"+asset+"/content", "", 200)
	if images.Load() != 1 || details.Load() != 1 {
		t.Fatal("duplicate fetch", details.Load(), images.Load())
	}
	h.request(t, "PUT", "/api/metadata/subjects/"+subject+"/selection", selection, 409)
	h.request(t, "POST", "/api/metadata/refresh", fmt.Sprintf(`{"libraryId":%q}`, library), 202)
	h.request(t, "GET", "/api/metadata/status?libraryId="+library+"&limit=1", "", 200)
	h.request(t, "GET", "/api/metadata/subjects/"+subject+"/candidates?source=metatube", "", 400)
	h.request(t, "POST", "/api/metadata/refresh", `{"unknown":1}`, 400)
	h.request(t, "PATCH", "/api/settings/metadata/metatube", `{"revision":2,"url":"http://new.invalid","clearCredential":true}`, 200)
	selection = fmt.Sprintf(`{"subjectRevision":1,"expectedRevision":1,"ref":%s}`, candidates.Items[0].Ref)
	h.request(t, "PUT", "/api/metadata/subjects/"+subject+"/selection", selection, 409)
	h.request(t, "GET", "/api/people/"+person, "", 200)
	h.request(t, "GET", "/api/assets/"+asset+"/content", "", 200) // old ready content retains original TTL
	h.request(t, "POST", "/api/metadata/refresh", `{}`, 403, "Origin", "http://evil.invalid")
	saved := h.client
	h.client = &http.Client{}
	h.request(t, "GET", "/api/people/"+person, "", 401)
	h.request(t, "GET", "/api/assets/"+asset+"/content?token="+strings.Repeat("a", 64), "", 401)
	h.client = saved
}
func TestMetadataPeopleAndFailurePagesUseScopedCursors(t *testing.T) {
	h := start(t)
	h.login(t)
	for _, name := range []string{"one.mp4", "two.mp4", "three.mp4"} {
		writeFile(t, filepath.Join(h.root, "movies", name), "fixture")
	}
	lib, scan := h.create(t, filepath.Join(h.root, "movies"), "movie")
	h.await(t, scan, "completed")
	files, e := h.store.MediaFiles(context.Background(), lib, "", 100)
	if e != nil {
		t.Fatal(e)
	}
	var person string
	ctx := context.Background()
	now := time.Now().UnixMilli()
	for i, f := range files {
		ref := model.RemoteRef{Source: "tmdb", CatalogID: "tmdb", Namespace: "tmdb", Kind: "movie", RemoteID: fmt.Sprint(i + 1), ProviderRevision: 1}
		target, e := h.store.BeginSelection(ctx, f.SubjectID, 1, 0, []model.ProviderVersion{{Source: "tmdb", CatalogID: "tmdb", Revision: 1}}, ref)
		if e != nil {
			t.Fatal(e)
		}
		title := model.Title{Ref: ref, Title: "电影", FetchedAt: now, ExpiresAt: now + 3600000, CreditsState: "ready", ArtworkState: "ready", Credits: []model.Credit{{Key: "actor7", Name: "演员", Role: "actor", Characters: []string{"角色"}}}}
		if e = h.store.CompleteMetadata(ctx, model.MetadataResult{Target: target, Title: &title, Selection: "manual"}, now); e != nil {
			t.Fatal(e)
		}
		m, e := h.store.MetadataMatch(ctx, f.SubjectID, now)
		if e != nil {
			t.Fatal(e)
		}
		person = m.Credits[0].PersonID
	}
	body := h.request(t, "GET", "/api/people/"+person+"/works?limit=1", "", 200)
	var page struct {
		Items []json.RawMessage
		Page  struct {
			NextCursor *string
			ExpiresAt  *int64
		}
	}
	json.Unmarshal(body, &page)
	if len(page.Items) != 1 || page.Page.NextCursor == nil || page.Page.ExpiresAt == nil {
		t.Fatal(string(body))
	}
	cursor := *page.Page.NextCursor
	h.request(t, "GET", "/api/people/"+person+"/works?limit=1&cursor="+cursor, "", 200)
	h.request(t, "GET", "/api/people/other/works?limit=1&cursor="+cursor, "", 409)
	h.request(t, "GET", "/api/metadata/status?limit=1&cursor="+cursor, "", 409)
	if _, e = h.store.Favorite(ctx, "media", files[0].ID, true); e != nil {
		t.Fatal(e)
	}
	h.request(t, "GET", "/api/people/"+person+"/works?limit=1&cursor="+cursor, "", 409)
	h.request(t, "POST", "/api/metadata/refresh", `{}`, 202)
	versions := []model.ProviderVersion{{Source: "tmdb", CatalogID: "tmdb", Revision: 1}}
	for range files {
		target, e := h.store.ClaimMetadata(ctx, versions, now+10)
		if e != nil {
			t.Fatal(e)
		}
		if e = h.store.CompleteMetadata(ctx, model.MetadataResult{Target: target, ErrorCode: "PROVIDER_UNAVAILABLE"}, now+10); e != nil {
			t.Fatal(e)
		}
	}
	body = h.request(t, "GET", "/api/metadata/status?limit=1", "", 200)
	var status struct {
		Failed int
		Page   struct{ NextCursor *string }
	}
	json.Unmarshal(body, &status)
	if status.Failed != 3 || status.Page.NextCursor == nil {
		t.Fatal(string(body))
	}
	h.request(t, "GET", "/api/metadata/status?limit=1&cursor="+*status.Page.NextCursor, "", 200)
}
