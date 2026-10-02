package metadata

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reelnest/internal/model"
	"reelnest/internal/store"
)

func actorService(t *testing.T, base string) (*Service, ProviderSnapshot) {
	t.Helper()
	s, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close(); s.Close() })
	p, err := m.Update(context.Background(), "metatube", SettingsPatch{Revision: 1, URL: &base})
	if err != nil {
		t.Fatal(err)
	}
	return m, p
}

func TestActorReferencesExpireAndStayWithinContractBounds(t *testing.T) {
	m, p := actorService(t, "http://unused.invalid")
	ref := model.ActorRef{CatalogID: p.CatalogID(), ProviderRevision: p.Revision(), Namespace: "example", RemoteID: "RawID"}
	token := actorToken{Ref: ref, Kind: "actor", Expires: time.Now().Add(-time.Second).UnixMilli()}
	if _, err := m.Actor(context.Background(), m.signActor(token)); !errors.Is(err, model.ErrCandidate) {
		t.Fatal("expired ref", err)
	}
	token.Expires = time.Now().Add(time.Hour).UnixMilli()
	token.Ref.RemoteID = strings.Repeat("\\", 1024)
	token.Kind = "wallpaper"
	token.Image = "https://images.invalid/" + strings.Repeat("&", 2000)
	id := m.signActor(token)
	if len(id) > 8192 {
		t.Fatal("token exceeds OpenAPI bound", len(id))
	}
	if got, _, err := m.readActorToken(id, true); err != nil || got != token {
		t.Fatal("token round trip", err)
	}
	if _, _, err := m.readActorToken(id, false); !errors.Is(err, model.ErrInvalid) {
		t.Fatal("image token used as actor", err)
	}
}

func TestActorQueriesDiscardResponsesAfterSettingsChange(t *testing.T) {
	for _, kind := range []string{"search", "detail", "image"} {
		t.Run(kind, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				<-release
				switch kind {
				case "search":
					fmt.Fprint(w, `{"data":[{"id":"1","provider":"example","name":"Actor"}]}`)
				case "detail":
					fmt.Fprint(w, `{"data":{"id":"1","provider":"example","name":"Actor"}}`)
				default:
					w.Header().Set("Content-Type", "image/png")
					fmt.Fprint(w, "\x89PNG\r\n\x1a\nfixture")
				}
			}))
			defer upstream.Close()
			m, p := actorService(t, upstream.URL)
			token := actorToken{Ref: model.ActorRef{CatalogID: p.CatalogID(), ProviderRevision: p.Revision(), Namespace: "example", RemoteID: "1"}, Kind: "actor", Expires: time.Now().Add(time.Hour).UnixMilli()}
			if kind == "image" {
				token.Kind = "photo"
			}
			done := make(chan error, 1)
			go func() {
				var err error
				switch kind {
				case "search":
					_, _, err = m.SearchActors(context.Background(), "Actor")
				case "detail":
					_, err = m.Actor(context.Background(), m.signActor(token))
				default:
					_, _, err = m.ActorImage(context.Background(), m.signActor(token))
				}
				done <- err
			}()
			<-entered
			base := "http://new.invalid"
			_, err := m.Update(context.Background(), "metatube", SettingsPatch{Revision: p.Revision(), URL: &base})
			close(release)
			if err != nil {
				t.Fatal(err)
			}
			if err = <-done; !errors.Is(err, model.ErrCandidate) {
				t.Fatal("old response escaped isolation", err)
			}
		})
	}
}

func TestActorQueryCancellationAndImageBudget(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer upstream.Close()
	m, p := actorService(t, upstream.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := m.SearchActors(ctx, "actor"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Saturate the provider budget. A canceled waiter never makes a request.
	budget := m.engine.perSource[sourceIndex("metatube")]
	budget <- struct{}{}
	budget <- struct{}{}
	token := actorToken{Ref: model.ActorRef{CatalogID: p.CatalogID(), ProviderRevision: p.Revision(), Namespace: "example", RemoteID: "1"}, Kind: "photo", Expires: time.Now().Add(time.Hour).UnixMilli()}
	if _, _, err := m.ActorImage(ctx, m.signActor(token)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	<-budget
	<-budget
	if calls.Load() != 0 {
		t.Fatal("canceled request reached upstream")
	}
}
