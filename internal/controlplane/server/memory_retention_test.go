package server

import (
	"strconv"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/security"
)

const (
	testMaxInMemoryMetricSnapshots = 512
	testMaxInMemoryAuditEvents     = 1024
)

func TestServerApplyAgentSnapshotKeepsRecentMetricSnapshotsInMemory(t *testing.T) {
	now := time.Date(2026, time.March, 21, 9, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	token, err := server.enrollment.IssueToken(security.EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("IssueToken() error = %v", err)
	}
	identity, err := server.enrollAgent(agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}

	totalSnapshots := testMaxInMemoryMetricSnapshots + 3
	for index := 0; index < totalSnapshots; index++ {
		if err := server.applyAgentSnapshot(agentSnapshot{
			AgentID:      identity.AgentID,
			NodeName:     "node-a",
			FleetGroupID: "ams-1",
			Version:      "1.0.0",
			Metrics: map[string]uint64{
				"requests_total": uint64(index),
			},
			Runtime:    gatewayRuntimeSnapshotForTest(),
			HasRuntime: true,
			ObservedAt: now.Add(time.Duration(index+10) * time.Second),
		}); err != nil {
			t.Fatalf("applyAgentSnapshot() error = %v", err)
		}
	}

	server.mu.RLock()
	metricsLen := len(server.metrics)
	first := server.metrics[0]
	last := server.metrics[len(server.metrics)-1]
	server.mu.RUnlock()

	if metricsLen != testMaxInMemoryMetricSnapshots {
		t.Fatalf("len(server.metrics) = %d, want %d", metricsLen, testMaxInMemoryMetricSnapshots)
	}
	expectedFirstValue := uint64(totalSnapshots - testMaxInMemoryMetricSnapshots)
	if first.Values["requests_total"] != expectedFirstValue {
		t.Fatalf("first metric requests_total = %d, want %d", first.Values["requests_total"], expectedFirstValue)
	}
	expectedLastValue := uint64(totalSnapshots - 1)
	if last.Values["requests_total"] != expectedLastValue {
		t.Fatalf("last metric requests_total = %d, want %d", last.Values["requests_total"], expectedLastValue)
	}
}

func TestServerAppendAuditKeepsRecentEventsInMemory(t *testing.T) {
	start := time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)
	now := start
	server := New(Options{
		Now: func() time.Time { return now },
	})

	totalEvents := testMaxInMemoryAuditEvents + 4
	for index := 0; index < totalEvents; index++ {
		now = start.Add(time.Duration(index+1) * time.Second)
		server.appendAudit("user-1", "action-"+strconv.Itoa(index), "target-1", nil)
	}

	server.mu.RLock()
	auditLen := len(server.auditTrail)
	first := server.auditTrail[0]
	last := server.auditTrail[len(server.auditTrail)-1]
	server.mu.RUnlock()

	if auditLen != testMaxInMemoryAuditEvents {
		t.Fatalf("len(server.auditTrail) = %d, want %d", auditLen, testMaxInMemoryAuditEvents)
	}
	expectedFirstAction := "action-" + strconv.Itoa(totalEvents-testMaxInMemoryAuditEvents)
	if first.Action != expectedFirstAction {
		t.Fatalf("first audit action = %q, want %q", first.Action, expectedFirstAction)
	}
	expectedLastAction := "action-" + strconv.Itoa(totalEvents-1)
	if last.Action != expectedLastAction {
		t.Fatalf("last audit action = %q, want %q", last.Action, expectedLastAction)
	}
}
