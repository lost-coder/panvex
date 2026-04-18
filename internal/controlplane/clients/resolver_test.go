package clients

import (
	"reflect"
	"testing"
)

func TestResolveTargetAgentIDs(t *testing.T) {
	t.Parallel()

	topology := AgentTopology{
		RegisteredAgents: map[string]struct{}{
			"agent-1": {},
			"agent-2": {},
			"agent-3": {},
		},
		FleetMembers: map[string][]string{
			"fleet-a": {"agent-1", "agent-2"},
			"fleet-b": {"agent-3"},
		},
	}

	cases := []struct {
		name        string
		assignments []Assignment
		want        []string
	}{
		{
			name:        "no assignments",
			assignments: nil,
			want:        []string{},
		},
		{
			name: "direct agent assignment resolves when registered",
			assignments: []Assignment{
				{TargetType: TargetTypeAgent, AgentID: "agent-1"},
			},
			want: []string{"agent-1"},
		},
		{
			name: "direct agent assignment drops unknown agent",
			assignments: []Assignment{
				{TargetType: TargetTypeAgent, AgentID: "ghost"},
			},
			want: []string{},
		},
		{
			name: "fleet-group expands to members",
			assignments: []Assignment{
				{TargetType: TargetTypeFleetGroup, FleetGroupID: "fleet-a"},
			},
			want: []string{"agent-1", "agent-2"},
		},
		{
			name: "dedupe across fleet and direct",
			assignments: []Assignment{
				{TargetType: TargetTypeFleetGroup, FleetGroupID: "fleet-a"},
				{TargetType: TargetTypeAgent, AgentID: "agent-2"},
				{TargetType: TargetTypeAgent, AgentID: "agent-3"},
			},
			want: []string{"agent-1", "agent-2", "agent-3"},
		},
		{
			name: "unknown target-type ignored",
			assignments: []Assignment{
				{TargetType: "weird", AgentID: "agent-1"},
			},
			want: []string{},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveTargetAgentIDs(tc.assignments, topology)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResolveIDByName(t *testing.T) {
	t.Parallel()

	clientsByID := map[string]Client{
		"client-1": {ID: "client-1", Name: "alice"},
		"client-2": {ID: "client-2", Name: "bob"},
		"client-3": {ID: "client-3", Name: "alice"}, // shadow name on different assignment
	}
	assignmentsByClient := map[string][]Assignment{
		"client-1": {{ClientID: "client-1", TargetType: TargetTypeAgent, AgentID: "agent-1"}},
		"client-2": {{ClientID: "client-2", TargetType: TargetTypeFleetGroup, FleetGroupID: "fleet-a"}},
		"client-3": {{ClientID: "client-3", TargetType: TargetTypeAgent, AgentID: "agent-2"}},
	}

	t.Run("direct agent match", func(t *testing.T) {
		t.Parallel()
		got := ResolveIDByName(clientsByID, assignmentsByClient, "agent-1", "", "alice")
		if got != "client-1" {
			t.Fatalf("got %q, want client-1", got)
		}
	})

	t.Run("fleet-group fallback match", func(t *testing.T) {
		t.Parallel()
		got := ResolveIDByName(clientsByID, assignmentsByClient, "agent-7", "fleet-a", "bob")
		if got != "client-2" {
			t.Fatalf("got %q, want client-2", got)
		}
	})

	t.Run("fleet-group ID empty skips fallback", func(t *testing.T) {
		t.Parallel()
		got := ResolveIDByName(clientsByID, assignmentsByClient, "agent-7", "", "bob")
		if got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})

	t.Run("no match returns empty", func(t *testing.T) {
		t.Parallel()
		got := ResolveIDByName(clientsByID, assignmentsByClient, "agent-1", "", "charlie")
		if got != "" {
			t.Fatalf("got %q, want empty", got)
		}
	})
}

func TestAggregateUsage(t *testing.T) {
	t.Parallel()

	t.Run("empty map returns zero value", func(t *testing.T) {
		t.Parallel()
		got := AggregateUsage(nil)
		if got != (AggregatedUsage{}) {
			t.Fatalf("got %+v, want zero", got)
		}
	})

	t.Run("sums across agents", func(t *testing.T) {
		t.Parallel()
		in := map[string]UsageSnapshot{
			"agent-1": {TrafficUsedBytes: 100, UniqueIPsUsed: 3, ActiveTCPConns: 2},
			"agent-2": {TrafficUsedBytes: 50, UniqueIPsUsed: 1, ActiveTCPConns: 5},
		}
		got := AggregateUsage(in)
		want := AggregatedUsage{TrafficUsedBytes: 150, UniqueIPsUsed: 4, ActiveTCPConns: 7}
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	})
}

func TestServiceWrappers(t *testing.T) {
	t.Parallel()

	svc := NewService()
	if svc == nil {
		t.Fatalf("NewService returned nil")
	}
	if !svc.ValidateHexSecret("00112233445566778899aabbccddeeff") {
		t.Fatalf("ValidateHexSecret rejected a known-valid hex secret")
	}
	got := svc.ResolveTargetAgentIDs(nil, AgentTopology{})
	if len(got) != 0 {
		t.Fatalf("Service.ResolveTargetAgentIDs on empty input returned %v", got)
	}
	if svc.ResolveIDByName(nil, nil, "a", "", "name") != "" {
		t.Fatalf("Service.ResolveIDByName on empty maps returned non-empty")
	}
	if svc.AggregateUsage(nil) != (AggregatedUsage{}) {
		t.Fatalf("Service.AggregateUsage on nil returned non-zero")
	}
}
