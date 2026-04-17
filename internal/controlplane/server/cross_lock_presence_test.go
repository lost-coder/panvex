package server

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestResolveClientTargetAgentIDsRunsCleanUnderRace validates the lock
// discipline of resolveClientTargetAgentIDs (P2-LOG-11 / M-C11 / L-08).
//
// Before the fix, the function read s.agents under s.mu.RLock while the
// caller's `assignments` slice was captured under s.clientsMu — two
// independent critical sections. Agents mutated concurrently with clients
// could produce deployment rows that referenced ids inconsistent with the
// current agent snapshot, and more importantly the reverse-order lock
// hazard (clientsMu -> mu) was undocumented. After the fix, the function
// snapshots needed agent fields under s.mu once, releases it, then
// iterates the caller-provided assignments against the snapshot — keeping
// the two lock windows strictly disjoint.
//
// Run with -race: any reverse-order acquisition or map read/write race
// against s.agents will fail the test.
func TestResolveClientTargetAgentIDsRunsCleanUnderRace(t *testing.T) {
	now := time.Date(2026, time.April, 17, 12, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	defer server.Close()

	// Seed a population of agents across a couple of fleet groups so the
	// fleet-group branch of the resolver has work to do.
	const initialAgents = 32
	server.mu.Lock()
	for idx := 0; idx < initialAgents; idx++ {
		id := fmt.Sprintf("agent-seed-%d", idx)
		fleetGroupID := "group-a"
		if idx%2 == 0 {
			fleetGroupID = "group-b"
		}
		server.agents[id] = Agent{
			ID:           id,
			NodeName:     id,
			FleetGroupID: fleetGroupID,
			Version:      "dev",
			LastSeenAt:   now,
		}
	}
	server.mu.Unlock()

	assignments := []managedClientAssignment{
		{
			ID:           "a-1",
			ClientID:     "client-1",
			TargetType:   clientAssignmentTargetFleetGroup,
			FleetGroupID: "group-a",
			CreatedAt:    now,
		},
		{
			ID:           "a-2",
			ClientID:     "client-1",
			TargetType:   clientAssignmentTargetFleetGroup,
			FleetGroupID: "group-b",
			CreatedAt:    now,
		},
		{
			ID:         "a-3",
			ClientID:   "client-1",
			TargetType: clientAssignmentTargetAgent,
			AgentID:    "agent-seed-0",
			CreatedAt:  now,
		},
	}

	stop := make(chan struct{})
	var resolversWG sync.WaitGroup
	var mutatorsWG sync.WaitGroup

	// Mutator: continually adds and removes agents under s.mu. If the
	// resolver were iterating s.agents without the snapshot, -race would
	// flag the concurrent map access.
	mutatorsWG.Add(1)
	go func() {
		defer mutatorsWG.Done()
		counter := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			id := fmt.Sprintf("agent-churn-%d", counter%8)
			counter++
			server.mu.Lock()
			if _, present := server.agents[id]; present {
				delete(server.agents, id)
			} else {
				server.agents[id] = Agent{
					ID:           id,
					NodeName:     id,
					FleetGroupID: "group-a",
					Version:      "dev",
					LastSeenAt:   now,
				}
			}
			server.mu.Unlock()
		}
	}()

	// Parallel clientsMu user: takes s.clientsMu to exercise the
	// mu -> clientsMu ordering rule from the other side. The resolver
	// takes s.mu; this takes s.clientsMu. A reverse-order bug would be
	// caught by the race detector or a deadlock.
	mutatorsWG.Add(1)
	go func() {
		defer mutatorsWG.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			server.clientsMu.Lock()
			_ = server.clientAssignments
			server.clientsMu.Unlock()
		}
	}()

	// Resolvers: hammer resolveClientTargetAgentIDs from many goroutines.
	const resolverGoroutines = 8
	const resolutionsPerGoroutine = 500
	for g := 0; g < resolverGoroutines; g++ {
		resolversWG.Add(1)
		go func() {
			defer resolversWG.Done()
			for iter := 0; iter < resolutionsPerGoroutine; iter++ {
				targets := server.resolveClientTargetAgentIDs(assignments)
				// Sanity: the direct assignment to agent-seed-0 must
				// always be present because agent-seed-0 is seeded and
				// never churned.
				found := false
				for _, id := range targets {
					if id == "agent-seed-0" {
						found = true
						break
					}
				}
				if !found {
					// Errorf on a goroutine (Fatalf would not abort).
					t.Errorf("resolveClientTargetAgentIDs() missing stable agent-seed-0, got %v", targets)
					return
				}
			}
		}()
	}

	// Wait for resolvers to finish their bounded loops, then signal
	// mutators to stop and wait for their teardown.
	resolversWG.Wait()
	close(stop)
	mutatorsWG.Wait()
}

