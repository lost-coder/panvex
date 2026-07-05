// Package updatehosts is the single source of truth for the hosts Panvex
// trusts to serve release artifacts. Both the control-plane self-update
// path and the agent updater consume it so the two host lists cannot drift
// (the drift that let release-assets.githubusercontent.com break self-update).
package updatehosts

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
)

// EnvAllowedHosts overrides the default download host allow-list with a
// comma-separated list, or the single sentinel "*" to disable the host
// check entirely (any https host is accepted). HTTPS-only and archive-size
// caps are enforced independently and are never lifted by this variable.
const EnvAllowedHosts = "PANVEX_UPDATE_ALLOWED_HOSTS"

// wildcard is the sentinel that disables the host allow-list.
const wildcard = "*"

// githubHosts is the canonical set of GitHub hosts a release download may
// legitimately hit: the union of the API path (api.github.com, codeload)
// and the asset-download path (objects/raw/release-assets). github.com
// issues the initial request and 302s to the CDN hosts.
var githubHosts = []string{
	"github.com",
	"api.github.com",
	"raw.githubusercontent.com",
	"objects.githubusercontent.com",
	"release-assets.githubusercontent.com",
	"codeload.github.com",
}

// IsDefaultHost reports whether host is one of the canonical GitHub hosts,
// independent of the active policy. Drives the "download to a non-GitHub
// host" warning.
func IsDefaultHost(host string) bool {
	h := strings.ToLower(host)
	for _, g := range githubHosts {
		if h == g {
			return true
		}
	}
	return false
}

// WarnIfNonDefault logs a WARN when rawURL's host is not a canonical GitHub
// host, so an operator who disabled the allow-list (or configured a mirror)
// sees each off-GitHub download. No-op when logger is nil or the URL has no
// parseable host (validation happens at the call site).
func WarnIfNonDefault(ctx context.Context, logger *slog.Logger, label, rawURL string) {
	if logger == nil {
		return
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || IsDefaultHost(u.Hostname()) {
		return
	}
	logger.WarnContext(ctx,
		"update download to non-GitHub host (allow-list disabled or mirror configured)",
		"label", label, "host", u.Hostname())
}
