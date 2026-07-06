package server

import (
	"context"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/security"
)

const (
	testMaxInMemoryMetricSnapshots = 512
	testAuditFirstPageLimit        = 1024
)

// TestServerServesRecentMetricSnapshotsFromStore asserts the A2 guarantee:
// after more than the cap is written, the store-backed read path serves
// exactly the most recent testMaxInMemoryMetricSnapshots snapshots in
// oldest→newest order. This is the same last-N window the removed in-memory
// ring used to enforce; it is now the store query's LIMIT 512 responsibility.
func TestServerServesRecentMetricSnapshotsFromStore(t *testing.T) {
	now := time.Date(2026, time.March, 21, 9, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "ams-1",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}
	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}
	// issueEnrollmentToken auto-created the "ams-1" fleet group with a
	// fresh UUID; subsequent snapshots must carry the canonical id so
	// the FK on agents.fleet_group_id resolves.
	fleetGroupID := resolveTestFleetGroupID(t, server.store, "ams-1")

	totalSnapshots := testMaxInMemoryMetricSnapshots + 3
	for index := 0; index < totalSnapshots; index++ {
		if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
			AgentID:      identity.AgentID,
			NodeName:     "node-a",
			FleetGroupID: fleetGroupID,
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

	// Flush the async batch writer so every snapshot reaches the store, then
	// read back through the same path /api/metrics uses.
	server.batchWriter.Flush(context.Background())
	metrics, err := server.listMetricSnapshots(context.Background())
	if err != nil {
		t.Fatalf("listMetricSnapshots() error = %v", err)
	}
	metricsLen := len(metrics)
	first := metrics[0]
	last := metrics[len(metrics)-1]

	if metricsLen != testMaxInMemoryMetricSnapshots {
		t.Fatalf("len(metrics) = %d, want %d", metricsLen, testMaxInMemoryMetricSnapshots)
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

// TestServerServesRecentAuditEventsFromStore asserts the A2 guarantee for the
// audit trail: after more than the cap is written, the store-backed first-page
// read serves exactly the most recent testAuditFirstPageLimit events in
// oldest→newest order. This is the same last-N window the removed in-memory
// ring used to enforce; it is now the store query's LIMIT 1024 responsibility.
func TestServerServesRecentAuditEventsFromStore(t *testing.T) {
	start := time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)
	// Atomic clock so each appended event gets a distinct, ascending timestamp
	// without racing the batch-writer goroutine, which calls Now() concurrently
	// while we advance it (the shared testServerWithSQLite helper captures `now`
	// by value, stamping every event identically).
	var nowPtr atomic.Pointer[time.Time]
	setNow := func(at time.Time) { tt := at; nowPtr.Store(&tt) }
	setNow(start)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return *nowPtr.Load() },
		Store:            store,
	})
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})

	totalEvents := testAuditFirstPageLimit + 4
	for index := 0; index < totalEvents; index++ {
		setNow(start.Add(time.Duration(index+1) * time.Second))
		server.appendAudit("user-1", "action-"+strconv.Itoa(index), "target-1", nil)
	}

	// Flush the async batch writer so every event reaches the store, then read
	// back through the same first-page path /api/audit uses. A bare
	// auditEvents.Drain races the background flush loop (batch size 50 keeps it
	// firing): it persists only its own swapped batch, not an in-flight
	// background drain, so a read right after can short-count. StopWithTimeout
	// is the real barrier — it halts the loop, waits for any in-flight drain
	// (wg.Wait), then does a final synchronous flush. Idempotent, so the
	// t.Cleanup Close() calling it again is fine.
	if err := server.batchWriter.StopWithTimeout(context.Background(), 10*time.Second); err != nil {
		t.Fatalf("batchWriter.StopWithTimeout() error = %v", err)
	}
	trail, err := server.auditFirstPage(context.Background())
	if err != nil {
		t.Fatalf("auditFirstPage() error = %v", err)
	}
	auditLen := len(trail)
	first := trail[0]
	last := trail[len(trail)-1]

	if auditLen != testAuditFirstPageLimit {
		t.Fatalf("len(trail) = %d, want %d", auditLen, testAuditFirstPageLimit)
	}
	expectedFirstAction := "action-" + strconv.Itoa(totalEvents-testAuditFirstPageLimit)
	if first.Action != expectedFirstAction {
		t.Fatalf("first audit action = %q, want %q", first.Action, expectedFirstAction)
	}
	expectedLastAction := "action-" + strconv.Itoa(totalEvents-1)
	if last.Action != expectedLastAction {
		t.Fatalf("last audit action = %q, want %q", last.Action, expectedLastAction)
	}

	// Verify ascending chronological order across the full first page.
	for i := 1; i < len(trail); i++ {
		if trail[i-1].CreatedAt.After(trail[i].CreatedAt) {
			t.Fatalf("trail[%d].CreatedAt (%v) not <= trail[%d].CreatedAt (%v)", i-1, trail[i-1].CreatedAt, i, trail[i].CreatedAt)
		}
	}
}
