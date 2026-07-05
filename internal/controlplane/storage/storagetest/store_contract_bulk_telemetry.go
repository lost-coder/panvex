package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runBulkTelemetryContract exercises the telemetry bulk-write helpers
// added by P6-6.1a (finding #10): multi-row/one-tx variants of the
// per-agent telemetry writers used by the server batch writer.
func runBulkTelemetryContract(t *testing.T, open OpenStore) {
	t.Helper()

	ctx := context.Background()
	ts := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)

	t.Run("PutTelemetryRuntimeCurrentBulk upserts and dedups last-wins", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		seedAgentForTelemetry(t, store, "agent-a")
		seedAgentForTelemetry(t, store, "agent-b")

		// Duplicate agent in one batch: the LAST occurrence must win on
		// both backends (Postgres would otherwise fail with SQLSTATE 21000).
		batch := []storage.TelemetryRuntimeCurrentRecord{
			{AgentID: "agent-a", ObservedAt: ts, RuntimeJSON: `{"v":1}`},
			{AgentID: "agent-b", ObservedAt: ts, RuntimeJSON: `{"v":10}`},
			{AgentID: "agent-a", ObservedAt: ts.Add(time.Minute), RuntimeJSON: `{"v":2}`},
		}
		if err := store.PutTelemetryRuntimeCurrentBulk(ctx, batch); err != nil {
			t.Fatalf("PutTelemetryRuntimeCurrentBulk: %v", err)
		}
		got, err := store.GetTelemetryRuntimeCurrent(ctx, "agent-a")
		if err != nil {
			t.Fatalf("GetTelemetryRuntimeCurrent: %v", err)
		}
		if got.RuntimeJSON != `{"v":2}` {
			t.Fatalf("RuntimeJSON = %q, want {\"v\":2} (last-wins)", got.RuntimeJSON)
		}
		// Second bulk call must UPDATE, not duplicate.
		if err := store.PutTelemetryRuntimeCurrentBulk(ctx, []storage.TelemetryRuntimeCurrentRecord{
			{AgentID: "agent-a", ObservedAt: ts.Add(2 * time.Minute), RuntimeJSON: `{"v":3}`},
		}); err != nil {
			t.Fatalf("second PutTelemetryRuntimeCurrentBulk: %v", err)
		}
		all, err := store.ListTelemetryRuntimeCurrent(ctx)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeCurrent: %v", err)
		}
		if len(all) != 2 {
			t.Fatalf("len(all) = %d, want 2 (upsert, no dup rows)", len(all))
		}
	})

	t.Run("ReplaceTelemetryRuntimeDCsBulk replaces per agent, empty slice clears", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		seedAgentForTelemetry(t, store, "agent-a")
		seedAgentForTelemetry(t, store, "agent-b")

		seed := func(agentID string, dc int) storage.TelemetryRuntimeDCRecord {
			return storage.TelemetryRuntimeDCRecord{
				AgentID: agentID, DC: dc, ObservedAt: ts,
				AvailableEndpoints: 3, AvailablePct: 100, RequiredWriters: 2,
				AliveWriters: 2, CoveragePct: 100, RTTMs: 12.5, Load: 0.4,
			}
		}
		// Seed both agents via the single-agent path.
		if err := store.ReplaceTelemetryRuntimeDCs(ctx, "agent-a", []storage.TelemetryRuntimeDCRecord{seed("agent-a", 1), seed("agent-a", 2)}); err != nil {
			t.Fatalf("seed agent-a: %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeDCs(ctx, "agent-b", []storage.TelemetryRuntimeDCRecord{seed("agent-b", 1)}); err != nil {
			t.Fatalf("seed agent-b: %v", err)
		}
		// Bulk replace: agent-a shrinks to one NEW dc, agent-b clears.
		err := store.ReplaceTelemetryRuntimeDCsBulk(ctx, map[string][]storage.TelemetryRuntimeDCRecord{
			"agent-a": {seed("agent-a", 5)},
			"agent-b": {},
		})
		if err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeDCsBulk: %v", err)
		}
		aRows, err := store.ListTelemetryRuntimeDCs(ctx, "agent-a")
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeDCs(a): %v", err)
		}
		if len(aRows) != 1 || aRows[0].DC != 5 {
			t.Fatalf("agent-a rows = %+v, want single DC=5", aRows)
		}
		bRows, err := store.ListTelemetryRuntimeDCs(ctx, "agent-b")
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeDCs(b): %v", err)
		}
		if len(bRows) != 0 {
			t.Fatalf("agent-b rows = %d, want 0 (empty slice clears)", len(bRows))
		}
	})

	t.Run("ReplaceTelemetryRuntimeUpstreamsBulk replaces per agent", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		seedAgentForTelemetry(t, store, "agent-a")

		up := func(agentID string, id int) storage.TelemetryRuntimeUpstreamRecord {
			return storage.TelemetryRuntimeUpstreamRecord{
				AgentID: agentID, UpstreamID: id, ObservedAt: ts,
				RouteKind: "direct", Address: "10.0.0.1:443", Healthy: true,
				Fails: 0, EffectiveLatencyMs: 8.5,
			}
		}
		if err := store.ReplaceTelemetryRuntimeUpstreams(ctx, "agent-a", []storage.TelemetryRuntimeUpstreamRecord{up("agent-a", 1), up("agent-a", 2)}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		err := store.ReplaceTelemetryRuntimeUpstreamsBulk(ctx, map[string][]storage.TelemetryRuntimeUpstreamRecord{
			"agent-a": {up("agent-a", 9)},
		})
		if err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeUpstreamsBulk: %v", err)
		}
		rows, err := store.ListTelemetryRuntimeUpstreams(ctx, "agent-a")
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(rows) != 1 || rows[0].UpstreamID != 9 {
			t.Fatalf("rows = %+v, want single UpstreamID=9", rows)
		}
	})

	t.Run("AppendTelemetryRuntimeEventsBulk mixes agents, upserts on (agent,seq)", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		seedAgentForTelemetry(t, store, "agent-a")
		seedAgentForTelemetry(t, store, "agent-b")

		batch := []storage.TelemetryRuntimeEventRecord{
			{AgentID: "agent-a", Sequence: 1, ObservedAt: ts, Timestamp: ts, EventType: "dc_down", Context: "dc=2", Severity: "warn"},
			{AgentID: "agent-b", Sequence: 1, ObservedAt: ts, Timestamp: ts, EventType: "restart", Context: "", Severity: "info"},
			// Duplicate (agent-a, seq 1) inside one batch: last-wins.
			{AgentID: "agent-a", Sequence: 1, ObservedAt: ts.Add(time.Second), Timestamp: ts.Add(time.Second), EventType: "dc_up", Context: "dc=2", Severity: "info"},
		}
		if err := store.AppendTelemetryRuntimeEventsBulk(ctx, batch); err != nil {
			t.Fatalf("AppendTelemetryRuntimeEventsBulk: %v", err)
		}
		aEvents, err := store.ListTelemetryRuntimeEvents(ctx, "agent-a", 10)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeEvents: %v", err)
		}
		if len(aEvents) != 1 || aEvents[0].EventType != "dc_up" {
			t.Fatalf("agent-a events = %+v, want single dc_up (last-wins)", aEvents)
		}
		bEvents, err := store.ListTelemetryRuntimeEvents(ctx, "agent-b", 10)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeEvents(b): %v", err)
		}
		if len(bEvents) != 1 {
			t.Fatalf("agent-b events = %d, want 1", len(bEvents))
		}
	})

	t.Run("PutTelemetryDiagnosticsCurrentBulk upserts with dedup", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		seedAgentForTelemetry(t, store, "agent-a")

		batch := []storage.TelemetryDiagnosticsCurrentRecord{
			{AgentID: "agent-a", ObservedAt: ts, State: "ok", SystemInfoJSON: `{"cpu":1}`},
			{AgentID: "agent-a", ObservedAt: ts.Add(time.Minute), State: "ok", SystemInfoJSON: `{"cpu":2}`},
		}
		if err := store.PutTelemetryDiagnosticsCurrentBulk(ctx, batch); err != nil {
			t.Fatalf("PutTelemetryDiagnosticsCurrentBulk: %v", err)
		}
		got, err := store.GetTelemetryDiagnosticsCurrent(ctx, "agent-a")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.SystemInfoJSON != `{"cpu":2}` {
			t.Fatalf("SystemInfoJSON = %q, want {\"cpu\":2}", got.SystemInfoJSON)
		}
	})

	t.Run("PutTelemetrySecurityInventoryCurrentBulk upserts with dedup", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		seedAgentForTelemetry(t, store, "agent-a")

		batch := []storage.TelemetrySecurityInventoryCurrentRecord{
			{AgentID: "agent-a", ObservedAt: ts, State: "ok", Enabled: true, EntriesTotal: 1, EntriesJSON: `[1]`},
			{AgentID: "agent-a", ObservedAt: ts.Add(time.Minute), State: "ok", Enabled: true, EntriesTotal: 2, EntriesJSON: `[1,2]`},
		}
		if err := store.PutTelemetrySecurityInventoryCurrentBulk(ctx, batch); err != nil {
			t.Fatalf("PutTelemetrySecurityInventoryCurrentBulk: %v", err)
		}
		got, err := store.GetTelemetrySecurityInventoryCurrent(ctx, "agent-a")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.EntriesTotal != 2 {
			t.Fatalf("EntriesTotal = %d, want 2", got.EntriesTotal)
		}
	})

	t.Run("bulk telemetry empty inputs are no-ops", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		if err := store.PutTelemetryRuntimeCurrentBulk(ctx, nil); err != nil {
			t.Fatalf("PutTelemetryRuntimeCurrentBulk(nil): %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeDCsBulk(ctx, nil); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeDCsBulk(nil): %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeUpstreamsBulk(ctx, nil); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeUpstreamsBulk(nil): %v", err)
		}
		if err := store.AppendTelemetryRuntimeEventsBulk(ctx, nil); err != nil {
			t.Fatalf("AppendTelemetryRuntimeEventsBulk(nil): %v", err)
		}
		if err := store.PutTelemetryDiagnosticsCurrentBulk(ctx, nil); err != nil {
			t.Fatalf("PutTelemetryDiagnosticsCurrentBulk(nil): %v", err)
		}
		if err := store.PutTelemetrySecurityInventoryCurrentBulk(ctx, nil); err != nil {
			t.Fatalf("PutTelemetrySecurityInventoryCurrentBulk(nil): %v", err)
		}
	})
}
