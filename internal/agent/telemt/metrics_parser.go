package telemt

import (
	"strconv"
	"strings"
)

// UserMetrics holds per-user traffic and connection counters scraped from Telemt's Prometheus endpoint.
type UserMetrics struct {
	OctetsFromClient   uint64
	OctetsToClient     uint64
	CurrentConnections int
	UniqueIPsCurrent   int
}

// ParseUserMetrics parses a Prometheus text-format payload and returns per-user metrics.
// Only the four known telemt_user_* metric names are extracted; all other lines are ignored.
// The returned map is keyed by username and is never nil.
func ParseUserMetrics(text string) map[string]*UserMetrics {
	result := make(map[string]*UserMetrics)

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)

		// Skip comments and empty lines.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// We only care about lines that carry a {user="..."} label.
		userStart := strings.Index(line, `{user="`)
		if userStart == -1 {
			continue
		}

		// Extract metric name (everything before the label set).
		metricName := line[:userStart]

		// Verify it is one of the four supported names.
		switch metricName {
		case "telemt_user_octets_from_client",
			"telemt_user_octets_to_client",
			"telemt_user_connections_current",
			"telemt_user_unique_ips_current":
		default:
			continue
		}

		// Extract username: content between {user=" and the closing "}.
		rest := line[userStart+len(`{user="`):]
		quoteEnd := strings.Index(rest, `"`)
		if quoteEnd == -1 {
			continue
		}
		username := rest[:quoteEnd]
		if username == "" {
			continue
		}

		// Extract value: the token after the closing "} separator.
		afterLabel := rest[quoteEnd+1:]
		// afterLabel should start with "} <value>"
		braceClose := strings.Index(afterLabel, "}")
		if braceClose == -1 {
			continue
		}
		valuePart := strings.TrimSpace(afterLabel[braceClose+1:])
		// Strip any inline comment or timestamp (take first token).
		if spaceIdx := strings.IndexAny(valuePart, " \t"); spaceIdx != -1 {
			valuePart = valuePart[:spaceIdx]
		}

		// Ensure the user entry exists.
		if _, ok := result[username]; !ok {
			result[username] = &UserMetrics{}
		}
		m := result[username]

		switch metricName {
		case "telemt_user_octets_from_client":
			if v, err := strconv.ParseUint(valuePart, 10, 64); err == nil {
				m.OctetsFromClient = v
			}
		case "telemt_user_octets_to_client":
			if v, err := strconv.ParseUint(valuePart, 10, 64); err == nil {
				m.OctetsToClient = v
			}
		case "telemt_user_connections_current":
			if v, err := strconv.Atoi(valuePart); err == nil {
				m.CurrentConnections = v
			}
		case "telemt_user_unique_ips_current":
			if v, err := strconv.Atoi(valuePart); err == nil {
				m.UniqueIPsCurrent = v
			}
		}
	}

	return result
}
