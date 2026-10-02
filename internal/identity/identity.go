// Package identity owns the single household identity, sessions and bounded
// password verification. Stream credentials are deliberately not accepted here.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
	"reelnest/internal/store"
)

var ErrUnauthorized = errors.New("UNAUTHENTICATED")
var ErrLimited = errors.New("RATE_LIMITED")

const Lifetime = 30 * 24 * time.Hour
const CookieName = "reelnest_session"

type Identity struct {
	store    *store.Store
	gate     chan struct{}
	mu       sync.Mutex
	window   time.Time
	failures int
}

func New(ctx context.Context, s *store.Store, username, password string) (*Identity, error) {
	name, _, e := s.Household(ctx)
	if e != nil {
		return nil, e
	}
	if name == "" {
		username = strings.TrimSpace(username)
		if username == "" || !utf8.ValidString(username) || utf8.RuneCountInString(username) > 80 || strings.IndexFunc(username, unicode.IsControl) >= 0 || len(password) < 12 || len(password) > 72 {
			return nil, errors.New("new household requires a valid username and a 12–72 byte password")
		}
		hash, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if e != nil {
			return nil, e
		}
		if e = s.InitializeHousehold(ctx, username, hash); e != nil {
			return nil, e
		}
	}
	return &Identity{store: s, gate: make(chan struct{}, 1)}, nil
}
func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Login has one verifier and no unbounded wait queue. A busy verifier and ten
// failures in five minutes both rate-limit callers, without trusting proxy IPs.
func (i *Identity) Login(ctx context.Context, user, password, oldToken string) (string, time.Time, error) {
	select {
	case i.gate <- struct{}{}:
		defer func() { <-i.gate }()
	default:
		return "", time.Time{}, ErrLimited
	}
	if e := ctx.Err(); e != nil {
		return "", time.Time{}, e
	}
	now := time.Now()
	i.mu.Lock()
	if now.Sub(i.window) >= 5*time.Minute {
		i.window = now
		i.failures = 0
	}
	limited := i.failures >= 10
	i.mu.Unlock()
	if limited {
		return "", time.Time{}, ErrLimited
	}
	name, hash, e := i.store.Household(ctx)
	if e != nil {
		return "", time.Time{}, e
	}
	valid := bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
	if !valid || user != name {
		i.mu.Lock()
		i.failures++
		i.mu.Unlock()
		return "", time.Time{}, ErrUnauthorized
	}
	var b [32]byte
	_, _ = rand.Read(b[:])
	token := hex.EncodeToString(b[:])
	expiry := now.Add(Lifetime)
	if e = i.store.CreateSession(ctx, tokenHash(token), tokenHash(oldToken), now.UnixMilli(), expiry.UnixMilli()); e != nil {
		return "", time.Time{}, e
	}
	i.mu.Lock()
	i.failures = 0
	i.mu.Unlock()
	return token, expiry, nil
}
func (i *Identity) Session(ctx context.Context, token string) (string, error) {
	if len(token) != 64 {
		return "", ErrUnauthorized
	}
	if _, e := hex.DecodeString(token); e != nil {
		return "", ErrUnauthorized
	}
	name, e := i.store.Session(ctx, tokenHash(token), time.Now().UnixMilli())
	if e != nil {
		return "", ErrUnauthorized
	}
	return name, nil
}
func (i *Identity) Logout(ctx context.Context, token string) error {
	return i.store.DeleteSession(ctx, tokenHash(token))
}
