package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runTelemetryContract extracts the telemetry current-state + detail-boost contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runTelemetryContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("telemetry current-state round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-telemetry-1",
			NodeName:     "telemt-a",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 28, 10, 1, 0, 0, time.UTC),
		}
		runtime := storage.TelemetryRuntimeCurrentRecord{
			AgentID:                   agent.ID,
			ObservedAt:                time.Date(2026, time.March, 28, 10, 2, 0, 0, time.UTC),
			State:                     "fresh",
			StateReason:               "",
			ReadOnly:                  false,
			AcceptingNewConnections:   true,
			MERuntimeReady:            true,
			ME2DCFallbackEnabled:      true,
			UseMiddleProxy:            false,
			StartupStatus:             "ready",
			StartupStage:              "steady_state",
			StartupProgressPct:        100,
			InitializationStatus:      "ready",
			Degraded:                  false,
			InitializationStage:       "steady_state",
			InitializationProgressPct: 100,
			TransportMode:             "direct",
			CurrentConnections:        120,
			CurrentConnectionsME:      70,
			CurrentConnectionsDirect:  50,
			ActiveUsers:               95,
			UptimeSeconds:             3600,
			ConnectionsTotal:          1024,
			ConnectionsBadTotal:       12,
			HandshakeTimeoutsTotal:    2,
			ConfiguredUsers:           4096,
			DCCoveragePct:             83,
			HealthyUpstreams:          2,
			TotalUpstreams:            3,
		}
		dcs := []storage.TelemetryRuntimeDCRecord{
			{
				AgentID:            agent.ID,
				DC:                 2,
				ObservedAt:         runtime.ObservedAt,
				AvailableEndpoints: 4,
				AvailablePct:       100,
				RequiredWriters:    6,
				AliveWriters:       5,
				CoveragePct:        83.3,
				RTTMs:              42,
				Load:               0.7,
			},
		}
		upstreams := []storage.TelemetryRuntimeUpstreamRecord{
			{
				AgentID:            agent.ID,
				UpstreamID:         1,
				ObservedAt:         runtime.ObservedAt,
				RouteKind:          "direct",
				Address:            "fra-core-01:443",
				Healthy:            true,
				Fails:              0,
				EffectiveLatencyMs: 19,
			},
		}
		events := []storage.TelemetryRuntimeEventRecord{
			{
				AgentID:    agent.ID,
				Sequence:   41,
				ObservedAt: runtime.ObservedAt,
				Timestamp:  time.Date(2026, time.March, 28, 10, 1, 30, 0, time.UTC),
				EventType:  "dc_quorum_warning",
				Context:    "DC 2 coverage dropped below quorum",
				Severity:   "warn",
			},
		}
		diagnostics := storage.TelemetryDiagnosticsCurrentRecord{
			AgentID:             agent.ID,
			ObservedAt:          time.Date(2026, time.March, 28, 10, 2, 30, 0, time.UTC),
			State:               "fresh",
			StateReason:         "",
			SystemInfoJSON:      `{"version":"1.0.0"}`,
			EffectiveLimitsJSON: `{"max_tcp_conns":4}`,
			SecurityPostureJSON: `{"read_only":false}`,
			MinimalAllJSON:      `{"enabled":true}`,
			MEPoolJSON:          `{"enabled":true}`,
		}
		security := storage.TelemetrySecurityInventoryCurrentRecord{
			AgentID:      agent.ID,
			ObservedAt:   time.Date(2026, time.March, 28, 10, 3, 0, 0, time.UTC),
			State:        "fresh",
			StateReason:  "",
			Enabled:      true,
			EntriesTotal: 2,
			EntriesJSON:  `["10.0.0.0/24","192.168.0.0/24"]`,
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}
		if err := store.PutTelemetryRuntimeCurrent(ctx, runtime); err != nil {
			t.Fatalf("PutTelemetryRuntimeCurrent() error = %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeDCs(ctx, agent.ID, dcs); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeDCs() error = %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeUpstreams(ctx, agent.ID, upstreams); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeUpstreams() error = %v", err)
		}
		if err := store.AppendTelemetryRuntimeEvents(ctx, agent.ID, events); err != nil {
			t.Fatalf("AppendTelemetryRuntimeEvents() error = %v", err)
		}
		if err := store.PutTelemetryDiagnosticsCurrent(ctx, diagnostics); err != nil {
			t.Fatalf("PutTelemetryDiagnosticsCurrent() error = %v", err)
		}
		if err := store.PutTelemetrySecurityInventoryCurrent(ctx, security); err != nil {
			t.Fatalf("PutTelemetrySecurityInventoryCurrent() error = %v", err)
		}

		storedRuntime, err := store.GetTelemetryRuntimeCurrent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("GetTelemetryRuntimeCurrent() error = %v", err)
		}
		if storedRuntime.CurrentConnections != runtime.CurrentConnections {
			t.Fatalf("GetTelemetryRuntimeCurrent() CurrentConnections = %d, want %d", storedRuntime.CurrentConnections, runtime.CurrentConnections)
		}

		storedRuntimes, err := store.ListTelemetryRuntimeCurrent(ctx)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeCurrent() error = %v", err)
		}
		if len(storedRuntimes) != 1 {
			t.Fatalf("len(ListTelemetryRuntimeCurrent()) = %d, want 1", len(storedRuntimes))
		}

		storedDCs, err := store.ListTelemetryRuntimeDCs(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeDCs() error = %v", err)
		}
		if len(storedDCs) != 1 {
			t.Fatalf("len(ListTelemetryRuntimeDCs()) = %d, want 1", len(storedDCs))
		}
		if storedDCs[0].CoveragePct != dcs[0].CoveragePct {
			t.Fatalf("ListTelemetryRuntimeDCs()[0].CoveragePct = %v, want %v", storedDCs[0].CoveragePct, dcs[0].CoveragePct)
		}

		storedUpstreams, err := store.ListTelemetryRuntimeUpstreams(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeUpstreams() error = %v", err)
		}
		if len(storedUpstreams) != 1 {
			t.Fatalf("len(ListTelemetryRuntimeUpstreams()) = %d, want 1", len(storedUpstreams))
		}
		if storedUpstreams[0].Address != upstreams[0].Address {
			t.Fatalf("ListTelemetryRuntimeUpstreams()[0].Address = %q, want %q", storedUpstreams[0].Address, upstreams[0].Address)
		}

		storedEvents, err := store.ListTelemetryRuntimeEvents(ctx, agent.ID, 10)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeEvents() error = %v", err)
		}
		if len(storedEvents) != 1 {
			t.Fatalf("len(ListTelemetryRuntimeEvents()) = %d, want 1", len(storedEvents))
		}
		if storedEvents[0].EventType != events[0].EventType {
			t.Fatalf("ListTelemetryRuntimeEvents()[0].EventType = %q, want %q", storedEvents[0].EventType, events[0].EventType)
		}

		storedDiagnostics, err := store.GetTelemetryDiagnosticsCurrent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("GetTelemetryDiagnosticsCurrent() error = %v", err)
		}
		if storedDiagnostics.SystemInfoJSON != diagnostics.SystemInfoJSON {
			t.Fatalf("GetTelemetryDiagnosticsCurrent() SystemInfoJSON = %q, want %q", storedDiagnostics.SystemInfoJSON, diagnostics.SystemInfoJSON)
		}

		storedSecurity, err := store.GetTelemetrySecurityInventoryCurrent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("GetTelemetrySecurityInventoryCurrent() error = %v", err)
		}
		if storedSecurity.EntriesTotal != security.EntriesTotal {
			t.Fatalf("GetTelemetrySecurityInventoryCurrent() EntriesTotal = %d, want %d", storedSecurity.EntriesTotal, security.EntriesTotal)
		}
	})

	t.Run("telemt_unreachable_round_trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC),
		}

		agentUnreachable := storage.AgentRecord{
			ID:           "agent-unreachable",
			NodeName:     "telemt-unreachable",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 28, 12, 1, 0, 0, time.UTC),
		}
		agentDefault := storage.AgentRecord{
			ID:           "agent-default",
			NodeName:     "telemt-default",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 28, 12, 2, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agentUnreachable); err != nil {
			t.Fatalf("PutAgent(unreachable) error = %v", err)
		}
		if err := store.PutAgent(ctx, agentDefault); err != nil {
			t.Fatalf("PutAgent(default) error = %v", err)
		}

		rec := storage.TelemetryRuntimeCurrentRecord{
			AgentID:                    "agent-unreachable",
			ObservedAt:                 time.Unix(1700000000, 0).UTC(),
			TelemtUnreachable:          true,
			TelemtUnreachableSinceUnix: 1699999970,
		}
		if err := store.PutTelemetryRuntimeCurrent(ctx, rec); err != nil {
			t.Fatalf("PutTelemetryRuntimeCurrent() error = %v", err)
		}
		got, err := store.GetTelemetryRuntimeCurrent(ctx, "agent-unreachable")
		if err != nil {
			t.Fatalf("GetTelemetryRuntimeCurrent() error = %v", err)
		}
		if !got.TelemtUnreachable {
			t.Fatal("TelemtUnreachable round-trip = false, want true")
		}
		if got.TelemtUnreachableSinceUnix != 1699999970 {
			t.Fatalf("TelemtUnreachableSinceUnix = %d, want 1699999970",
				got.TelemtUnreachableSinceUnix)
		}

		rec2 := storage.TelemetryRuntimeCurrentRecord{
			AgentID:    "agent-default",
			ObservedAt: time.Unix(1700000100, 0).UTC(),
		}
		if err := store.PutTelemetryRuntimeCurrent(ctx, rec2); err != nil {
			t.Fatalf("PutTelemetryRuntimeCurrent(default) error = %v", err)
		}
		got2, err := store.GetTelemetryRuntimeCurrent(ctx, "agent-default")
		if err != nil {
			t.Fatalf("GetTelemetryRuntimeCurrent(default) error = %v", err)
		}
		if got2.TelemtUnreachable {
			t.Fatal("TelemtUnreachable for default record = true, want false (healthy by default)")
		}
		if got2.TelemtUnreachableSinceUnix != 0 {
			t.Fatalf("TelemtUnreachableSinceUnix for default = %d, want 0", got2.TelemtUnreachableSinceUnix)
		}
	})

	// F4: detail boost is in-memory only on the panel and no longer
	// persisted, so there is no storage round-trip to contract-test.
}
