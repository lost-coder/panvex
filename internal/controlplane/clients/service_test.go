package clients

import (
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func TestNewServiceWithDepsDefaults(t *testing.T) {
	t.Parallel()

	svc := NewServiceWithDeps(nil, nil)
	if svc == nil {
		t.Fatal("NewServiceWithDeps returned nil")
	}
	if svc.now == nil {
		t.Fatal("NewServiceWithDeps: now must default to time.Now")
	}
	if svc.clients == nil || svc.assignments == nil || svc.deployments == nil || svc.usage == nil || svc.lastUsageSeq == nil {
		t.Fatal("NewServiceWithDeps: in-memory maps must be initialized")
	}

	fixed := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	svc.SetNow(func() time.Time { return fixed })
	if got := svc.now(); !got.Equal(fixed) {
		t.Fatalf("SetNow: got %v want %v", got, fixed)
	}

	// nil SetNow must be a no-op.
	svc.SetNow(nil)
	if got := svc.now(); !got.Equal(fixed) {
		t.Fatalf("SetNow(nil): got %v want %v (clock must be unchanged)", got, fixed)
	}
}

func TestServiceSequenceHelpers(t *testing.T) {
	t.Parallel()

	svc := NewService()
	if got := svc.NextClientID(); got != "client-0000001" {
		t.Fatalf("NextClientID: got %q want client-0000001", got)
	}
	if got := svc.NextClientID(); got != "client-0000002" {
		t.Fatalf("NextClientID (2): got %q want client-0000002", got)
	}
	if got := svc.NextAssignmentID(); got != "client-assignment-0000001" {
		t.Fatalf("NextAssignmentID: got %q want client-assignment-0000001", got)
	}
	if got := svc.NextDiscoveredID(); got != "discovered-0000001" {
		t.Fatalf("NextDiscoveredID: got %q", got)
	}

	svc.RecoverSequencesFromRecords(
		[]string{"client-0000007", "bogus"},
		[]string{"client-assignment-0000100"},
		[]string{"discovered-0000050"},
	)
	if got := svc.NextClientID(); got != "client-0000008" {
		t.Fatalf("after recover: NextClientID = %q want client-0000008", got)
	}
	if got := svc.NextAssignmentID(); got != "client-assignment-0000101" {
		t.Fatalf("after recover: NextAssignmentID = %q", got)
	}
	if got := svc.NextDiscoveredID(); got != "discovered-0000051" {
		t.Fatalf("after recover: NextDiscoveredID = %q", got)
	}
}

func TestServiceListAndDetailSnapshot(t *testing.T) {
	t.Parallel()

	svc := NewService()
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Second)
	deleted := t0

	svc.ReplaceInMemory(
		Client{ID: "client-1", Name: "alpha", CreatedAt: t0, UpdatedAt: t0},
		[]Assignment{{ID: "a1", ClientID: "client-1", TargetType: TargetTypeAgent, AgentID: "agent-1", CreatedAt: t0}},
		[]Deployment{{ClientID: "client-1", AgentID: "agent-1", Status: DeploymentStatusQueued, UpdatedAt: t0}},
	)
	svc.ReplaceInMemory(
		Client{ID: "client-2", Name: "beta", CreatedAt: t1, UpdatedAt: t1},
		nil,
		nil,
	)
	svc.ReplaceInMemory(
		Client{ID: "client-3", Name: "gamma", CreatedAt: t0, UpdatedAt: t0, DeletedAt: &deleted},
		nil,
		nil,
	)

	list := svc.ListSnapshot()
	if len(list) != 2 {
		t.Fatalf("ListSnapshot: len=%d want 2 (deleted must be filtered)", len(list))
	}
	if list[0].ID != "client-1" || list[1].ID != "client-2" {
		t.Fatalf("ListSnapshot order: got %q,%q want client-1,client-2", list[0].ID, list[1].ID)
	}

	client, assignments, deployments, err := svc.DetailSnapshot("client-1")
	if err != nil {
		t.Fatalf("DetailSnapshot: err=%v", err)
	}
	if client.Name != "alpha" {
		t.Fatalf("DetailSnapshot: got client %q want alpha", client.Name)
	}
	if len(assignments) != 1 || assignments[0].AgentID != "agent-1" {
		t.Fatalf("DetailSnapshot assignments: got %+v", assignments)
	}
	if len(deployments) != 1 || deployments[0].AgentID != "agent-1" {
		t.Fatalf("DetailSnapshot deployments: got %+v", deployments)
	}

	if _, _, _, err := svc.DetailSnapshot("missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("DetailSnapshot(missing): err=%v want ErrNotFound", err)
	}
}

