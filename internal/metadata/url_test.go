package metadata_test

import (
	"errors"
	"strings"
	"testing"

	"reelnest/internal/metadata"
	"reelnest/internal/model"
)

func TestMetaTubeURLNormalizationIsIdempotentAndBounded(t *testing.T) {
	for input, want := range map[string]string{
		"": "", "  ": "",
		"http://NAS:80/":                 "http://nas/v1",
		"https://EXAMPLE.COM:443/Proxy":  "https://example.com/Proxy/v1",
		"https://example.com/Proxy/v1/":  "https://example.com/Proxy/v1",
		"http://127.0.0.1:08081/prefix/": "http://127.0.0.1:8081/prefix/v1",
		"http://[::1]:80":                "http://[::1]/v1",
		"http://[::FFFF:c000:201]:80":    "http://[::ffff:192.0.2.1]/v1",
		"http://[::1]:8080/Proxy/v1/v1":  "http://[::1]:8080/Proxy/v1/v1",
		"https://example.com/%70refix":   "https://example.com/prefix/v1",
	} {
		got, err := metadata.NormalizeMetaTubeURL(input)
		if err != nil || got != want {
			t.Fatalf("%q: got %q, want %q, err %v", input, got, want, err)
		}
		if twice, err := metadata.NormalizeMetaTubeURL(got); err != nil || twice != got {
			t.Fatal("normalization not idempotent", got, twice, err)
		}
	}
	for _, input := range []string{
		"ftp://example.com", "https:example.com", "//example.com", "https:///v1", "http://",
		"https://user:secret@example.com", "https://example.com?", "https://example.com#", "https://example.com?token=x",
		"http://example.com:0", "http://example.com:65536", "http://example.com:", "http://example.com:-1",
		"http://[not-ip]/", "http://[127.0.0.1]/", "http://[fe80::1%25lo0]/",
		"http://example.com/p/../v1", "http://example.com/./v1", "http://example.com/p/%2e%2e/v1",
		"http://example.com/p%2fv1", "http://example.com/p%5cv1", "http://example.com/p%00v1",
		"http://example.com/p%20v1", "http://example.com/p\\v1", "http://example.com/p//v1",
		"http://example.com/p%252f/v1", "http://example.com/p%3fv1", "http://example.com/p%23v1",
		"http://exa mple.com", "http://example.com/p\nv1", "http://example.com/" + strings.Repeat("a", 2048),
	} {
		if got, err := metadata.NormalizeMetaTubeURL(input); !errors.Is(err, model.ErrInvalid) {
			t.Errorf("accepted unsafe/ambiguous URL %q -> %q: %v", input, got, err)
		}
	}
}
