package metadata

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"reelnest/internal/metadata/providers"
	"reelnest/internal/model"
)

// Actor queries are read-only and independent of local title/people bindings.
// Signed, expiring references authorize only provider-issued identities/images,
// never a URL supplied by the browser. Household authentication is still required.
type actorToken struct {
	Ref     model.ActorRef
	Kind    string
	Image   string
	Expires int64
}

type ActorResult struct {
	ID, PhotoID string
	model.ActorCandidate
}

type ActorDetail struct {
	ID, PhotoID string
	Wallpapers  []string
	model.ActorProfile
}

func (m *Service) actorSignature(payload string) []byte {
	key := m.store.Keys().Stream
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte("reelnest:actor-reference:v1:" + payload))
	return mac.Sum(nil)
}

func (m *Service) signActor(t actorToken) string {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(t)
	payload := base64.RawURLEncoding.EncodeToString(body.Bytes())
	return payload + "." + base64.RawURLEncoding.EncodeToString(m.actorSignature(payload))
}

func (m *Service) readActorToken(raw string, image bool) (actorToken, ProviderSnapshot, error) {
	var token actorToken
	p, err := m.Snapshot("metatube")
	if err != nil {
		return token, p, err
	}
	payload, signature, ok := strings.Cut(raw, ".")
	sig, decodeErr := base64.RawURLEncoding.DecodeString(signature)
	if len(raw) > 8192 || !ok || decodeErr != nil || !hmac.Equal(sig, m.actorSignature(payload)) {
		return token, p, model.ErrInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || json.Unmarshal(body, &token) != nil || !providers.ValidActorRef(token.Ref) ||
		(!image && (token.Kind != "actor" || token.Image != "")) ||
		(image && token.Kind != "photo" && token.Kind != "wallpaper") {
		return token, p, model.ErrInvalid
	}
	if token.Expires <= time.Now().UnixMilli() || !p.Configured() || token.Ref.CatalogID != p.CatalogID() || token.Ref.ProviderRevision != p.Revision() {
		return token, p, model.ErrCandidate
	}
	return token, p, nil
}

func (m *Service) actorContext(parent context.Context) (context.Context, func()) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	stop := context.AfterFunc(m.engine.ctx, cancel)
	return ctx, func() { stop(); cancel() }
}

func (m *Service) SearchActors(ctx context.Context, name string) ([]ActorResult, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 || strings.ContainsFunc(name, unicode.IsControl) {
		return nil, false, model.ErrInvalid
	}
	p, err := m.Snapshot("metatube")
	if err != nil {
		return nil, false, err
	}
	if !p.Configured() {
		return nil, false, model.ErrProvider
	}
	ctx, cancel := m.actorContext(ctx)
	defer cancel()
	page, err := m.adapter(p).SearchActors(ctx, name)
	if err != nil {
		return nil, false, err
	}
	current, err := m.Snapshot("metatube")
	if err != nil || current != p {
		return nil, false, model.ErrCandidate
	}
	expires := time.Now().Add(time.Hour).UnixMilli()
	items := []ActorResult{}
	for _, a := range page.Items {
		items = append(items, ActorResult{
			ID:      m.signActor(actorToken{Ref: a.Ref, Kind: "actor", Expires: expires}),
			PhotoID: m.signActor(actorToken{Ref: a.Ref, Kind: "photo", Expires: expires}), ActorCandidate: a,
		})
	}
	return items, page.Truncated, nil
}

func (m *Service) Actor(ctx context.Context, id string) (ActorDetail, error) {
	var result ActorDetail
	token, p, err := m.readActorToken(id, false)
	if err != nil {
		return result, err
	}
	ctx, cancel := m.actorContext(ctx)
	defer cancel()
	a, err := m.adapter(p).FetchActor(ctx, token.Ref)
	if err != nil {
		return result, err
	}
	if _, _, err = m.readActorToken(id, false); err != nil {
		return result, err
	}
	result = ActorDetail{ID: id, ActorProfile: a, Wallpapers: []string{}}
	token.Kind = "photo"
	result.PhotoID = m.signActor(token)
	token.Kind = "wallpaper"
	for _, image := range a.Images {
		token.Image = image
		result.Wallpapers = append(result.Wallpapers, m.signActor(token))
	}
	// Some providers omit images while still serving their primary image.
	if len(result.Wallpapers) == 0 {
		result.Wallpapers = append(result.Wallpapers, m.signActor(token))
	}
	return result, nil
}

func (m *Service) ActorImage(ctx context.Context, id string) ([]byte, string, error) {
	token, p, err := m.readActorToken(id, true)
	if err != nil {
		return nil, "", err
	}
	ctx, cancel := m.actorContext(ctx)
	defer cancel()
	// Share existing image budgets: two per provider, four globally. No SQL or
	// global lock is held during network calls, and shutdown cancels the wait.
	source := m.engine.perSource[sourceIndex("metatube")]
	if err = acquire(ctx, source); err != nil {
		return nil, "", err
	}
	defer func() { <-source }()
	if err = acquire(ctx, m.engine.global); err != nil {
		return nil, "", err
	}
	defer func() { <-m.engine.global }()
	if _, _, err = m.readActorToken(id, true); err != nil {
		return nil, "", err
	}
	body, mime, err := m.adapter(p).FetchActorImage(ctx, token.Ref, token.Image, token.Kind == "wallpaper")
	if err != nil {
		return nil, "", model.ErrAsset
	}
	if _, _, err = m.readActorToken(id, true); err != nil {
		return nil, "", err
	}
	return body, mime, nil
}
