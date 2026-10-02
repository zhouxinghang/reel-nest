package metadata

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"reelnest/internal/model"
)

// NormalizeMetaTubeURL gives equivalent configured API addresses one catalog
// space. An optional /v1 suffix is canonicalized to an explicit /v1, preserving
// case-sensitive reverse-proxy prefixes. Private HTTP services are supported.
// No DNS resolution or network request is performed while saving settings.
func NormalizeMetaTubeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 2048 || !utf8.ValidString(raw) || strings.ContainsAny(raw, "\\?#") || strings.ContainsFunc(raw, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return "", model.ErrInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", model.ErrInvalid
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", model.ErrInvalid
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		ip, err := netip.ParseAddr(host)
		if err != nil || !ip.Is6() || ip.Zone() != "" {
			return "", model.ErrInvalid
		}
		host = "[" + ip.String() + "]"
	} else if strings.ContainsAny(u.Host, "[]") || strings.ContainsFunc(host, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_')
	}) {
		return "", model.ErrInvalid
	}
	port := u.Port()
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", model.ErrInvalid
		}
		port = strconv.Itoa(n)
		if (u.Scheme == "https" && port == "443") || (u.Scheme == "http" && port == "80") {
			port = ""
		}
	}
	if strings.HasSuffix(u.Host, ":") {
		return "", model.ErrInvalid
	}
	u.Host = host
	if port != "" {
		u.Host += ":" + port
	}
	// Reject ambiguous traversal/encoded separators instead of silently moving
	// the authenticated API outside the administrator's selected prefix.
	if strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.ContainsAny(u.Path, "\\%?#") || strings.ContainsFunc(u.Path, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) {
		return "", model.ErrInvalid
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return "", model.ErrInvalid
		}
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if strings.Contains(u.Path, "//") {
		return "", model.ErrInvalid
	}
	if !strings.HasSuffix(u.Path, "/v1") {
		u.Path += "/v1"
	}
	u.RawPath = ""
	result := u.String()
	if len(result) > 2048 {
		return "", model.ErrInvalid
	}
	return result, nil
}
