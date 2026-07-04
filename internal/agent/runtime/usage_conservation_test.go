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