func TestServiceFindByNameAndSecret(t *testing.T) {
	t.Parallel()

	svc := NewService()
	t0 := time.Now()
	svc.ReplaceInMemory(Client{ID: "client-1", Name: "alpha", Secret: "deadbeef", CreatedAt: t0}, nil, nil)
	svc.ReplaceInMemory(Client{ID: "client-2", Name: "beta", Secret: "abcd1234", CreatedAt: t0}, nil, nil)
	deleted := t0
	svc.ReplaceInMemory(Client{ID: "client-3", Name: "alpha", Secret: "deadbeef", CreatedAt: t0, DeletedAt: &deleted}, nil, nil)

	if got, ok := svc.FindByNameAndSecret("alpha", "deadbeef"); !ok || got.ID != "client-1" {
		t.Fatalf("FindByNameAndSecret(alpha,deadbeef): got=%+v ok=%v want client-1", got, ok)
	}
	if _, ok := svc.FindByNameAndSecret("alpha", "zzzz"); ok {
		t.Fatal("FindByNameAndSecret mismatched secret: want miss")
	}
	if _, ok := svc.FindByNameAndSecret("missing", "deadbeef"); ok {
		t.Fatal("FindByNameAndSecret unknown name: want miss")
	}
}

func TestServiceManagedIdentifiersForAgent(t *testing.T) {
	t.Parallel()

	svc := NewService()
	t0 := time.Now()
	svc.ReplaceInMemory(
		Client{ID: "client-1", Name: "alpha", Secret: "s1", CreatedAt: t0},
		[]Assignment{{ID: "a1", ClientID: "client-1", TargetType: TargetTypeAgent, AgentID: "agent-1", CreatedAt: t0}},
		nil,
	)
	svc.ReplaceInMemory(
		Client{ID: "client-2", Name: "beta", Secret: "s2", CreatedAt: t0},
		[]Assignment{{ID: "a2", ClientID: "client-2", TargetType: TargetTypeFleetGroup, FleetGroupID: "fg-1", CreatedAt: t0}},
		nil,
	)
	svc.ReplaceInMemory(
		Client{ID: "client-3", Name: "gamma", Secret: "s3", CreatedAt: t0},
		[]Assignment{{ID: "a3", ClientID: "client-3", TargetType: TargetTypeAgent, AgentID: "agent-other", CreatedAt: t0}},
		nil,
	)

	names, secrets := svc.ManagedIdentifiersForAgent("agent-1", "fg-1")
	if _, ok := names["alpha"]; !ok {
		t.Fatal("names missing alpha (direct assignment)")
	}
	if _, ok := names["beta"]; !ok {
		t.Fatal("names missing beta (via fleet-group)")
	}
	if _, ok := names["gamma"]; ok {
		t.Fatal("names contains gamma (assigned to a different agent)")
	}
	if _, ok := secrets["s1"]; !ok {
		t.Fatal("secrets missing s1")
	}
	if _, ok := secrets["s2"]; !ok {
		t.Fatal("secrets missing s2 (fleet-group)")
	}
	if _, ok := secrets["s3"]; ok {
		t.Fatal("secrets contains s3 (unrelated agent)")
	}

	// Without a fleet group, only direct assignments resolve.
	names2, _ := svc.ManagedIdentifiersForAgent("agent-1", "")
	if _, ok := names2["beta"]; ok {
		t.Fatal("no-fleet lookup resolved via fleet-group: want direct-only")
	}
}

func TestServiceResolveIDByNameForAgent(t *testing.T) {
	t.Parallel()

	svc := NewService()
	t0 := time.Now()
	svc.ReplaceInMemory(
		Client{ID: "client-1", Name: "alpha", CreatedAt: t0},
		[]Assignment{{ID: "a1", ClientID: "client-1", TargetType: TargetTypeAgent, AgentID: "agent-1", CreatedAt: t0}},
		nil,
	)
	if got := svc.ResolveIDByNameForAgent("agent-1", "", "alpha"); got != "client-1" {
		t.Fatalf("ResolveIDByNameForAgent: got %q want client-1", got)
	}
	if got := svc.ResolveIDByNameForAgent("agent-1", "", "missing"); got != "" {
		t.Fatalf("ResolveIDByNameForAgent miss: got %q want empty", got)
	}
}

