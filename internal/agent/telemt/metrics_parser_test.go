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
