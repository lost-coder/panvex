package clients

import (
	"testing"
	"time"
)

func TestServiceSequenceHelpers(t *testing.T) {
	t.Parallel()

	svc := NewServiceV2(ServiceConfig{})
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

func TestServiceSetNow(t *testing.T) {
	t.Parallel()

	svc := NewServiceV2(ServiceConfig{})
	if svc.now == nil {
		t.Fatal("NewServiceV2: now must default to time.Now")
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
