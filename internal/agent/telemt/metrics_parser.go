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
	// UniqueIPsCurrent is the count of IPs active right now (gauge
	// telemt_user_unique_ips_current). UniqueIPsRecentWindow is the count of
	// distinct IPs seen over the configured observation window
	// (telemt_user_unique_ips_recent_window) — i.e. "unique used", which the
	// panel surfaces as UniqueIPsUsed. They are distinct telemt signals and
	// must not be conflated (IN-H3).
	UniqueIPsCurrent      int
	UniqueIPsRecentWindow int
}

// UpstreamCounters holds global upstream connect counters scraped from Telemt.
// All fields are cumulative since process start and reset on Telemt restart.
type UpstreamCounters struct {
	Attempt  uint64
	Success  uint64
	Fail     uint64
	Failfast uint64
}

type MetricsSnapshot struct {
	Users            map[string]*UserMetrics
	UptimeSeconds    float64
	UpstreamCounters UpstreamCounters
	// UserTelemetryEnabled mirrors the label-less gauge
	// telemt_telemetry_user_enabled: false means Telemt is configured with
	// per-user telemetry switched off, so no telemt_user_* series exist at
	// all. Defaults to true when the marker is absent — an older Telemt that
	// does not export it is assumed healthy, so upgrading the agent alone
	// never spuriously warns.
	UserTelemetryEnabled bool
	// UserSeriesSuppressed mirrors telemt_telemetry_user_series_suppressed:
	// true when Telemt stopped emitting per-user series, either because
	// telemetry is off or because the 4096-user labeled-series cap truncated
	// them. Defaults to false (healthy) when the marker is absent.
	UserSeriesSuppressed bool
}

// ParseMetricsSnapshot parses a Prometheus text-format payload and returns per-user metrics plus process uptime.
func ParseMetricsSnapshot(text string) MetricsSnapshot {
	result := MetricsSnapshot{
		Users: make(map[string]*UserMetrics),
		// Healthy defaults: absence of the suppression markers means an
		// older Telemt build, not a suppressed node.
		UserTelemetryEnabled: true,
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if tryParseUptimeLine(line, &result) {
			continue
		}

		if tryParseUpstreamCounter(line, &result) {
			continue
		}

		if tryParseScalarGauge(line, "telemt_telemetry_user_enabled ", func(v float64) {
			result.UserTelemetryEnabled = v != 0
		}) {
			continue
		}

		if tryParseScalarGauge(line, "telemt_telemetry_user_series_suppressed ", func(v float64) {
			result.UserSeriesSuppressed = v != 0
		}) {
			continue
		}

		parseUserMetricLine(line, result.Users)
	}

	return result
}

// tryParseUpstreamCounter sets the matching field on result.UpstreamCounters
// when line is one of the four upstream connect counters and returns true.
//
// A malformed value (ParseUint fails) is deliberately tolerated: the field
// stays at its previous zero/value and the next scrape that produces a
// well-formed line will recover. This mirrors the user-metric and uptime
// branches above and keeps the parser tolerant of partial Telemt output.
// See TestParseMetricsSnapshotIgnoresMalformedUpstreamCounter for the
// behavioural contract. There is no logger plumbed into this function;
// adding one just to flag a transient malformed line would be more noise
// than signal.
func tryParseUpstreamCounter(line string, result *MetricsSnapshot) bool {
	type binding struct {
		prefix string
		apply  func(uint64)
	}
	bindings := []binding{
		{"telemt_upstream_connect_attempt_total ", func(v uint64) { result.UpstreamCounters.Attempt = v }},
		{"telemt_upstream_connect_success_total ", func(v uint64) { result.UpstreamCounters.Success = v }},
		{"telemt_upstream_connect_fail_total ", func(v uint64) { result.UpstreamCounters.Fail = v }},
		{"telemt_upstream_connect_failfast_hard_error_total ", func(v uint64) { result.UpstreamCounters.Failfast = v }},
	}
	for _, b := range bindings {
		if !strings.HasPrefix(line, b.prefix) {
			continue
		}
		valuePart := strings.TrimSpace(strings.TrimPrefix(line, b.prefix))
		if v, err := strconv.ParseUint(valuePart, 10, 64); err == nil {
			b.apply(v)
		}
		return true
	}
	return false
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

// tryParseScalarGauge applies the value of a label-less gauge line to apply
// and returns true when line belongs to prefix (which must include the
// trailing space, so the binding is exact and a longer metric name sharing
// the prefix cannot match).
//
// Malformed values follow the same contract as tryParseUpstreamCounter /
// tryParseUptimeLine: the line is still reported as consumed, apply is not
// called, and the field keeps its initialised value until a well-formed
// scrape arrives. See TestParseMetricsSnapshotIgnoresMalformedTelemetryMarker.
func tryParseScalarGauge(line, prefix string, apply func(float64)) bool {
	rest, ok := strings.CutPrefix(line, prefix)
	if !ok {
		return false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
	if err != nil {
		return true
	}
	apply(value)
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
		"telemt_user_unique_ips_current",
		"telemt_user_unique_ips_recent_window":
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
	case "telemt_user_unique_ips_recent_window":
		if v, err := strconv.Atoi(valuePart); err == nil {
			m.UniqueIPsRecentWindow = v
		}
	}
}
