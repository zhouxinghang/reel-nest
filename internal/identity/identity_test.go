package identity_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"reelnest/internal/identity"
	"reelnest/internal/store"
)

func TestHouseholdSessionsRotationRestartExpiryAndLimiter(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, e := store.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	i, e := identity.New(ctx, s, "家庭", "a-long-test-password")
	if e != nil {
		t.Fatal(e)
	}
	token, expiry, e := i.Login(ctx, "家庭", "a-long-test-password", "")
	if e != nil || len(token) != 64 || time.Until(expiry) < 29*24*time.Hour {
		t.Fatal(token, expiry, e)
	}
	if name, e := i.Session(ctx, token); e != nil || name != "家庭" {
		t.Fatal(name, e)
	}
	second, _, e := i.Login(ctx, "家庭", "a-long-test-password", token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = i.Session(ctx, token); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("old cookie retained", e)
	}
	key := s.Keys()
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = store.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	i, e = identity.New(ctx, s, "replacement", "invalid")
	if e != nil {
		t.Fatal("reinitialized account on restart", e)
	}
	if name, e := i.Session(ctx, second); e != nil || name != "家庭" || s.Keys() != key {
		t.Fatal(name, e)
	}
	for _, fake := range []string{"", "garbage", strings.Repeat("a", 64), hex.EncodeToString(key.Stream[:])} {
		if _, e = i.Session(ctx, fake); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("non-session granted household rights", e)
		}
	}
	if e = i.Logout(ctx, second); e != nil {
		t.Fatal(e)
	}
	if _, e = i.Session(ctx, second); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("logout not revoked", e)
	}
	hash := sha256.Sum256([]byte(token))
	if e = s.CreateSession(ctx, hex.EncodeToString(hash[:]), "", time.Now().UnixMilli()-10, time.Now().UnixMilli()-1); e != nil {
		t.Fatal(e)
	}
	if _, e = i.Session(ctx, token); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("expired session valid", e)
	}
	for n := 0; n < 10; n++ {
		if _, _, e = i.Login(ctx, "家庭", "incorrect", ""); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal(n, e)
		}
	}
	if _, _, e = i.Login(ctx, "家庭", "a-long-test-password", ""); !errors.Is(e, identity.ErrLimited) {
		t.Fatal("limiter missing", e)
	}
}