func TestServiceAggregatedUsage(t *testing.T) {
	t.Parallel()

	svc := NewService()
	t0 := time.Now()
	svc.SeedUsage("client-1", "agent-a", 100, 2, 3, t0)
	svc.SeedUsage("client-1", "agent-b", 50, 1, 1, t0)

	agg := svc.AggregatedUsage("client-1")
	if agg.TrafficUsedBytes != 150 {
		t.Fatalf("AggregatedUsage traffic: %d want 150", agg.TrafficUsedBytes)
	}
	if agg.ActiveTCPConns != 3 {
		t.Fatalf("AggregatedUsage conns: %d want 3", agg.ActiveTCPConns)
	}

	zero := svc.AggregatedUsage("missing")
	if zero != (AggregatedUsage{}) {
		t.Fatalf("AggregatedUsage(missing): got %+v want zero", zero)
	}
}

func TestServiceApplyUsageSnapshotSeqDedup(t *testing.T) {
	t.Parallel()

	svc := NewService()
	t0 := time.Now()
	known := map[string]struct{}{"client-1": {}}

	// First batch lands (seq=5).
	svc.ApplyUsageSnapshot("agent-a", []UsageSnapshot{{ClientID: "client-1", TrafficUsedBytes: 100, Seq: 5, ObservedAt: t0}}, known)
	if got := svc.AggregatedUsage("client-1").TrafficUsedBytes; got != 100 {
		t.Fatalf("after first batch: traffic %d want 100", got)
	}

	// Stale replay (seq=3) must be dropped.
	svc.ApplyUsageSnapshot("agent-a", []UsageSnapshot{{ClientID: "client-1", TrafficUsedBytes: 999, Seq: 3, ObservedAt: t0}}, known)
	if got := svc.AggregatedUsage("client-1").TrafficUsedBytes; got != 100 {
		t.Fatalf("after stale replay: traffic %d want 100 (replay must be ignored)", got)
	}

	// Next seq progresses.
	svc.ApplyUsageSnapshot("agent-a", []UsageSnapshot{{ClientID: "client-1", TrafficUsedBytes: 200, Seq: 6, ObservedAt: t0}}, known)
	if got := svc.AggregatedUsage("client-1").TrafficUsedBytes; got != 200 {
		t.Fatalf("after fresh seq: traffic %d want 200", got)
	}

	// Agent restart: seq resets to 1, new baseline is accepted.
	svc.ApplyUsageSnapshot("agent-a", []UsageSnapshot{{ClientID: "client-1", TrafficUsedBytes: 50, Seq: 1, ObservedAt: t0}}, known)
	if got := svc.AggregatedUsage("client-1").TrafficUsedBytes; got != 50 {
		t.Fatalf("after seq=1 restart: traffic %d want 50", got)
	}

	// Unknown client must be filtered.
	svc.ApplyUsageSnapshot("agent-a", []UsageSnapshot{{ClientID: "unmanaged", TrafficUsedBytes: 10, Seq: 2, ObservedAt: t0}}, known)
	if svc.AggregatedUsage("unmanaged").TrafficUsedBytes != 0 {
		t.Fatal("unknown client must not be recorded")
	}
}

func TestServiceDropAgentUsage(t *testing.T) {
	t.Parallel()

	svc := NewService()
	t0 := time.Now()
	svc.SeedUsage("client-1", "agent-a", 10, 0, 0, t0)
	svc.SeedUsage("client-1", "agent-b", 20, 0, 0, t0)
	svc.SeedUsage("client-2", "agent-a", 30, 0, 0, t0)

	svc.DropAgentUsage("agent-a")
	if got := svc.AggregatedUsage("client-1").TrafficUsedBytes; got != 20 {
		t.Fatalf("after drop agent-a: client-1 traffic %d want 20", got)
	}
	if got := svc.AggregatedUsage("client-2").TrafficUsedBytes; got != 0 {
		t.Fatalf("after drop agent-a: client-2 traffic %d want 0", got)
	}
}

