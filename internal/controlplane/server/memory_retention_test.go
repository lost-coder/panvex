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

	server.metricsAuditMu.RLock()
	trail := server.snapshotAuditTrailLocked()
	server.metricsAuditMu.RUnlock()
	auditLen := len(trail)
	first := trail[0]
	last := trail[len(trail)-1]

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

// TestAuditTrailRingBuffer verifies P2-PERF-02: appending more than the
// maximum capacity keeps only the most recent maxInMemoryAuditEvents events
// and preserves their chronological order, without panic. The behaviour is
// identical to the previous slice-shift implementation but now runs in O(1)
// per append.
func TestAuditTrailRingBuffer(t *testing.T) {
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	now := start
	server := New(Options{
		Now: func() time.Time { return now },
	})

	const totalEvents = 2000
	for index := 0; index < totalEvents; index++ {
		now = start.Add(time.Duration(index+1) * time.Second)
		server.appendAudit("actor", "action-"+strconv.Itoa(index), "target", nil)
	}

	server.metricsAuditMu.RLock()
	trail := server.snapshotAuditTrailLocked()
	server.metricsAuditMu.RUnlock()

	if len(trail) != testMaxInMemoryAuditEvents {
		t.Fatalf("len(trail) = %d, want %d", len(trail), testMaxInMemoryAuditEvents)
	}
	expectedFirst := "action-" + strconv.Itoa(totalEvents-testMaxInMemoryAuditEvents)
	if trail[0].Action != expectedFirst {
		t.Fatalf("trail[0].Action = %q, want %q", trail[0].Action, expectedFirst)
	}
	expectedLast := "action-" + strconv.Itoa(totalEvents-1)
	if trail[len(trail)-1].Action != expectedLast {
		t.Fatalf("last action = %q, want %q", trail[len(trail)-1].Action, expectedLast)
	}

	// Verify strict chronological order across the full snapshot.
	for i := 1; i < len(trail); i++ {
		if !trail[i-1].CreatedAt.Before(trail[i].CreatedAt) && !trail[i-1].CreatedAt.Equal(trail[i].CreatedAt) {
			t.Fatalf("trail[%d].CreatedAt (%v) not <= trail[%d].CreatedAt (%v)", i-1, trail[i-1].CreatedAt, i, trail[i].CreatedAt)
		}
	}
}

// BenchmarkAuditTrailAppend asserts that the ring buffer append primitive
// itself has zero allocations once the ring is full. The old slice-shift
// implementation required an O(N) copy(s.auditTrail, s.auditTrail[1:]) on
// every overflow, which cannot inline or elide. The ring buffer append is
// pure pointer math — it must not allocate at all. Run with -benchmem.
//
// This benchmark targets the hot primitive only (appendAuditTrailLocked),
// not the full appendAudit path; the latter includes upstream allocations
// from newSequenceID + eventEnvelope interface boxing + publish snapshot
// that are out of scope for P2-PERF-02.
func BenchmarkAuditTrailAppend(b *testing.B) {
	server := &Server{}
	event := AuditEvent{
		ID:      "audit-1",
		ActorID: "actor",
		Action:  "action",
	}
	// Fill the ring so the benchmark measures the steady-state overwrite
	// path, not the initial fill phase.
	for i := 0; i < maxInMemoryAuditEvents; i++ {
		server.appendAuditTrailLocked(event)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.appendAuditTrailLocked(event)
	}
}
