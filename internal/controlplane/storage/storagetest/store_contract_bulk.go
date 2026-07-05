package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runBulkWriteContract exercises the bulk-write helpers introduced
// in P3-PERF-01a (PutAgentsBulk, PutInstancesBulk, etc.). Split out
// of store_contract_transact.go (R-Q-18) so each contract file stays
// under the 400 LOC ceiling.
func runBulkWriteContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("PutAgentsBulk empty slice is a no-op", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		if err := store.PutAgentsBulk(context.Background(), nil); err != nil {
			t.Fatalf("PutAgentsBulk(nil) err = %v, want nil", err)
		}
		if err := store.PutAgentsBulk(context.Background(), []storage.AgentRecord{}); err != nil {
			t.Fatalf("PutAgentsBulk([]) err = %v, want nil", err)
		}
	})

	t.Run("PutAgentsBulk UPSERT semantics - last write wins on duplicate id", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "00000000-0000-4000-8000-000000000002",
			Name:      "Bulk Group",
			CreatedAt: time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC),
		}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupShort, err)
		}

		ts := time.Date(2026, time.April, 1, 10, 5, 0, 0, time.UTC)
		// Two entries with the same ID in one batch — the second must win.
		batch := []storage.AgentRecord{
			{ID: "a-dup", NodeName: "first", FleetGroupID: group.ID, Version: "v1", LastSeenAt: ts},
			{ID: "a-dup", NodeName: "second", FleetGroupID: group.ID, Version: "v2", LastSeenAt: ts},
			{ID: "a-unique", NodeName: "solo", FleetGroupID: group.ID, Version: "v1", LastSeenAt: ts},
		}
		if err := store.PutAgentsBulk(ctx, batch); err != nil {
			t.Fatalf("PutAgentsBulk: %v", err)
		}

		agents, err := store.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents: %v", err)
		}
		if len(agents) != 2 {
			t.Fatalf("len(agents) = %d, want 2 (dedup)", len(agents))
		}
		var dup storage.AgentRecord
		for _, a := range agents {
			if a.ID == "a-dup" {
				dup = a
			}
		}
		if dup.NodeName != "second" || dup.Version != "v2" {
			t.Fatalf("dup node_name=%q version=%q, want second/v2 (last-write-wins)", dup.NodeName, dup.Version)
		}

		// Calling PutAgentsBulk again with an updated row for the same id
		// updates in place (UPSERT semantics across calls).
		if err := store.PutAgentsBulk(ctx, []storage.AgentRecord{{
			ID: "a-dup", NodeName: "third", FleetGroupID: group.ID, Version: "v3", LastSeenAt: ts,
		}}); err != nil {
			t.Fatalf("PutAgentsBulk (second call): %v", err)
		}
		agents, err = store.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents after second call: %v", err)
		}
		for _, a := range agents {
			if a.ID == "a-dup" && a.NodeName != "third" {
				t.Fatalf("after second PutAgentsBulk, node_name = %q, want 'third'", a.NodeName)
			}
		}
	})

	t.Run("PutInstancesBulk upserts a batch", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "00000000-0000-4000-8000-000000000004", Name: "Inst", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupShort, err)
		}
		agent := storage.AgentRecord{ID: "inst-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf(errPutAgentShort, err)
		}
		ts := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
		batch := []storage.InstanceRecord{
			{ID: "i1", AgentID: agent.ID, Name: "t1", Version: "v1", ConfigFingerprint: "c1", Connections: 1, UpdatedAt: ts},
			{ID: "i2", AgentID: agent.ID, Name: "t2", Version: "v1", ConfigFingerprint: "c2", Connections: 2, UpdatedAt: ts},
		}
		if err := store.PutInstancesBulk(ctx, batch); err != nil {
			t.Fatalf("PutInstancesBulk: %v", err)
		}
		if err := store.PutInstancesBulk(ctx, nil); err != nil {
			t.Fatalf("PutInstancesBulk(nil): %v", err)
		}
		got, err := store.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(instances) = %d, want 2", len(got))
		}
	})

	t.Run("AppendMetricSnapshotsBulk inserts a batch", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "00000000-0000-4000-8000-000000000006", Name: "M", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupShort, err)
		}
		agent := storage.AgentRecord{ID: "m-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf(errPutAgentShort, err)
		}
		ts := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
		batch := []storage.MetricSnapshotRecord{
			{ID: "s1", AgentID: agent.ID, InstanceID: "", CapturedAt: ts, Values: map[string]uint64{"cpu": 1}},
			{ID: "s2", AgentID: agent.ID, InstanceID: "", CapturedAt: ts.Add(time.Second), Values: map[string]uint64{"cpu": 2}},
		}
		if err := store.AppendMetricSnapshotsBulk(ctx, batch); err != nil {
			t.Fatalf("AppendMetricSnapshotsBulk: %v", err)
		}
		if err := store.AppendMetricSnapshotsBulk(ctx, nil); err != nil {
			t.Fatalf("AppendMetricSnapshotsBulk(nil): %v", err)
		}
		got, err := store.ListMetricSnapshots(ctx)
		if err != nil {
			t.Fatalf("ListMetricSnapshots: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(snapshots) = %d, want 2", len(got))
		}
	})

	t.Run("AppendServerLoadPointsBulk inserts and de-dupes on (agent,captured_at)", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "00000000-0000-4000-8000-000000000007", Name: "SL", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupShort, err)
		}
		agent := storage.AgentRecord{ID: "sl-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf(errPutAgentShort, err)
		}

		// Probe: if the backend does not actually persist timeseries data
		// (the in-memory contract stub uses no-op stubs), skip the list-based
		// assertions. This keeps the same contract runnable against both the
		// production backends and the lightweight memoryStore fixture.
		probe := storage.ServerLoadPointRecord{AgentID: agent.ID, CapturedAt: time.Now().UTC(), SampleCount: 1}
		if err := store.AppendServerLoadPoint(ctx, probe); err != nil {
			t.Fatalf("AppendServerLoadPoint(probe): %v", err)
		}
		seen, err := store.ListServerLoadPoints(ctx, agent.ID, probe.CapturedAt.Add(-time.Hour), probe.CapturedAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListServerLoadPoints(probe): %v", err)
		}
		persistent := len(seen) > 0

		ts := time.Date(2026, time.April, 2, 11, 0, 0, 0, time.UTC)
		batch := []storage.ServerLoadPointRecord{
			{AgentID: agent.ID, CapturedAt: ts, SampleCount: 1},
			{AgentID: agent.ID, CapturedAt: ts.Add(time.Minute), SampleCount: 1},
			// Duplicate key: same agent + captured_at as first row. Must be
			// ignored by the ON CONFLICT DO NOTHING semantics.
			{AgentID: agent.ID, CapturedAt: ts, SampleCount: 99},
		}
		if err := store.AppendServerLoadPointsBulk(ctx, batch); err != nil {
			t.Fatalf("AppendServerLoadPointsBulk: %v", err)
		}
		if err := store.AppendServerLoadPointsBulk(ctx, nil); err != nil {
			t.Fatalf("AppendServerLoadPointsBulk(nil): %v", err)
		}
		if !persistent {
			return
		}
		got, err := store.ListServerLoadPoints(ctx, agent.ID, ts.Add(-time.Hour), ts.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListServerLoadPoints: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(server_load) = %d, want 2 (conflict ignored)", len(got))
		}
	})

	t.Run("RollupServerLoadHourly weights averages by sample_count (IN-L5)", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "00000000-0000-4000-8000-000000000008", Name: "SLW", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupShort, err)
		}
		agent := storage.AgentRecord{ID: "slw-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf(errPutAgentShort, err)
		}

		bucket := time.Date(2026, time.April, 3, 9, 0, 0, 0, time.UTC)
		// Two raw points in the same hour bucket carrying very different
		// sample counts. A naive AVG(avg) returns (10+90)/2 = 50; the
		// sample-weighted mean SUM(avg*count)/SUM(count) =
		// (10*1 + 90*9)/10 = 82. The hourly sample_count must be the SUM of
		// underlying samples (10), not the raw-row COUNT(*) (2). See IN-L5.
		p1 := storage.ServerLoadPointRecord{
			AgentID: agent.ID, CapturedAt: bucket.Add(5 * time.Minute),
			CPUPctAvg: 10, MemPctAvg: 10, ConnectionsAvg: 10, ActiveUsersAvg: 10,
			DCCoverageAvgPct: 10, SampleCount: 1,
		}
		// Persistence probe: the in-memory contract stub does not persist
		// timeseries rows, so skip the rollup assertions there.
		if err := store.AppendServerLoadPoint(ctx, p1); err != nil {
			t.Fatalf("AppendServerLoadPoint(p1): %v", err)
		}
		seen, err := store.ListServerLoadPoints(ctx, agent.ID, bucket, bucket.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListServerLoadPoints(probe): %v", err)
		}
		if len(seen) == 0 {
			return
		}

		p2 := storage.ServerLoadPointRecord{
			AgentID: agent.ID, CapturedAt: bucket.Add(15 * time.Minute),
			CPUPctAvg: 90, MemPctAvg: 90, ConnectionsAvg: 90, ActiveUsersAvg: 90,
			DCCoverageAvgPct: 90, SampleCount: 9,
		}
		if err := store.AppendServerLoadPoint(ctx, p2); err != nil {
			t.Fatalf("AppendServerLoadPoint(p2): %v", err)
		}

		if err := store.RollupServerLoadHourly(ctx, bucket); err != nil {
			t.Fatalf("RollupServerLoadHourly: %v", err)
		}
		hourly, err := store.ListServerLoadHourly(ctx, agent.ID, bucket, bucket.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListServerLoadHourly: %v", err)
		}
		if len(hourly) != 1 {
			t.Fatalf("len(hourly) = %d, want 1", len(hourly))
		}
		h := hourly[0]

		const wantWeighted = 82.0
		const eps = 1e-9
		approx := func(name string, got float64) {
			if d := got - wantWeighted; d > eps || d < -eps {
				t.Errorf("%s = %v, want weighted %v (naive AVG would give 50)", name, got, wantWeighted)
			}
		}
		approx("CPUPctAvg", h.CPUPctAvg)
		approx("MemPctAvg", h.MemPctAvg)
		approx("ConnectionsAvg", h.ConnectionsAvg)
		approx("ActiveUsersAvg", h.ActiveUsersAvg)
		approx("DCCoverageAvg", h.DCCoverageAvg)
		if h.SampleCount != 10 {
			t.Errorf("SampleCount = %d, want 10 (sum of sample counts, not COUNT(*)=2)", h.SampleCount)
		}
	})

	t.Run("AppendDCHealthPointsBulk inserts a batch", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "00000000-0000-4000-8000-000000000003", Name: "DC", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupShort, err)
		}
		agent := storage.AgentRecord{ID: "dc-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf(errPutAgentShort, err)
		}

		// Persistence probe — see the server_load bulk test for the rationale.
		probe := storage.DCHealthPointRecord{AgentID: agent.ID, CapturedAt: time.Now().UTC(), DC: 99, SampleCount: 1}
		if err := store.AppendDCHealthPoint(ctx, probe); err != nil {
			t.Fatalf("AppendDCHealthPoint(probe): %v", err)
		}
		seen, err := store.ListDCHealthPoints(ctx, agent.ID, probe.CapturedAt.Add(-time.Hour), probe.CapturedAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListDCHealthPoints(probe): %v", err)
		}
		persistent := len(seen) > 0

		ts := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
		batch := []storage.DCHealthPointRecord{
			{AgentID: agent.ID, CapturedAt: ts, DC: 2, SampleCount: 1},
			{AgentID: agent.ID, CapturedAt: ts, DC: 3, SampleCount: 1},
		}
		if err := store.AppendDCHealthPointsBulk(ctx, batch); err != nil {
			t.Fatalf("AppendDCHealthPointsBulk: %v", err)
		}
		if err := store.AppendDCHealthPointsBulk(ctx, nil); err != nil {
			t.Fatalf("AppendDCHealthPointsBulk(nil): %v", err)
		}
		if !persistent {
			return
		}
		got, err := store.ListDCHealthPoints(ctx, agent.ID, ts.Add(-time.Hour), ts.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListDCHealthPoints: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(dc_health) = %d, want 2", len(got))
		}
	})

	t.Run("UpsertClientIPHistoryBulk upserts a batch and updates last_seen on conflict", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "00000000-0000-4000-8000-000000000005", Name: "IP", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupShort, err)
		}
		agent := storage.AgentRecord{ID: "ip-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf(errPutAgentShort, err)
		}
		client := storage.ClientRecord{
			ID: "ip-client", Name: "alice", SecretCiphertext: "s", UserADTag: "0123456789abcdef0123456789abcdef",
			Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := store.PutClient(ctx, client); err != nil {
			t.Fatalf("PutClient: %v", err)
		}

		first := time.Date(2026, time.April, 2, 13, 0, 0, 0, time.UTC)
		later := first.Add(5 * time.Minute)

		// Persistence probe — uses an IP outside the batch set and a timestamp
		// inside the subsequent list window so we can detect whether the
		// backend actually persists rows.
		probeTime := first.Add(-30 * time.Minute)
		probe := storage.ClientIPHistoryRecord{AgentID: agent.ID, ClientID: client.ID, IPAddress: "127.0.0.254", FirstSeen: probeTime, LastSeen: probeTime}
		if err := store.UpsertClientIPHistory(ctx, probe); err != nil {
			t.Fatalf("UpsertClientIPHistory(probe): %v", err)
		}
		seen, err := store.ListClientIPHistory(ctx, client.ID, first.Add(-time.Hour), later.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListClientIPHistory(probe): %v", err)
		}
		persistent := len(seen) > 0
		const otherClientIPv4 = "192.0.2.2"
		batch := []storage.ClientIPHistoryRecord{
			{AgentID: agent.ID, ClientID: client.ID, IPAddress: fixtureClientIPv4, FirstSeen: first, LastSeen: first},
			{AgentID: agent.ID, ClientID: client.ID, IPAddress: otherClientIPv4, FirstSeen: first, LastSeen: first},
			// Duplicate key (same agent+client+ip as first row). last_seen
			// must advance via the ON CONFLICT DO UPDATE clause.
			{AgentID: agent.ID, ClientID: client.ID, IPAddress: fixtureClientIPv4, FirstSeen: first, LastSeen: later},
		}
		if err := store.UpsertClientIPHistoryBulk(ctx, batch); err != nil {
			t.Fatalf("UpsertClientIPHistoryBulk: %v", err)
		}
		if err := store.UpsertClientIPHistoryBulk(ctx, nil); err != nil {
			t.Fatalf("UpsertClientIPHistoryBulk(nil): %v", err)
		}
		if !persistent {
			return
		}
		got, err := store.ListClientIPHistory(ctx, client.ID, first.Add(-time.Hour), later.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListClientIPHistory: %v", err)
		}
		// 3 distinct (agent,client,ip) combos: probe 127.0.0.254, fixtureClientIPv4, otherClientIPv4.
		if len(got) != 3 {
			t.Fatalf("len(ip_history) = %d, want 3 (probe + 2 from batch, conflict collapses)", len(got))
		}
		var first10 storage.ClientIPHistoryRecord
		for _, r := range got {
			if r.IPAddress == fixtureClientIPv4 {
				first10 = r
			}
		}
		if !first10.LastSeen.Equal(later) {
			t.Fatalf("%s last_seen = %v, want %v (updated on conflict)", fixtureClientIPv4, first10.LastSeen, later)
		}
	})

	t.Run("UpsertClientUsage is last-write-wins (P4)", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "00000000-0000-4000-8000-000000000010", Name: "Mono", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf(errPutFleetGroupShort, err)
		}
		agent := storage.AgentRecord{ID: "mono-agent", NodeName: "mono", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf(errPutAgentShort, err)
		}
		client := storage.ClientRecord{
			ID: "mono-client", Name: "mono", SecretCiphertext: "s",
			UserADTag: "0123456789abcdef0123456789abcdef",
			Enabled:   true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := store.PutClient(ctx, client); err != nil {
			t.Fatalf("PutClient: %v", err)
		}

		// Persistence probe — the in-memory fixture backend intentionally
		// no-ops UpsertClientUsage / ListClientUsage, so skip the assertions
		// there.
		probeSeen, err := store.ListClientUsage(ctx)
		if err != nil {
			t.Fatalf("ListClientUsage(probe): %v", err)
		}
		if err := store.UpsertClientUsage(ctx, storage.ClientUsageRecord{
			ClientID: client.ID, AgentID: agent.ID, AgentBootID: "boot-probe", LastTotalBytes: 1, ObservedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("UpsertClientUsage(probe): %v", err)
		}
		probeAfter, err := store.ListClientUsage(ctx)
		if err != nil {
			t.Fatalf("ListClientUsage(probe after): %v", err)
		}
		if len(probeAfter) == len(probeSeen) {
			return
		}

		findRow := func() (storage.ClientUsageRecord, bool) {
			t.Helper()
			all, err := store.ListClientUsage(ctx)
			if err != nil {
				t.Fatalf("ListClientUsage: %v", err)
			}
			for _, r := range all {
				if r.ClientID == client.ID && r.AgentID == agent.ID {
					return r, true
				}
			}
			return storage.ClientUsageRecord{}, false
		}

		base := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)

		// P4: the upsert is unconditional last-write-wins — ordering and
		// duplicate protection moved upstream into the panel's watermark
		// derivation, so ANY later write must apply, including one carrying
		// a smaller absolute (e.g. an operator usage reset).
		if err := store.UpsertClientUsage(ctx, storage.ClientUsageRecord{
			ClientID: client.ID, AgentID: agent.ID,
			TrafficUsedBytes: 500, AgentBootID: "boot-1", LastTotalBytes: 500, ObservedAt: base,
		}); err != nil {
			t.Fatalf("UpsertClientUsage(first): %v", err)
		}
		if err := store.UpsertClientUsage(ctx, storage.ClientUsageRecord{
			ClientID: client.ID, AgentID: agent.ID,
			TrafficUsedBytes: 300, AgentBootID: "boot-2", LastTotalBytes: 40, ObservedAt: base.Add(time.Minute),
		}); err != nil {
			t.Fatalf("UpsertClientUsage(second): %v", err)
		}
		row, ok := findRow()
		if !ok || row.TrafficUsedBytes != 300 || row.AgentBootID != "boot-2" || row.LastTotalBytes != 40 {
			t.Fatalf("after second write: row=%+v ok=%v, want traffic=300 boot=boot-2 last_total=40 (last write wins)", row, ok)
		}
	})
}
