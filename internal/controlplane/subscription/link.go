// Package subscription holds pure, dependency-free helpers for rendering a
// client's public subscription page: parsing Telegram proxy links into
// display labels. No storage or server imports — keep it unit-testable.
package subscription

import (
	"net/url"
	"strings"
)

// Link is a parsed Telegram MTProto proxy connection link.
type Link struct {
	Raw    string // original link, used verbatim for the "Add to Telegram" button
	Domain string // server host
	Port   string // server port
	Mode   string // "FakeTLS" | "Secure" | "Classic"
}

// ParseLink extracts the display fields from a tg://proxy or https://t.me/proxy
// link. Returns ok=false for unparseable links or links missing server/secret,
// so the caller can skip them rather than render a broken row.
func ParseLink(raw string) (Link, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, false
	}
	q := u.Query()
	server := strings.TrimSpace(q.Get("server"))
	secret := strings.TrimSpace(q.Get("secret"))
	if server == "" || secret == "" {
		return Link{}, false
	}
	return Link{
		Raw:    raw,
		Domain: server,
		Port:   strings.TrimSpace(q.Get("port")),
		Mode:   modeFromSecret(secret),
	}, true
}

// modeFromSecret maps the Telemt obfuscation mode from the secret prefix:
// "ee" => Fake TLS, "dd" => Secure (random padding), anything else => Classic.
func modeFromSecret(secret string) string {
	switch {
	case strings.HasPrefix(secret, "ee"):
		return "FakeTLS"
	case strings.HasPrefix(secret, "dd"):
		return "Secure"
	default:
		return "Classic"
	}
}
