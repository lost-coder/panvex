package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// seedAgentForTelemetry inserts the fleet group + agent row that
// telemt_runtime_current's FK requires, so a runtime-current sub-test can
// Put a row for agentID. Mirrors the inline seeding the other blocks do.
func seedAgentForTelemetry(t *testing.T, store storage.Store, agentID string) {
	t.Helper()
	ctx := context.Background()
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        testFleetGroupID,
		Name:      "Default",
		CreatedAt: time.Date(2026, time.July, 2, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seedAgentForTelemetry: PutFleetGroup() error = %v", err)
	}
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           agentID,
		NodeName:     agentID,
		FleetGroupID: testFleetGroupID,
		Version:      "dev",
		LastSeenAt:   time.Date(2026, time.July, 2, 9, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("seedAgentForTelemetry: PutAgent() error = %v", err)
	}
}

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
			AgentID:     agent.ID,
			ObservedAt:  time.Date(2026, time.March, 28, 10, 2, 0, 0, time.UTC),
			RuntimeJSON: `{"current_connections":120,"active_users":95,"transport_mode":"direct"}`,
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
		if storedRuntime.RuntimeJSON != runtime.RuntimeJSON {
			t.Fatalf("GetTelemetryRuntimeCurrent() RuntimeJSON = %q, want %q", storedRuntime.RuntimeJSON, runtime.RuntimeJSON)
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

		// The unreachable flag now lives inside the runtime_json blob (the
		// storage layer is opaque to its shape); round-trip it verbatim.
		const unreachableJSON = `{"telemt_unreachable":true,"telemt_unreachable_since_unix":1699999970}`
		rec := storage.TelemetryRuntimeCurrentRecord{
			AgentID:     "agent-unreachable",
			ObservedAt:  time.Unix(1700000000, 0).UTC(),
			RuntimeJSON: unreachableJSON,
		}
		if err := store.PutTelemetryRuntimeCurrent(ctx, rec); err != nil {
			t.Fatalf("PutTelemetryRuntimeCurrent() error = %v", err)
		}
		got, err := store.GetTelemetryRuntimeCurrent(ctx, "agent-unreachable")
		if err != nil {
			t.Fatalf("GetTelemetryRuntimeCurrent() error = %v", err)
		}
		if got.RuntimeJSON != unreachableJSON {
			t.Fatalf("RuntimeJSON round-trip = %q, want %q", got.RuntimeJSON, unreachableJSON)
		}

		const healthyJSON = `{"telemt_unreachable":false}`
		rec2 := storage.TelemetryRuntimeCurrentRecord{
			AgentID:     "agent-default",
			ObservedAt:  time.Unix(1700000100, 0).UTC(),
			RuntimeJSON: healthyJSON,
		}
		if err := store.PutTelemetryRuntimeCurrent(ctx, rec2); err != nil {
			t.Fatalf("PutTelemetryRuntimeCurrent(default) error = %v", err)
		}
		got2, err := store.GetTelemetryRuntimeCurrent(ctx, "agent-default")
		if err != nil {
			t.Fatalf("GetTelemetryRuntimeCurrent(default) error = %v", err)
		}
		if got2.RuntimeJSON != healthyJSON {
			t.Fatalf("RuntimeJSON(default) round-trip = %q, want %q", got2.RuntimeJSON, healthyJSON)
		}
	})

	// F4: detail boost is in-memory only on the panel and no longer
	// persisted, so there is no storage round-trip to contract-test.

	t.Run("telemetry bulk rehydration per-agent windows", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        testFleetGroupID,
			Name:      "Default",
			CreatedAt: time.Date(2026, time.April, 1, 9, 0, 0, 0, time.UTC),
		}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}

		base := time.Date(2026, time.April, 1, 9, 5, 0, 0, time.UTC)
		agentA := storage.AgentRecord{ID: "agent-bulk-a", NodeName: "bulk-a", FleetGroupID: group.ID, Version: "dev", LastSeenAt: base}
		agentB := storage.AgentRecord{ID: "agent-bulk-b", NodeName: "bulk-b", FleetGroupID: group.ID, Version: "dev", LastSeenAt: base}
		for _, agent := range []storage.AgentRecord{agentA, agentB} {
			if err := store.PutAgent(ctx, agent); err != nil {
				t.Fatalf("PutAgent(%s) error = %v", agent.ID, err)
			}
			if err := store.PutTelemetryRuntimeCurrent(ctx, storage.TelemetryRuntimeCurrentRecord{
				AgentID:     agent.ID,
				ObservedAt:  base,
				RuntimeJSON: `{}`,
			}); err != nil {
				t.Fatalf("PutTelemetryRuntimeCurrent(%s) error = %v", agent.ID, err)
			}
		}

		// Agent A: 2 DCs, 2 upstreams, 15 events. Agent B: 1 DC, 1 upstream, 3 events.
		if err := store.ReplaceTelemetryRuntimeDCs(ctx, agentA.ID, []storage.TelemetryRuntimeDCRecord{
			{AgentID: agentA.ID, DC: 1, ObservedAt: base, CoveragePct: 90},
			{AgentID: agentA.ID, DC: 2, ObservedAt: base, CoveragePct: 80},
		}); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeDCs(A) error = %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeDCs(ctx, agentB.ID, []storage.TelemetryRuntimeDCRecord{
			{AgentID: agentB.ID, DC: 5, ObservedAt: base, CoveragePct: 70},
		}); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeDCs(B) error = %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeUpstreams(ctx, agentA.ID, []storage.TelemetryRuntimeUpstreamRecord{
			{AgentID: agentA.ID, UpstreamID: 1, ObservedAt: base, RouteKind: "direct", Address: "a-1:443", Healthy: true},
			{AgentID: agentA.ID, UpstreamID: 2, ObservedAt: base, RouteKind: "direct", Address: "a-2:443", Healthy: false},
		}); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeUpstreams(A) error = %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeUpstreams(ctx, agentB.ID, []storage.TelemetryRuntimeUpstreamRecord{
			{AgentID: agentB.ID, UpstreamID: 9, ObservedAt: base, RouteKind: "direct", Address: "b-9:443", Healthy: true},
		}); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeUpstreams(B) error = %v", err)
		}

		eventsA := make([]storage.TelemetryRuntimeEventRecord, 0, 15)
		for i := 0; i < 15; i++ {
			eventsA = append(eventsA, storage.TelemetryRuntimeEventRecord{
				AgentID:    agentA.ID,
				Sequence:   int64(i + 1),
				ObservedAt: base,
				Timestamp:  base.Add(time.Duration(i) * time.Minute),
				EventType:  "tick",
				Context:    "a",
				Severity:   "ok",
			})
		}
		if err := store.AppendTelemetryRuntimeEvents(ctx, agentA.ID, eventsA); err != nil {
			t.Fatalf("AppendTelemetryRuntimeEvents(A) error = %v", err)
		}
		eventsB := make([]storage.TelemetryRuntimeEventRecord, 0, 3)
		for i := 0; i < 3; i++ {
			eventsB = append(eventsB, storage.TelemetryRuntimeEventRecord{
				AgentID:    agentB.ID,
				Sequence:   int64(i + 1),
				ObservedAt: base,
				Timestamp:  base.Add(time.Duration(i) * time.Minute),
				EventType:  "tick",
				Context:    "b",
				Severity:   "ok",
			})
		}
		if err := store.AppendTelemetryRuntimeEvents(ctx, agentB.ID, eventsB); err != nil {
			t.Fatalf("AppendTelemetryRuntimeEvents(B) error = %v", err)
		}

		// Bulk current: both agents.
		allCurrent, err := store.ListTelemetryRuntimeCurrent(ctx)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeCurrent() error = %v", err)
		}
		if len(allCurrent) != 2 {
			t.Fatalf("len(ListTelemetryRuntimeCurrent()) = %d, want 2", len(allCurrent))
		}

		// Bulk DCs grouped.
		allDCs, err := store.ListAllTelemetryRuntimeDCs(ctx)
		if err != nil {
			t.Fatalf("ListAllTelemetryRuntimeDCs() error = %v", err)
		}
		dcsByAgent := map[string]int{}
		for _, dc := range allDCs {
			dcsByAgent[dc.AgentID]++
		}
		if dcsByAgent[agentA.ID] != 2 || dcsByAgent[agentB.ID] != 1 {
			t.Fatalf("ListAllTelemetryRuntimeDCs() per-agent counts = %v, want A=2 B=1", dcsByAgent)
		}

		// Bulk upstreams grouped.
		allUps, err := store.ListAllTelemetryRuntimeUpstreams(ctx)
		if err != nil {
			t.Fatalf("ListAllTelemetryRuntimeUpstreams() error = %v", err)
		}
		upsByAgent := map[string]int{}
		for _, up := range allUps {
			upsByAgent[up.AgentID]++
		}
		if upsByAgent[agentA.ID] != 2 || upsByAgent[agentB.ID] != 1 {
			t.Fatalf("ListAllTelemetryRuntimeUpstreams() per-agent counts = %v, want A=2 B=1", upsByAgent)
		}

		// Bulk events with PER-AGENT limit of 10: agent A keeps its 10
		// most recent (sequences 6..15), agent B keeps all 3.
		allEvents, err := store.ListAllTelemetryRuntimeEventsPerAgent(ctx, 10)
		if err != nil {
			t.Fatalf("ListAllTelemetryRuntimeEventsPerAgent() error = %v", err)
		}
		eventsByAgent := map[string][]storage.TelemetryRuntimeEventRecord{}
		for _, ev := range allEvents {
			eventsByAgent[ev.AgentID] = append(eventsByAgent[ev.AgentID], ev)
		}
		if len(eventsByAgent[agentA.ID]) != 10 {
			t.Fatalf("per-agent events for A = %d, want 10 (most-recent window)", len(eventsByAgent[agentA.ID]))
		}
		if len(eventsByAgent[agentB.ID]) != 3 {
			t.Fatalf("per-agent events for B = %d, want 3", len(eventsByAgent[agentB.ID]))
		}
		// The 10 returned for A must be the newest (sequences 6..15); the
		// oldest five (sequences 1..5) must be excluded.
		seenSeq := map[int64]bool{}
		for _, ev := range eventsByAgent[agentA.ID] {
			seenSeq[ev.Sequence] = true
		}
		for seq := int64(1); seq <= 5; seq++ {
			if seenSeq[seq] {
				t.Fatalf("ListAllTelemetryRuntimeEventsPerAgent(10) returned stale sequence %d for A; want only 6..15", seq)
			}
		}
		for seq := int64(6); seq <= 15; seq++ {
			if !seenSeq[seq] {
				t.Fatalf("ListAllTelemetryRuntimeEventsPerAgent(10) missing recent sequence %d for A", seq)
			}
		}
	})

	// runtimeJSONFixture — нетривиальный JSON-документ: вложенные объекты,
	// массивы, юникод, спецсимволы. Хранилище обязано вернуть его байт-в-байт:
	// колонка runtime_json — непрозрачная строка для storage-слоя.
	const runtimeJSONFixture = `{"route_mode":"me→direct","system_load":{"cpu_usage_pct":42.5,"load_1m":1.25},` +
		`"connections_bad_by_class":[{"class":"тайм-аут","total":7}],"fail_rate_pct_5m":0.031,` +
		`"quote":"a\"b\\c","updated_at":"2026-07-02T03:04:05.123456789Z"}`

	t.Run("runtime current stores the JSON blob verbatim", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		ctx := context.Background()
		seedAgentForTelemetry(t, store, "agent-json")

		observed := time.Date(2026, time.July, 2, 10, 0, 0, 0, time.UTC)
		rec := storage.TelemetryRuntimeCurrentRecord{
			AgentID:     "agent-json",
			ObservedAt:  observed,
			RuntimeJSON: runtimeJSONFixture,
		}
		if err := store.PutTelemetryRuntimeCurrent(ctx, rec); err != nil {
			t.Fatalf("PutTelemetryRuntimeCurrent() error = %v", err)
		}
		got, err := store.GetTelemetryRuntimeCurrent(ctx, "agent-json")
		if err != nil {
			t.Fatalf("GetTelemetryRuntimeCurrent() error = %v", err)
		}
		if got.RuntimeJSON != runtimeJSONFixture {
			t.Fatalf("RuntimeJSON round-trip mismatch:\n got: %s\nwant: %s", got.RuntimeJSON, runtimeJSONFixture)
		}
		if !got.ObservedAt.Equal(observed) {
			t.Fatalf("ObservedAt = %v, want %v", got.ObservedAt, observed)
		}
		if got.AgentID != "agent-json" {
			t.Fatalf("AgentID = %q, want %q", got.AgentID, "agent-json")
		}
	})

	t.Run("runtime current upsert overwrites blob and observed_at", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		ctx := context.Background()
		seedAgentForTelemetry(t, store, "agent-upsert")

		first := storage.TelemetryRuntimeCurrentRecord{
			AgentID:     "agent-upsert",
			ObservedAt:  time.Date(2026, time.July, 2, 10, 0, 0, 0, time.UTC),
			RuntimeJSON: `{"v":1}`,
		}
		second := storage.TelemetryRuntimeCurrentRecord{
			AgentID:     "agent-upsert",
			ObservedAt:  time.Date(2026, time.July, 2, 11, 0, 0, 0, time.UTC),
			RuntimeJSON: `{"v":2}`,
		}
		if err := store.PutTelemetryRuntimeCurrent(ctx, first); err != nil {
			t.Fatalf("Put(first) error = %v", err)
		}
		if err := store.PutTelemetryRuntimeCurrent(ctx, second); err != nil {
			t.Fatalf("Put(second) error = %v", err)
		}
		got, err := store.GetTelemetryRuntimeCurrent(ctx, "agent-upsert")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got.RuntimeJSON != `{"v":2}` || !got.ObservedAt.Equal(second.ObservedAt) {
			t.Fatalf("upsert did not overwrite: got %+v", got)
		}

		all, err := store.ListTelemetryRuntimeCurrent(ctx)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(all) != 1 || all[0].RuntimeJSON != `{"v":2}` {
			t.Fatalf("List() = %+v, want single {v:2} row", all)
		}
	})

	t.Run("runtime current missing row returns ErrNotFound", func(t *testing.T) {
		store := open(t)
		defer store.Close()
		_, err := store.GetTelemetryRuntimeCurrent(context.Background(), "agent-absent")
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Get(absent) error = %v, want storage.ErrNotFound", err)
		}
	})
}
