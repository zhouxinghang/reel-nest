package http_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type settingsView struct {
	Source, Name                      string
	URL, CatalogID                    *string
	CredentialSet, PINSet, Configured bool
	Revision                          int64
}

func parseSetting(t *testing.T, b []byte) settingsView {
	t.Helper()
	var s settingsView
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestProviderSettingsHTTPContractAndConfiguredSemantics(t *testing.T) {
	h := start(t)
	h.request(t, "GET", "/api/settings/metadata", "", 401)
	h.request(t, "PATCH", "/api/settings/metadata/tmdb", `{"revision":1,"credential":"secret"}`, 401)
	h.login(t)
	b := h.request(t, "GET", "/api/settings/metadata", "", 200)
	var settings struct{ Sources []settingsView }
	if err := json.Unmarshal(b, &settings); err != nil || len(settings.Sources) != 3 {
		t.Fatal(string(b), err)
	}
	for _, p := range settings.Sources {
		if p.Configured || p.CredentialSet || p.PINSet || p.Revision != 1 || p.URL != nil {
			t.Fatal(p)
		}
	}
	b = h.request(t, "PATCH", "/api/settings/metadata/tmdb", `{"revision":1,"credential":"synthetic-token"}`, 200)
	p := parseSetting(t, b)
	if !p.Configured || !p.CredentialSet || p.Revision != 2 || p.CatalogID == nil || *p.CatalogID != "tmdb" || strings.Contains(string(b), "synthetic-token") {
		t.Fatal(string(b))
	}
	for _, body := range []string{`{"revision":2}`, `{"revision":2,"credential":null}`, `{"revision":2,"credential":"  "}`} {
		if got := parseSetting(t, h.request(t, "PATCH", "/api/settings/metadata/tmdb", body, 200)); got.Revision != 2 || !got.CredentialSet {
			t.Fatal(got)
		}
	}
	h.request(t, "PATCH", "/api/settings/metadata/tmdb", `{"revision":1,"credential":"stale"}`, 409)
	h.request(t, "PATCH", "/api/settings/metadata/tvdb", `{"revision":1,"credential":"test-key","pin":"test-pin"}`, 200)
	cleared := parseSetting(t, h.request(t, "PATCH", "/api/settings/metadata/tvdb", `{"revision":2,"clearCredential":true}`, 200))
	if cleared.Configured || cleared.CredentialSet || !cleared.PINSet {
		t.Fatal(cleared)
	}
	// An anonymous MetaTube address is configured without any API Token. A
	// cleared credential isn't configured just because its ciphertext exists.
	meta := parseSetting(t, h.request(t, "PATCH", "/api/settings/metadata/metatube", `{"revision":1,"url":"http://127.0.0.1:1/proxy"}`, 200))
	if !meta.Configured || meta.CredentialSet || meta.CatalogID == nil || meta.URL == nil || *meta.URL != "http://127.0.0.1:1/proxy/v1" {
		t.Fatal(meta)
	}
	b = h.request(t, "GET", "/api/libraries", "", 200)
	var libraries struct{ Sources []settingsView }
	if err := json.Unmarshal(b, &libraries); err != nil {
		t.Fatal(err)
	}
	for _, p := range libraries.Sources {
		if p.Configured != (p.Source != "tvdb") {
			t.Fatal("library status inferred from ciphertext", p)
		}
	}
	h.request(t, "PATCH", "/api/settings/metadata/metatube", `{"revision":2,"url":null}`, 200)
	h.request(t, "PATCH", "/api/settings/metadata/metatube", `{"revision":2,"credential":"old-token"}`, 200)
	h.request(t, "PATCH", "/api/settings/metadata/metatube", `{"revision":3,"url":"http://localhost:1"}`, 400)
	changed := parseSetting(t, h.request(t, "PATCH", "/api/settings/metadata/metatube", `{"revision":3,"url":"http://localhost:1","clearCredential":true}`, 200))
	if *changed.CatalogID == *meta.CatalogID || changed.CredentialSet {
		t.Fatal(changed)
	}
	disabled := parseSetting(t, h.request(t, "PATCH", "/api/settings/metadata/metatube", `{"revision":4,"url":""}`, 200))
	if disabled.Configured || disabled.CatalogID != nil || disabled.URL != nil {
		t.Fatal(disabled)
	}
	// Cookie-free stream-shaped credentials cannot read or change provider settings.
	saved := h.client
	h.client = &http.Client{}
	h.request(t, "GET", "/api/settings/metadata", "", 401, "Authorization", "Bearer "+strings.Repeat("a", 64))
	h.request(t, "PATCH", "/api/settings/metadata/tmdb", `{"revision":2,"clearCredential":true}`, 401)
	h.client = saved
	h.request(t, "PATCH", "/api/settings/metadata/tmdb", `{"revision":2,"clearCredential":true}`, 403, "Origin", "http://different.invalid")
	if got := parseSetting(t, h.request(t, "PATCH", "/api/settings/metadata/tmdb", `{"revision":2}`, 200)); !got.CredentialSet {
		t.Fatal("unauthorized write mutated setting")
	}
}

func TestProviderSettingsHTTPRejectsInvalidFormsWithoutSecretsInErrors(t *testing.T) {
	h := start(t)
	h.login(t)
	for _, body := range []string{
		`{}`, `{"revision":0}`, `{"revision":1,"unknown":"sensitive-marker"}`,
		`{"revision":1} {}`, `{"revision":1,"credential":7}`, `{"revision":1,"clearCredential":null}`,
		`{"revision":1,"credential":"sensitive-marker","clearCredential":true}`,
		`{"revision":1,"credential":"sensitive marker"}`, `{"revision":1,"url":null}`,
		`{"revision":1,"pin":null}`, `{"revision":1,"clearPin":false}`,
		fmt.Sprintf(`{"revision":1,"credential":%q}`, strings.Repeat("a", 4097)),
		fmt.Sprintf(`{"revision":1,"credential":%q}`, strings.Repeat("界", 1366)),
		fmt.Sprintf(`{"revision":1,"credential":%q}`, strings.Repeat("a", 17<<10)),
	} {
		b := h.request(t, "PATCH", "/api/settings/metadata/tmdb", body, 400)
		if strings.Contains(string(b), "sensitive") {
			t.Fatal("validation error echoed secret", string(b))
		}
	}
	for _, base := range []string{"file:///etc/passwd", "http://user:secret@localhost", "http://localhost?token=secret", "http://localhost/p/../v1"} {
		b := h.request(t, "PATCH", "/api/settings/metadata/metatube", fmt.Sprintf(`{"revision":1,"url":%q}`, base), 400)
		if strings.Contains(string(b), "secret") {
			t.Fatal("URL echoed credentials")
		}
	}
	if got := parseSetting(t, h.request(t, "PATCH", "/api/settings/metadata/tmdb", `{"revision":1}`, 200)); got.Revision != 1 || got.Configured {
		t.Fatal(got)
	}
}
