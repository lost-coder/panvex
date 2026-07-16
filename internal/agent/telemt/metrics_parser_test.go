package telemt

import (
	"strings"
	"testing"
)

func TestParseMetricsSnapshotCapturesUpstreamCounters(t *testing.T) {
	payload := strings.Join([]string{
		"telemt_uptime_seconds 123.4",
		"telemt_upstream_connect_attempt_total 1000",
		"telemt_upstream_connect_success_total 950",
		"telemt_upstream_connect_fail_total 40",
		"telemt_upstream_connect_failfast_hard_error_total 10",
	}, "\n")

	snap := ParseMetricsSnapshot(payload)

	if snap.UpstreamCounters.Attempt != 1000 {
		t.Fatalf("Attempt = %d, want 1000", snap.UpstreamCounters.Attempt)
	}
	if snap.UpstreamCounters.Success != 950 {
		t.Fatalf("Success = %d, want 950", snap.UpstreamCounters.Success)
	}
	if snap.UpstreamCounters.Fail != 40 {
		t.Fatalf("Fail = %d, want 40", snap.UpstreamCounters.Fail)
	}
	if snap.UpstreamCounters.Failfast != 10 {
		t.Fatalf("Failfast = %d, want 10", snap.UpstreamCounters.Failfast)
	}
}

func TestParseMetricsSnapshotIgnoresMalformedUpstreamCounter(t *testing.T) {
	snap := ParseMetricsSnapshot("telemt_upstream_connect_attempt_total notanumber\n")
	if snap.UpstreamCounters.Attempt != 0 {
		t.Fatalf("Attempt should remain 0 for unparsable line, got %d", snap.UpstreamCounters.Attempt)
	}
}

func TestParseMetricsSnapshotReadsTelemetrySuppressionMarkers(t *testing.T) {
	text := strings.Join([]string{
		"# TYPE telemt_telemetry_user_enabled gauge",
		"telemt_telemetry_user_enabled 0",
		"# TYPE telemt_telemetry_user_series_suppressed gauge",
		"telemt_telemetry_user_series_suppressed 1",
	}, "\n")
	snap := ParseMetricsSnapshot(text)
	if snap.UserTelemetryEnabled {
		t.Fatal("user_enabled 0 must parse as disabled")
	}
	if !snap.UserSeriesSuppressed {
		t.Fatal("series_suppressed 1 must parse as suppressed")
	}
}

func TestParseMetricsSnapshotTelemetryMarkersAbsentDefaultsHealthy(t *testing.T) {
	snap := ParseMetricsSnapshot("telemt_uptime_seconds 5\n")
	if !snap.UserTelemetryEnabled || snap.UserSeriesSuppressed {
		t.Fatalf("old Telemt without markers must default healthy: enabled=%v suppressed=%v",
			snap.UserTelemetryEnabled, snap.UserSeriesSuppressed)
	}
}

// TestParseMetricsSnapshotTelemetryMarkersHealthyValues covers the
// affirmative healthy case: markers present and reporting "all good".
func TestParseMetricsSnapshotTelemetryMarkersHealthyValues(t *testing.T) {
	text := strings.Join([]string{
		"telemt_telemetry_user_enabled 1",
		"telemt_telemetry_user_series_suppressed 0",
	}, "\n")
	snap := ParseMetricsSnapshot(text)
	if !snap.UserTelemetryEnabled || snap.UserSeriesSuppressed {
		t.Fatalf("enabled=1 suppressed=0 must be healthy: enabled=%v suppressed=%v",
			snap.UserTelemetryEnabled, snap.UserSeriesSuppressed)
	}
}

// TestParseMetricsSnapshotIgnoresMalformedTelemetryMarker mirrors
// TestParseMetricsSnapshotIgnoresMalformedUpstreamCounter: the line is
// consumed as ours, the field keeps its initialised value.
func TestParseMetricsSnapshotIgnoresMalformedTelemetryMarker(t *testing.T) {
	snap := ParseMetricsSnapshot("telemt_telemetry_user_enabled notanumber\n")
	if !snap.UserTelemetryEnabled {
		t.Fatal("malformed marker must leave UserTelemetryEnabled at its healthy default")
	}
}
