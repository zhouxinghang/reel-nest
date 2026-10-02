// Package metadata owns provider configuration, matching jobs and image
// downloads. Workers consume immutable snapshots; settings locks never cover
// network I/O. No legacy environment import is supported.
package metadata

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"reelnest/internal/model"
	"reelnest/internal/store"
)

var ErrSettingsUnreadable = errors.New("provider settings cannot be decrypted or validated; restore a complete DATA_DIR backup")

const (
	TMDBBaseURL = "https://api.themoviedb.org/3"
	TVDBBaseURL = "https://api4.thetvdb.com/v4"
)

var sources = [3]string{"tmdb", "tvdb", "metatube"}

// ProviderSnapshot is a copied, immutable configuration capability. It is not
// an HTTP DTO. Secrets are private, omitted from JSON and redacted by fmt.
// Callers must still validate its revision/catalog in the result's write
// transaction; holding a snapshot does NOT authorize a future metadata commit.
type ProviderSnapshot struct {
	source, catalogID, baseURL, credential, pin string
	revision                                    int64
}

func (p ProviderSnapshot) Source() string     { return p.source }
func (p ProviderSnapshot) CatalogID() string  { return p.catalogID }
func (p ProviderSnapshot) BaseURL() string    { return p.baseURL }
func (p ProviderSnapshot) Revision() int64    { return p.revision }
func (p ProviderSnapshot) Credential() string { return p.credential }
func (p ProviderSnapshot) PIN() string        { return p.pin }
func (p ProviderSnapshot) Configured() bool {
	if p.source == "metatube" {
		return p.baseURL != ""
	}
	return p.credential != ""
}
func (p ProviderSnapshot) String() string {
	return fmt.Sprintf("ProviderSnapshot{source:%s revision:%d configured:%t}", p.source, p.revision, p.Configured())
}
func (p ProviderSnapshot) GoString() string { return p.String() }

type registry struct{ providers [3]ProviderSnapshot }

// Service owns DB-commit + registry-publication serialization, not network I/O.
// Snapshot and Settings reads never acquire the update mutex. The containing
// runtime drains HTTP/workers before closing the store.
type Service struct {
	store   *store.Store
	cipher  cipher.AEAD
	update  sync.Mutex
	current atomic.Pointer[registry]
	closed  atomic.Bool
	engine  *engine
}

func New(ctx context.Context, s *store.Store) (*Service, error) {
	key := s.Keys().Settings
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	m := &Service{store: s, cipher: aead}
	rows, err := s.ProviderSettings(ctx)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(sources) {
		return nil, ErrSettingsUnreadable
	}
	reg := &registry{}
	seen := [3]bool{}
	for _, row := range rows {
		i := sourceIndex(row.Source)
		if i < 0 || seen[i] {
			return nil, ErrSettingsUnreadable
		}
		seen[i] = true
		p, err := m.decrypt(row)
		if err != nil {
			return nil, err
		}
		reg.providers[i] = p
	}
	m.current.Store(reg)
	m.initEngine(Options{})
	return m, nil
}

func sourceIndex(source string) int {
	for i, s := range sources {
		if s == source {
			return i
		}
	}
	return -1
}

func (m *Service) Snapshot(source string) (ProviderSnapshot, error) {
	if m.closed.Load() {
		return ProviderSnapshot{}, model.ErrClosed
	}
	i := sourceIndex(source)
	if i < 0 {
		return ProviderSnapshot{}, model.ErrNotFound
	}
	return m.current.Load().providers[i], nil
}

// Settings returns one coherent publication; no caller can mutate the registry.
func (m *Service) Settings() ([]ProviderSnapshot, error) {
	if m.closed.Load() {
		return nil, model.ErrClosed
	}
	reg := m.current.Load()
	return append([]ProviderSnapshot{}, reg.providers[:]...), nil
}

// Close prevents new saves and waits for any already-entered save to publish.
// It does not own or close the database.
func (m *Service) Close() {
	m.update.Lock()
	m.engine.mu.Lock()
	m.closed.Store(true)
	m.engine.cancel()
	m.engine.mu.Unlock()
	m.update.Unlock()
	m.engine.wg.Wait()
}

type SettingsPatch struct {
	Revision                  int64
	Credential, PIN, URL      *string
	ClearCredential, ClearPIN bool
}

