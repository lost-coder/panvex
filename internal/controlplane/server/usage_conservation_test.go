package server

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
)

// TestPanelUsageConservationUnderLossReplayAndRestarts is the P4
// acceptance scenario (spec «Готово, когда»): a fake client with a real
// byte counter feeds cumulative reports through the full panel merge
// path while the transport misbehaves — snapshots get LOST, delivered
// snapshots get REPLAYED after reconnects, the PANEL restarts mid-epoch
// (mirror rehydrated from persisted rows), and the AGENT restarts into
// a fresh boot epoch. The accumulated traffic must equal the generated
// traffic EXACTLY: nothing lost, nothing double-counted.
//
// By-design boundary (not exercised here): bytes still undelivered when
// an agent epoch ends (the agent died holding the tail) are lost — the
// test always delivers the last report of each epoch, as a live agent
// does on its next tick.
func TestPanelUsageConservationUnderLossReplayAndRestarts(t *testing.T) {
	now := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	defer server.Close()

	const agentID = "agent-conserve"
	const clientID = "client-conserve"
	seedClientAndAgentRows(t, server, clientID, agentID, now)

	rng := rand.New(rand.NewSource(7)) //nolint:gosec // deterministic test data
	at := now
	deliver := func(bootID string, total uint64) {
		at = at.Add(time.Second)
		server.mu.Lock()
		server.applyClientUsageSnapshot(context.Background(), agentID, bootID,
			[]clients.UsageReport{{ClientID: clientID, TotalBytes: total, ObservedAt: at}})
		server.mu.Unlock()
	}

	var trueTotal uint64 // the fake client's ground-truth byte counter

	// Agent boot epoch 1: 30 ticks of random increments. Every 3rd tick
	// is LOST in transit; every 5th delivered tick is REPLAYED
	// (reconnect); the epoch's final tick is always delivered.
	var bootTotal uint64
	for tick := 1; tick <= 30; tick++ {
		inc := uint64(rng.Intn(10_000))
		bootTotal += inc
		trueTotal += inc
		if tick%3 == 0 && tick != 30 {
			continue // lost snapshot — the next delivered total catches up
		}
		deliver("boot-1", bootTotal)
		if tick%5 == 0 {
			deliver("boot-1", bootTotal) // replay after reconnect
		}
	}

	// PANEL restart mid-epoch: rehydrate the usage mirror (including the
	// watermark) from the persisted client_usage rows, then keep
	// receiving reports of the SAME agent epoch.
	if err := server.clientsSvc.Restore(context.Background()); err != nil {
		t.Fatalf("clientsSvc.Restore: %v", err)
	}
	for tick := 1; tick <= 10; tick++ {
		inc := uint64(rng.Intn(10_000))
		bootTotal += inc
		trueTotal += inc
		deliver("boot-1", bootTotal)
	}

	// AGENT restart: fresh boot epoch, counter back at zero. The first
	// report of the new epoch is immediately replayed (reconnect burst) —
	// the old protocol dropped this entire first batch (audit #8).
	bootTotal = 0
	for tick := 1; tick <= 20; tick++ {
		inc := uint64(rng.Intn(10_000))
		bootTotal += inc
		trueTotal += inc
		deliver("boot-2", bootTotal)
		if tick == 1 {
			deliver("boot-2", bootTotal)
		}
	}

	server.mu.Lock()
	got := mirrorUsage(server, clientID, agentID)
	server.mu.Unlock()
	if got.TrafficUsedBytes != trueTotal {
		t.Fatalf("accumulated = %d, want %d (conservation: accounted == actual)", got.TrafficUsedBytes, trueTotal)
	}
	if got.AgentBootID != "boot-2" || got.LastTotalBytes != bootTotal {
		t.Fatalf("final watermark = (%q, %d), want (boot-2, %d)", got.AgentBootID, got.LastTotalBytes, bootTotal)
	}
}
