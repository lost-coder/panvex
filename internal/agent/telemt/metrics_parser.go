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

		if strings.HasPrefix(line, "telemt_uptime_seconds ") {
			valuePart := strings.TrimSpace(strings.TrimPrefix(line, "telemt_uptime_seconds "))
			if v, err := strconv.ParseFloat(valuePart, 64); err == nil {
				result.UptimeSeconds = v
			}
			continue
		}

		userStart := strings.Index(line, `{user="`)
		if userStart == -1 {
			continue
		}

		metricName := line[:userStart]
		switch metricName {
		case "telemt_user_octets_from_client",
			"telemt_user_octets_to_client",
			"telemt_user_connections_current",
			"telemt_user_unique_ips_current":
		default:
			continue
		}

		rest := line[userStart+len(`{user="`):]
		quoteEnd := strings.Index(rest, `"`)
		if quoteEnd == -1 {
			continue
		}
		username := rest[:quoteEnd]
		if username == "" {
			continue
		}

		afterLabel := rest[quoteEnd+1:]
		braceClose := strings.Index(afterLabel, "}")
		if braceClose == -1 {
			continue
		}
		valuePart := strings.TrimSpace(afterLabel[braceClose+1:])
		if spaceIdx := strings.IndexAny(valuePart, " \t"); spaceIdx != -1 {
			valuePart = valuePart[:spaceIdx]
		}

		if _, ok := result.Users[username]; !ok {
			result.Users[username] = &UserMetrics{}
		}
		m := result.Users[username]

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

// ParseUserMetrics parses a Prometheus text-format payload and returns per-user metrics only.
func ParseUserMetrics(text string) map[string]*UserMetrics {
	return ParseMetricsSnapshot(text).Users
}
