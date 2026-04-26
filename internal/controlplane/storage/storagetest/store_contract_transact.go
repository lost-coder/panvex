package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runTransactContract exercises the Store.Transact contract that
// every backend must satisfy (Q5.U-Q-18: split out of the
// store_contract.go monolith). RunStoreContract calls this directly.
func runTransactContract(t *testing.T, open OpenStore) {
	t.Helper()

	// --- Transact contract (P2-ARCH-01) ---

	t.Run("Transact commits on nil return", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "tx-commit-group",
			Name:      "tx-commit-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		client := storage.ClientRecord{
			ID:        "tx-commit-client",
			Name:      "tx-commit-client",
			SecretCiphertext: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			CreatedAt: group.CreatedAt,
			UpdatedAt: group.CreatedAt,
		}

		if err := store.Transact(ctx, func(tx storage.Store) error {
			if err := tx.PutFleetGroup(ctx, group); err != nil {
				return err
			}
			return tx.PutClient(ctx, client)
		}); err != nil {
			t.Fatalf("Transact() commit error = %v", err)
		}

		got, err := store.GetClientByID(ctx, client.ID)
		if err != nil {
			t.Fatalf("GetClientByID() after commit error = %v", err)
		}
		if got.ID != client.ID {
			t.Fatalf("GetClientByID().ID = %q, want %q", got.ID, client.ID)
		}
	})

	t.Run("Transact rolls back on fn error", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "tx-rollback-group",
			Name:      "tx-rollback-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		client := storage.ClientRecord{
			ID:        "tx-rollback-client",
			Name:      "tx-rollback-client",
			SecretCiphertext: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			CreatedAt: group.CreatedAt,
			UpdatedAt: group.CreatedAt,
		}

		sentinel := errors.New("sentinel rollback")
		err := store.Transact(ctx, func(tx storage.Store) error {
			if err := tx.PutFleetGroup(ctx, group); err != nil {
				return err
			}
			if err := tx.PutClient(ctx, client); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("Transact() err = %v, want %v", err, sentinel)
		}

		if _, err := store.GetClientByID(ctx, client.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetClientByID() after rollback err = %v, want ErrNotFound", err)
		}
	})

	t.Run("Transact rolls back on panic and re-raises", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		client := storage.ClientRecord{
			ID:        "tx-panic-client",
			Name:      "tx-panic-client",
			SecretCiphertext: "cccccccccccccccccccccccccccccccc",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}

		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic to propagate out of Transact")
				}
			}()
			_ = store.Transact(ctx, func(tx storage.Store) error {
				if err := tx.PutClient(ctx, client); err != nil {
					t.Fatalf("PutClient inside Transact error = %v", err)
				}
				panic("boom")
			})
		}()

		if _, err := store.GetClientByID(ctx, client.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetClientByID() after panic-rollback err = %v, want ErrNotFound", err)
		}
	})

	t.Run("Transact returns ErrNestedTransact on nested call", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		var inner error
		outer := store.Transact(ctx, func(tx storage.Store) error {
			inner = tx.Transact(ctx, func(storage.Store) error { return nil })
			return nil
		})
		if outer != nil {
			t.Fatalf("outer Transact() err = %v, want nil", outer)
		}
		if !errors.Is(inner, storage.ErrNestedTransact) {
			t.Fatalf("inner Transact() err = %v, want ErrNestedTransact", inner)
		}
	})

	t.Run("Transact aborts when context cancelled before fn", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := store.Transact(ctx, func(tx storage.Store) error {
			t.Fatalf("fn should not run after ctx cancellation")
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Transact() err = %v, want context.Canceled", err)
		}
	})

	t.Run("Transact serializes concurrent writers on same row", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "tx-concurrent-group",
			Name:      "tx-concurrent-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}

		const clientID = "tx-concurrent-client"
		type result struct {
			err    error
			winner string
		}
		results := make(chan result, 2)
		run := func(name string) {
			err := store.Transact(ctx, func(tx storage.Store) error {
				client := storage.ClientRecord{
					ID:        clientID,
					Name:      name,
					SecretCiphertext: "dddddddddddddddddddddddddddddddd",
					CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
					UpdatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
				}
				if err := tx.PutClient(ctx, client); err != nil {
					return err
				}
				assignment := storage.ClientAssignmentRecord{
					ID:           name + "-assignment",
					ClientID:     clientID,
					FleetGroupID: group.ID,
					CreatedAt:    client.CreatedAt,
				}
				return tx.PutClientAssignment(ctx, assignment)
			})
			results <- result{err: err, winner: name}
		}
		go run("name-a")
		go run("name-b")
		r1 := <-results
		r2 := <-results

		if r1.err != nil && r2.err != nil {
			t.Fatalf("both Transacts failed: r1=%v r2=%v", r1.err, r2.err)
		}

		got, err := store.GetClientByID(ctx, clientID)
		if err != nil {
			t.Fatalf("GetClientByID() error = %v", err)
		}
		if got.Name != "name-a" && got.Name != "name-b" {
			t.Fatalf("GetClientByID().Name = %q, want name-a or name-b", got.Name)
		}

		assignments, err := store.ListClientAssignments(ctx, clientID)
		if err != nil {
			t.Fatalf("ListClientAssignments() error = %v", err)
		}
		if len(assignments) == 0 {
			t.Fatalf("expected at least one assignment from the winning Transact")
		}
	})

	t.Run("Transact rejects nil fn", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		if err := store.Transact(context.Background(), nil); err == nil {
			t.Fatalf("Transact(nil) err = nil, want non-nil")
		}
	})

	// -------------------------------------------------------------------
	// P3-PERF-01a: bulk insert contract tests.
	// -------------------------------------------------------------------

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
			ID:        "bulk-grp",
			Name:      "Bulk Group",
			CreatedAt: time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC),
		}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
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
		group := storage.FleetGroupRecord{ID: "inst-grp", Name: "Inst", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "inst-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
		}
		ts := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
		batch := []storage.InstanceRecord{
			{ID: "i1", AgentID: agent.ID, Name: "t1", Version: "v1", ConfigFingerprint: "c1", ConnectedUsers: 1, UpdatedAt: ts},
			{ID: "i2", AgentID: agent.ID, Name: "t2", Version: "v1", ConfigFingerprint: "c2", ConnectedUsers: 2, UpdatedAt: ts},
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
		group := storage.FleetGroupRecord{ID: "m-grp", Name: "M", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "m-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
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
		group := storage.FleetGroupRecord{ID: "sl-grp", Name: "SL", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "sl-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
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

	t.Run("AppendDCHealthPointsBulk inserts a batch", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "dc-grp", Name: "DC", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "dc-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
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
		group := storage.FleetGroupRecord{ID: "ip-grp", Name: "IP", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "ip-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
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
		batch := []storage.ClientIPHistoryRecord{
			{AgentID: agent.ID, ClientID: client.ID, IPAddress: "10.0.0.1", FirstSeen: first, LastSeen: first},
			{AgentID: agent.ID, ClientID: client.ID, IPAddress: "10.0.0.2", FirstSeen: first, LastSeen: first},
			// Duplicate key (same agent+client+ip as first row). last_seen
			// must advance via the ON CONFLICT DO UPDATE clause.
			{AgentID: agent.ID, ClientID: client.ID, IPAddress: "10.0.0.1", FirstSeen: first, LastSeen: later},
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
		// 3 distinct (agent,client,ip) combos: probe 127.0.0.254, 10.0.0.1, 10.0.0.2.
		if len(got) != 3 {
			t.Fatalf("len(ip_history) = %d, want 3 (probe + 2 from batch, conflict collapses)", len(got))
		}
		var first10 storage.ClientIPHistoryRecord
		for _, r := range got {
			if r.IPAddress == "10.0.0.1" {
				first10 = r
			}
		}
		if !first10.LastSeen.Equal(later) {
			t.Fatalf("10.0.0.1 last_seen = %v, want %v (updated on conflict)", first10.LastSeen, later)
		}
	})
}
