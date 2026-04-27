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

type MetricsSnapshot struct {
	Users         map[string]*UserMetrics
	UptimeSeconds float64
}

// ParseMetricsSnapshot parses a Prometheus text-format payload and returns per-user metrics plus process uptime.
func ParseMetricsSnapshot(text string) MetricsSnapshot {
	result := MetricsSnapshot{
		Users: make(map[string]*UserMetrics),
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if tryParseUptimeLine(line, &result) {
			continue
		}

		parseUserMetricLine(line, result.Users)
	}

	return result
}

// tryParseUptimeLine sets result.UptimeSeconds when line is the uptime
// metric and returns true; otherwise returns false so the caller can try
// other parsers.
func tryParseUptimeLine(line string, result *MetricsSnapshot) bool {
	const prefix = "telemt_uptime_seconds "
	if !strings.HasPrefix(line, prefix) {
		return false
	}
	valuePart := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if v, err := strconv.ParseFloat(valuePart, 64); err == nil {
		result.UptimeSeconds = v
	}
	return true
}

// parseUserMetricLine parses one telemt_user_* metric line and merges its
// value into the per-username map. Lines that do not match the expected
// shape are silently ignored.
func parseUserMetricLine(line string, users map[string]*UserMetrics) {
	userStart := strings.Index(line, `{user="`)
	if userStart == -1 {
		return
	}

	metricName := line[:userStart]
	switch metricName {
	case "telemt_user_octets_from_client",
		"telemt_user_octets_to_client",
		"telemt_user_connections_current",
		"telemt_user_unique_ips_current":
	default:
		return
	}

	username, valuePart, ok := extractUserMetricValue(line[userStart+len(`{user="`):])
	if !ok {
		return
	}

	if _, exists := users[username]; !exists {
		users[username] = &UserMetrics{}
	}
	applyUserMetric(users[username], metricName, valuePart)
}

// extractUserMetricValue parses the substring after `{user="` into the
// username and the trimmed numeric value part. ok is false when the
// label or value cannot be located.
func extractUserMetricValue(rest string) (username string, valuePart string, ok bool) {
	quoteEnd := strings.Index(rest, `"`)
	if quoteEnd == -1 {
		return "", "", false
	}
	username = rest[:quoteEnd]
	if username == "" {
		return "", "", false
	}

	afterLabel := rest[quoteEnd+1:]
	braceClose := strings.Index(afterLabel, "}")
	if braceClose == -1 {
		return "", "", false
	}
	valuePart = strings.TrimSpace(afterLabel[braceClose+1:])
	if spaceIdx := strings.IndexAny(valuePart, " \t"); spaceIdx != -1 {
		valuePart = valuePart[:spaceIdx]
	}
	return username, valuePart, true
}

// applyUserMetric updates the matching field on m for metricName. Unknown
// or unparsable values are silently ignored — same behaviour as before.
func applyUserMetric(m *UserMetrics, metricName, valuePart string) {
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

// ParseUserMetrics parses a Prometheus text-format payload and returns per-user metrics only.
func ParseUserMetrics(text string) map[string]*UserMetrics {
	return ParseMetricsSnapshot(text).Users
}