func (m *Service) Update(ctx context.Context, source string, patch SettingsPatch) (ProviderSnapshot, error) {
	m.update.Lock()
	defer m.update.Unlock()
	old, err := m.Snapshot(source)
	if err != nil {
		return ProviderSnapshot{}, err
	}
	if err = ctx.Err(); err != nil {
		return ProviderSnapshot{}, err
	}
	if patch.Revision < 1 || patch.Revision >= 9007199254740991 {
		return ProviderSnapshot{}, model.ErrInvalid
	}
	if old.revision != patch.Revision {
		return ProviderSnapshot{}, model.ErrConflict
	}
	if (source != "tvdb" && (patch.PIN != nil || patch.ClearPIN)) || (source != "metatube" && patch.URL != nil) {
		return ProviderSnapshot{}, model.ErrInvalid
	}
	next := old
	if next.credential, err = patchSecret(old.credential, patch.Credential, patch.ClearCredential); err != nil {
		return ProviderSnapshot{}, err
	}
	if next.pin, err = patchSecret(old.pin, patch.PIN, patch.ClearPIN); err != nil {
		return ProviderSnapshot{}, err
	}
	if patch.URL != nil {
		if next.baseURL, err = NormalizeMetaTubeURL(*patch.URL); err != nil {
			return ProviderSnapshot{}, err
		}
		// Even a credential typed identically must be an explicit replacement.
		// A blank/null field is never consent to transfer an existing credential.
		replacement := patch.Credential != nil && strings.TrimSpace(*patch.Credential) != ""
		if next.baseURL != old.baseURL && old.credential != "" && !replacement && !patch.ClearCredential {
			return ProviderSnapshot{}, model.ErrInvalid
		}
	}
	if next == old {
		return old, nil // No revision churn or work requeue for an empty form save.
	}
	next.revision++
	sealed, err := m.encrypt(next)
	if err != nil {
		return ProviderSnapshot{}, err
	}
	row, err := m.store.SaveProviderSettings(ctx, model.StoredProviderSettings{
		Source: source, BaseURL: next.baseURL, Secrets: sealed, Revision: next.revision,
	}, old.revision)
	if err != nil {
		return ProviderSnapshot{}, err
	}
	next.catalogID = row.CatalogID
	// Publication cannot fail and must happen even if the request is canceled
	// immediately after a successful commit. Readers see either complete version.
	reg := *m.current.Load()
	reg.providers[sourceIndex(source)] = next
	m.current.Store(&reg)
	return next, nil
}

func validSecret(value string) bool {
	return utf8.ValidString(value) && len(value) <= 4096 && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}
func patchSecret(old string, value *string, clear bool) (string, error) {
	v := ""
	if value != nil {
		v = strings.TrimSpace(*value)
	}
	if !validSecret(v) || (clear && v != "") {
		return "", model.ErrInvalid
	}
	if clear {
		return "", nil
	}
	if v != "" {
		return v, nil
	}
	return old, nil
}

func settingsAAD(p ProviderSnapshot) []byte {
	return []byte(fmt.Sprintf("reelnest:provider-settings:v1\x00%s\x00%s\x00%d", p.source, p.baseURL, p.revision))
}

type secrets struct{ Credential, PIN string }

func (m *Service) encrypt(p ProviderSnapshot) ([]byte, error) {
	// Even an empty configuration is authenticated; configured is a semantic
	// property, not inferred from the presence of ciphertext.
	body, err := json.Marshal(secrets{p.credential, p.pin})
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, m.cipher.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return m.cipher.Seal(nonce, nonce, body, settingsAAD(p)), nil
}

func (m *Service) decrypt(row model.StoredProviderSettings) (ProviderSnapshot, error) {
	p := ProviderSnapshot{source: row.Source, catalogID: row.CatalogID, baseURL: row.BaseURL, revision: row.Revision}
	if p.revision < 1 || p.revision > 9007199254740991 {
		return ProviderSnapshot{}, ErrSettingsUnreadable
	}
	if row.Secrets == nil {
		// Only the schema's never-configured rows may have no authenticated value.
		if p.revision != 1 || (p.source == "metatube" && (p.catalogID != "" || p.baseURL != "")) {
			return ProviderSnapshot{}, ErrSettingsUnreadable
		}
	} else {
		size := m.cipher.NonceSize()
		if len(row.Secrets) < size+m.cipher.Overhead() {
			return ProviderSnapshot{}, ErrSettingsUnreadable
		}
		body, err := m.cipher.Open(nil, row.Secrets[:size], row.Secrets[size:], settingsAAD(p))
		var s secrets
		if err != nil || json.Unmarshal(body, &s) != nil || !validSecret(s.Credential) || !validSecret(s.PIN) {
			return ProviderSnapshot{}, ErrSettingsUnreadable
		}
		p.credential, p.pin = s.Credential, s.PIN
	}
	switch p.source {
	case "tmdb":
		if p.catalogID != "tmdb" || p.baseURL != TMDBBaseURL || p.pin != "" {
			return ProviderSnapshot{}, ErrSettingsUnreadable
		}
	case "tvdb":
		if p.catalogID != "tvdb" || p.baseURL != TVDBBaseURL {
			return ProviderSnapshot{}, ErrSettingsUnreadable
		}
	case "metatube":
		normalized, err := NormalizeMetaTubeURL(p.baseURL)
		if err != nil || normalized != p.baseURL || (p.baseURL == "") != (p.catalogID == "") || p.pin != "" {
			return ProviderSnapshot{}, ErrSettingsUnreadable
		}
	default:
		return ProviderSnapshot{}, ErrSettingsUnreadable
	}
	return p, nil
}