func TestServiceRestoreFromRecords(t *testing.T) {
	t.Parallel()

	svc := NewService()
	t0 := time.Date(2024, 2, 3, 4, 5, 6, 0, time.UTC)
	svc.RestoreFromRecords(
		[]storage.ClientRecord{{ID: "client-0000042", Name: "alpha", SecretCiphertext: "s", CreatedAt: t0, UpdatedAt: t0}},
		[]storage.ClientAssignmentRecord{{ID: "client-assignment-0000005", ClientID: "client-0000042", TargetType: TargetTypeAgent, AgentID: "agent-1", CreatedAt: t0}},
		[]storage.ClientDeploymentRecord{{ClientID: "client-0000042", AgentID: "agent-1", Status: DeploymentStatusSucceeded, UpdatedAt: t0}},
	)

	client, assignments, deployments, err := svc.DetailSnapshot("client-0000042")
	if err != nil {
		t.Fatalf("DetailSnapshot after restore: err=%v", err)
	}
	if client.Name != "alpha" {
		t.Fatalf("restored client name: got %q want alpha", client.Name)
	}
	if len(assignments) != 1 || len(deployments) != 1 {
		t.Fatalf("restored assignments=%d deployments=%d want 1,1", len(assignments), len(deployments))
	}

	// Sequence counters must be advanced past restored IDs.
	if got := svc.NextClientID(); got != "client-0000043" {
		t.Fatalf("after restore: NextClientID = %q want client-0000043", got)
	}
	if got := svc.NextAssignmentID(); got != "client-assignment-0000006" {
		t.Fatalf("after restore: NextAssignmentID = %q", got)
	}
}

func TestBuildDeploymentsAdditionsRetainAndRemove(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	current := []Deployment{
		{ClientID: "c1", AgentID: "agent-old", DesiredOperation: "client.update", Status: DeploymentStatusSucceeded, UpdatedAt: t0},
		{ClientID: "c1", AgentID: "agent-keep", DesiredOperation: "client.update", Status: DeploymentStatusSucceeded, UpdatedAt: t0},
	}
	next := BuildDeployments(current, "c1", []string{"agent-keep", "agent-new"}, "client.update", "client.delete", t0)
	if len(next) != 3 {
		t.Fatalf("BuildDeployments: len=%d want 3 (keep + new + old marked delete)", len(next))
	}
	byAgent := map[string]Deployment{}
	for _, d := range next {
		byAgent[d.AgentID] = d
	}
	if byAgent["agent-new"].DesiredOperation != "client.update" {
		t.Fatalf("new target: op=%q want client.update", byAgent["agent-new"].DesiredOperation)
	}
	if byAgent["agent-old"].DesiredOperation != "client.delete" {
		t.Fatalf("removed target: op=%q want client.delete", byAgent["agent-old"].DesiredOperation)
	}
	if byAgent["agent-keep"].Status != DeploymentStatusQueued {
		t.Fatalf("kept target must be re-queued: got %q", byAgent["agent-keep"].Status)
	}

	// When the desired operation is already delete, stranded rows are left alone.
	current2 := []Deployment{{ClientID: "c1", AgentID: "agent-a", DesiredOperation: "client.update", Status: DeploymentStatusSucceeded, UpdatedAt: t0}}
	next2 := BuildDeployments(current2, "c1", []string{"agent-a"}, "client.delete", "client.delete", t0)
	if len(next2) != 1 || next2[0].DesiredOperation != "client.delete" {
		t.Fatalf("delete path: got %+v", next2)
	}
}

func TestRemovedTargetAgentIDs(t *testing.T) {
	t.Parallel()

	got := RemovedTargetAgentIDs(
		[]Deployment{{AgentID: "a"}, {AgentID: "b"}, {AgentID: "c"}},
		[]string{"b"},
	)
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("RemovedTargetAgentIDs: got %+v want [a c]", got)
	}
}

func TestDeploymentAgentIDs(t *testing.T) {
	t.Parallel()

	got := DeploymentAgentIDs([]Deployment{{AgentID: "c"}, {AgentID: "a"}, {AgentID: "b"}})
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("DeploymentAgentIDs: got %+v want sorted [a b c]", got)
	}
}
