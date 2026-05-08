package webhooks

import "strings"

// matchesFilter reports whether action satisfies any of the
// dot-prefix patterns in filter. An empty filter matches every
// event (broadcast endpoint). A pattern ending in ".*" matches any
// action whose dot-namespaced prefix equals everything before the
// trailing ".*" (so "agent.*" matches "agent.unhealthy" but not
// "agentless.foo"). A pattern without ".*" must equal the action.
//
// Patterns are not regular expressions — keeping the language
// boring lets operators reason about which receivers fire without
// writing escape rules. Add cases as the audit grows; do not
// promote this to a regexp engine.
func matchesFilter(action string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, pat := range filter {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if strings.HasSuffix(pat, ".*") {
			prefix := strings.TrimSuffix(pat, ".*")
			if action == prefix {
				continue // "agent.*" must NOT match the bare "agent"
			}
			if strings.HasPrefix(action, prefix+".") {
				return true
			}
			continue
		}
		if action == pat {
			return true
		}
	}
	return false
}

// parseFilter splits a comma-separated CSV stored on
// webhook_endpoints.event_filter into the slice form the package
// uses internally. Trims whitespace; an empty input yields nil
// (which matchesFilter treats as match-all).
func parseFilter(csv string) []string {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
