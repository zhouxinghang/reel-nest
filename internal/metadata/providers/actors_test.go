package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"reelnest/internal/model"
)

func TestMetaTubeActorsSearchDetailsAndUncroppedImages(t *testing.T) {
	var details, images atomic.Int32
	c := fixture(t, "metatube", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Upstream") != "configured.invalid" {
			t.Error("left configured origin", r.URL)
		}
		if strings.Contains(r.URL.Path, "/images/") {
			images.Add(1)
			if r.Header.Get("Authorization") != "" || !strings.Contains(r.RequestURI, "/images/primary/example/Ab%2FC") {
				t.Error("wrong image auth/identity", r.RequestURI)
			}
			if images.Load() == 1 && r.URL.RawQuery != "" {
				t.Error("portrait must use default crop")
			}
			if images.Load() == 2 && (r.URL.Query().Get("ratio") != "0" || r.URL.Query().Get("url") != "https://images.invalid/wall.jpg?a=1&b=2") {
				t.Error("wallpaper URL/crop", r.URL)
			}
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "\x89PNG\r\n\x1a\nfixture")
			return
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-secret" {
			t.Error("missing actor bearer")
		}
		if strings.HasSuffix(r.URL.Path, "/actors/search") {
			if r.URL.Query().Get("q") != "名字 & 别名" || r.URL.Query().Get("fallback") != "true" {
				t.Error(r.URL)
			}
			jsonReply(w, `{"data":[{"id":"Ab/C","provider":"EXAMPLE","name":"同名演员","aliases":["别名","别名",""]},{"id":"Ab/C","provider":"example","name":"重复"},{"id":"Ab/C","provider":"Other","name":"同名演员"}]}`)
			return
		}
		if !strings.Contains(r.RequestURI, "/actors/example/Ab%2FC?lazy=true") {
			t.Error(r.RequestURI)
		}
		details.Add(1)
		jsonReply(w, `{"data":{"id":"Ab/C","provider":"Example","name":"同名演员","aliases":["别名"],"summary":"简介","homepage":"javascript:bad","height":170,"birthday":"1995-01-02T00:00:00Z","debut_date":"0001-01-01T00:00:00Z","nationality":"日本","hobby":"阅读","skill":"舞蹈","blood_type":"A","images":["https://images.invalid/wall.jpg?a=1&b=2","https://images.invalid/wall.jpg?a=1&b=2","file:///private","https://user:password@evil.invalid/x"]}}`)
	})
	ctx := context.Background()
	page, err := c.SearchActors(ctx, "名字 & 别名")
	if err != nil || page.Truncated || len(page.Items) != 2 || page.Items[0].Ref.Namespace != "example" || page.Items[0].Ref.RemoteID != "Ab/C" || len(page.Items[0].Aliases) != 1 {
		t.Fatal(page, err)
	}
	a, err := c.FetchActor(ctx, page.Items[0].Ref)
	if err != nil || details.Load() != 1 || a.Name != "同名演员" || a.Summary != "简介" || a.Height != "170" || a.Birthday != "1995-01-02" || a.DebutDate != "" || a.Homepage != "" || len(a.Images) != 1 {
		t.Fatal(a, err)
	}
	for i, original := range []bool{false, true} {
		image := ""
		if original {
			image = a.Images[0]
		}
		b, mime, err := c.FetchActorImage(ctx, a.Ref, image, original)
		if err != nil || mime != "image/png" || len(b) == 0 || images.Load() != int32(i+1) {
			t.Fatal(mime, err)
		}
	}
}

func TestMetaTubeActorsRejectInvalidAndMismatchedReplies(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantError  bool
	}{
		{"empty", `{"data":[]}`, 200, false},
		{"not-found", `{"error":{"code":404}}`, 404, false},
		{"missing-data", `{}`, 200, true},
		{"null", `{"data":null}`, 200, true},
		{"error", `{"data":[],"error":{"message":"synthetic-secret"}}`, 200, true},
		{"malformed", `not json`, 200, true},
		{"auth", `synthetic-secret`, 401, true},
		{"rate-limit", `synthetic-secret`, 429, true},
		{"failure", `synthetic-secret`, 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixture(t, "metatube", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			page, err := c.SearchActors(context.Background(), "演员")
			if (err != nil) != tc.wantError || len(page.Items) != 0 || (err != nil && strings.Contains(err.Error(), "synthetic-secret")) {
				t.Fatal(page, err)
			}
		})
	}
	for _, body := range []string{
		`{"data":{"id":"other","provider":"example","name":"演员"}}`,
		`{"data":{"id":"1","provider":"other","name":"演员"}}`,
		`{"data":{"id":"1","provider":"example","name":""}}`,
	} {
		c := fixture(t, "metatube", func(w http.ResponseWriter, r *http.Request) { jsonReply(w, body) })
		if _, err := c.FetchActor(context.Background(), model.ActorRef{CatalogID: "metatube", Namespace: "example", RemoteID: "1", ProviderRevision: 2}); !errors.Is(err, model.ErrMetadata) {
			t.Fatal(body, err)
		}
	}
}

func TestMetaTubeActorsBoundedResultsAndNoGuessedIdentity(t *testing.T) {
	var rows []string
	rows = append(rows, `{"id":"..","provider":"example","name":"bad"}`, `{"id":"1","provider":"../bad","name":"bad"}`, `{"name":"name-only"}`)
	for i := 0; i < 60; i++ {
		rows = append(rows, fmt.Sprintf(`{"id":"%d","provider":"example","name":"演员"}`, i))
	}
	c := fixture(t, "metatube", func(w http.ResponseWriter, r *http.Request) { jsonReply(w, `{"data":[`+strings.Join(rows, ",")+`]}`) })
	page, err := c.SearchActors(context.Background(), "演员")
	if err != nil || len(page.Items) != 50 || !page.Truncated || page.Items[0].Ref.RemoteID != "0" {
		t.Fatal(page, err)
	}
	c = fixture(t, "metatube", func(w http.ResponseWriter, r *http.Request) { t.Error("invalid ref made network request") })
	for _, ref := range []model.ActorRef{
		{CatalogID: "old", Namespace: "example", RemoteID: "1", ProviderRevision: 2},
		{CatalogID: "metatube", Namespace: "example", RemoteID: "1", ProviderRevision: 1},
		{CatalogID: "metatube", Namespace: "example", RemoteID: "..", ProviderRevision: 2},
	} {
		if _, err := c.FetchActor(context.Background(), ref); !errors.Is(err, model.ErrCandidate) {
			t.Fatal(err)
		}
		if _, _, err := c.FetchActorImage(context.Background(), ref, "", true); !errors.Is(err, model.ErrAsset) {
			t.Fatal(err)
		}
	}
}

func TestMetaTubeActorImageCannotRedirectOrReturnHTML(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		var calls atomic.Int32
		c := fixture(t, "metatube", func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if redirect {
				http.Redirect(w, r, "https://other.invalid/secret", 302)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "<html>not an image</html>")
		})
		_, _, err := c.FetchActorImage(context.Background(), model.ActorRef{CatalogID: "metatube", Namespace: "example", RemoteID: "1", ProviderRevision: 2}, "", true)
		if err == nil || calls.Load() != 1 {
			t.Fatal(err, calls.Load())
		}
	}
}
