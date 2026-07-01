package updatehosts

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
)

// HostPolicy decides whether a download URL's host is trusted. Build it with
// PolicyFromEnv; it is immutable afterwards.
type HostPolicy struct {
	disabled bool
	allowed  map[string]struct{}
}

// PolicyFromEnv builds a HostPolicy from EnvAllowedHosts:
//   - "*"           -> disabled (any https host accepted)
//   - unset/empty   -> the default GitHubHosts() set
//   - "a.com,b.io"  -> exactly those hosts (trimmed, lower-cased)
//
// A lone "*" disables; a "*" token mixed into a list is ignored (the list is
// treated as explicit), so the switch cannot be tripped by accident.
func PolicyFromEnv() HostPolicy {
	raw := strings.TrimSpace(os.Getenv(EnvAllowedHosts))
	if raw == wildcard {
		return HostPolicy{disabled: true}
	}
	hosts := parseList(raw)
	if len(hosts) == 0 {
		hosts = githubHosts
	}
	set := make(map[string]struct{}, len(hosts))
	for _, h := range hosts {
		set[strings.ToLower(h)] = struct{}{}
	}
	return HostPolicy{allowed: set}
}

func parseList(raw string) []string {
	if raw == "" {
		return nil
	}
	out := make([]string, 0)
	for _, p := range strings.Split(raw, ",") {
		if t := strings.TrimSpace(p); t != "" && t != wildcard {
			out = append(out, t)
		}
	}
	return out
}

// Disabled reports whether the host allow-list has been turned off via "*".
// Even when disabled, https is still required by CheckURL.
func (p HostPolicy) Disabled() bool { return p.disabled }

// Hosts returns the sorted allowed host list, or nil when disabled. Used by
// the agent updater to populate its Config.
func (p HostPolicy) Hosts() []string {
	if p.disabled {
		return nil
	}
	out := make([]string, 0, len(p.allowed))
	for h := range p.allowed {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// CheckURL enforces https always and, unless the policy is disabled, that the
// URL host is in the allow-list. The host match is port-insensitive (uses
// u.Hostname(), stripping any explicit port), consistent with the agent
// updater and with IsDefaultHost/WarnIfNonDefault, which already use
// Hostname(). The "not in the allow-list" wording is kept for compatibility
// with existing self-update tests.
func (p HostPolicy) CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("url %q: only https is allowed", raw)
	}
	if p.disabled {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if _, ok := p.allowed[host]; !ok {
		return fmt.Errorf("url %q: host %q is not in the allow-list", raw, host)
	}
	return nil
}