// TestPresenceConnectedAtPersistsAcrossSnapshots verifies the L-05 fix:
// applyAgentSnapshot no longer calls MarkConnected, so connectedAt only
// moves forward when a new gRPC stream is opened (P2-LOG-12). Repeated
// snapshots update lastSeenAt (Heartbeat) but leave connectedAt stable.
func TestPresenceConnectedAtPersistsAcrossSnapshots(t *testing.T) {
	now := time.Date(2026, time.April, 17, 9, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	defer server.Close()

	agentID := "agent-xyz"

	// Seed the agent so applyAgentSnapshot has a record to update.
	server.mu.Lock()
	server.agents[agentID] = Agent{
		ID:           agentID,
		NodeName:     "node-xyz",
		FleetGroupID: "grp",
		Version:      "dev",
		LastSeenAt:   now,
	}
	server.mu.Unlock()

	// Simulate a stream open: the Connect handler calls MarkConnected at
	// stream-open. Record T1.
	streamOpenT1 := now
	server.presence.MarkConnected(agentID, streamOpenT1)

	t1, ok := server.presence.ConnectedAt(agentID)
	if !ok {
		t.Fatalf("ConnectedAt(%q) not tracked after MarkConnected", agentID)
	}
	if !t1.Equal(streamOpenT1.UTC()) {
		t.Fatalf("ConnectedAt after MarkConnected = %s, want %s", t1, streamOpenT1.UTC())
	}

	// Send three snapshots — each moves lastSeenAt forward but must NOT
	// rewrite connectedAt.
	for idx := 1; idx <= 3; idx++ {
		snapshotAt := streamOpenT1.Add(time.Duration(idx) * 5 * time.Second)
		if err := server.applyAgentSnapshot(agentSnapshot{
			AgentID:      agentID,
			NodeName:     "node-xyz",
			FleetGroupID: "grp",
			Version:      "dev",
			ObservedAt:   snapshotAt,
		}); err != nil {
			t.Fatalf("applyAgentSnapshot(#%d) error = %v", idx, err)
		}

		got, ok := server.presence.ConnectedAt(agentID)
		if !ok {
			t.Fatalf("ConnectedAt after snapshot #%d not tracked", idx)
		}
		if !got.Equal(streamOpenT1.UTC()) {
			t.Fatalf("ConnectedAt after snapshot #%d = %s, want stable %s (MarkConnected must not be called from applyAgentSnapshot)",
				idx, got, streamOpenT1.UTC())
		}
	}

	// A new stream open (Connect) must bump connectedAt forward.
	streamOpenT2 := streamOpenT1.Add(time.Minute)
	server.presence.MarkConnected(agentID, streamOpenT2)

	t2, ok := server.presence.ConnectedAt(agentID)
	if !ok {
		t.Fatalf("ConnectedAt after second MarkConnected not tracked")
	}
	if !t2.After(t1) {
		t.Fatalf("ConnectedAt after second stream open = %s, want > %s", t2, t1)
	}
	if !t2.Equal(streamOpenT2.UTC()) {
		t.Fatalf("ConnectedAt after second stream open = %s, want %s", t2, streamOpenT2.UTC())
	}
}
