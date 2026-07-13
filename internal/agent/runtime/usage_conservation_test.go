package runtime

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
)

// TestAgentUsageConservationUnderTelemtRestarts drives BuildUsageSnapshot
// with a fake Telemt whose per-client counter random-walks upward and
// restarts (counter reset + uptime rewind) twice mid-run. The last
// emitted cumulative total must equal EXACTLY the sum of increments
// generated after the process baseline tick — the agent neither loses
// nor double-counts local traffic — and emitted totals must never
// decrease within one process epoch (P4 conservation, spec «Готово,
// когда»).
func TestAgentUsageConservationUnderTelemtRestarts(t *testing.T) {
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic test data
	client := &fakeTelemtClient{
		metricsUsage: []telemt.ClientUsage{
			{ClientID: "client-1", TrafficUsedBytes: 987_654, ActiveTCPConns: 1},
		},
		metricsUptime: 3_600,
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)

	at := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)
	// Baseline tick: the pre-existing 987654 bytes belong to the previous
	// process epoch and must not be counted into this epoch's totals.
	if _, err := agent.BuildUsageSnapshot(context.Background(), at); err != nil {
		t.Fatalf("baseline tick: %v", err)
	}

	var expected uint64    // ground truth: bytes generated after baseline
	var lastEmitted uint64 // last total seen on the wire
	for tick := 1; tick <= 200; tick++ {
		at = at.Add(time.Minute)
		if tick == 70 || tick == 140 {
			// Telemt restart: uptime rewinds, counter resets to zero.
			client.metricsUptime = 1
			client.metricsUsage[0].TrafficUsedBytes = 0
		} else {
			client.metricsUptime += 60
		}
		inc := uint64(rng.Intn(50_000))
		client.metricsUsage[0].TrafficUsedBytes += inc
		expected += inc

		snap, err := agent.BuildUsageSnapshot(context.Background(), at)
		if err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
		if len(snap.Clients) == 0 {
			continue // zero delta, gauges unchanged — row legitimately skipped
		}
		total := snap.Clients[0].TrafficTotalBytes
		if total < lastEmitted {
			t.Fatalf("tick %d: emitted total rewound %d -> %d", tick, lastEmitted, total)
		}
		lastEmitted = total
	}

	if lastEmitted != expected {
		t.Fatalf("final emitted total = %d, want %d (conservation: emitted == generated)", lastEmitted, expected)
	}
	if got := agent.UsageTotalForTest("client-1"); got != expected {
		t.Fatalf("internal usageTotals = %d, want %d", got, expected)
	}
}

// TestAgentUsageNoDoubleCountOnClientFlap (R2, audit 2026-07-07 §1.3): a client
// that drops out of a single metrics sample (pruning its lastOctets tracker)
// and reappears within the SAME telemt run must not have its whole cumulative
// counter added to the total a second time.
func TestAgentUsageNoDoubleCountOnClientFlap(t *testing.T) {
	client := &fakeTelemtClient{
		metricsUsage: []telemt.ClientUsage{
			{ClientID: "client-1", TrafficUsedBytes: 0, ActiveTCPConns: 1},
		},
		metricsUptime: 3_600,
	}
	agent := New(Config{AgentID: "agent-1", NodeName: "node-a"}, client)
	at := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)

	// Baseline tick adopts the current counter (0) without counting it.
	if _, err := agent.BuildUsageSnapshot(context.Background(), at); err != nil {
		t.Fatalf("baseline: %v", err)
	}

	// Grow the cumulative counter to 100 — total tracks it.
	at = at.Add(time.Minute)
	client.metricsUsage[0].TrafficUsedBytes = 100
	if _, err := agent.BuildUsageSnapshot(context.Background(), at); err != nil {
		t.Fatalf("grow: %v", err)
	}
	if got := agent.UsageTotalForTest("client-1"); got != 100 {
		t.Fatalf("after grow: total = %d, want 100", got)
	}

	// Client drops out of one sample → cleanupStaleUsageStateLocked prunes
	// its lastOctets. usageTotals deliberately survives.
	at = at.Add(time.Minute)
	client.metricsUsage = nil
	if _, err := agent.BuildUsageSnapshot(context.Background(), at); err != nil {
		t.Fatalf("flap: %v", err)
	}

	// Client reappears within the SAME run (no uptime rewind) with a higher
	// cumulative. The gap delta (20) is lost, but the whole history (100)
	// must NOT be re-counted.
	at = at.Add(time.Minute)
	client.metricsUsage = []telemt.ClientUsage{
		{ClientID: "client-1", TrafficUsedBytes: 120, ActiveTCPConns: 1},
	}
	if _, err := agent.BuildUsageSnapshot(context.Background(), at); err != nil {
		t.Fatalf("reappear: %v", err)
	}
	if got := agent.UsageTotalForTest("client-1"); got != 100 {
		t.Fatalf("after flap+reappear: total = %d, want 100 (no double-count; was 220 pre-fix)", got)
	}

	// Forward accounting resumes from the re-baselined counter: +30 → 130.
	at = at.Add(time.Minute)
	client.metricsUsage[0].TrafficUsedBytes = 150
	if _, err := agent.BuildUsageSnapshot(context.Background(), at); err != nil {
		t.Fatalf("post-reappear grow: %v", err)
	}
	if got := agent.UsageTotalForTest("client-1"); got != 130 {
		t.Fatalf("after post-reappear grow: total = %d, want 130", got)
	}
}
