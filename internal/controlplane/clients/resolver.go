package clients

import "sort"

// AgentTopology is the caller-supplied snapshot of the current
// agent/fleet state used by assignment-target resolution.
//
// RegisteredAgents is the set of agent IDs that are currently known to
// the control-plane. Assignments whose TargetType == TargetTypeAgent
// only resolve when the AgentID is present here.
//
// FleetMembers maps a FleetGroupID to the list of agent IDs in that
// group. Assignments whose TargetType == TargetTypeFleetGroup resolve
// to every agent in the corresponding entry.
//
// Both fields may be nil — a nil Topology resolves no assignments.
type AgentTopology struct {
	RegisteredAgents map[string]struct{}
	FleetMembers     map[string][]string
}

// ResolveTargetAgentIDs maps a slice of assignments to the concrete set
// of agent IDs they currently resolve to, given an AgentTopology
// snapshot. Result is deduplicated and sorted for deterministic output.
//
// This is a pure function — the locking dance required to obtain a
// consistent AgentTopology is the caller's responsibility. The
// controlplane/server package documents the ordering (s.mu ->
// s.clientsMu) in clients_flow.go.
func ResolveTargetAgentIDs(assignments []Assignment, topology AgentTopology) []string {
	targetAgentIDs := make(map[string]struct{})
	for _, assignment := range assignments {
		switch assignment.TargetType {
		case TargetTypeFleetGroup:
			for _, agentID := range topology.FleetMembers[assignment.FleetGroupID] {
				targetAgentIDs[agentID] = struct{}{}
			}
		case TargetTypeAgent:
			if _, ok := topology.RegisteredAgents[assignment.AgentID]; ok {
				targetAgentIDs[assignment.AgentID] = struct{}{}
			}
		}
	}

	result := make([]string, 0, len(targetAgentIDs))
	for agentID := range targetAgentIDs {
		result = append(result, agentID)
	}
	sort.Strings(result)
	return result
}

// ResolveIDByName returns the managed-client ID that matches clientName
// on the given agent, or "" when no match is found.
//
// A client matches when it is either directly assigned to agentID OR
// assigned to a fleet group that agentID belongs to (agentFleetGroupID).
// This mirrors the fleet-group fallback fix from P2-LOG-07 / M-C3:
// without it, usage stats for clients attached via fleet-group
// assignments were silently dropped.
//
// Inputs:
//   - clients: snapshot keyed by client ID (DeletedAt clients pass through;
//     callers that want to skip tombstones should prefilter).
//   - assignmentsByClient: snapshot keyed by client ID.
//   - agentID, agentFleetGroupID, clientName: lookup parameters.
//
// This is a pure function. See controlplane/server/clients_flow.go for
// the lock-ordering discipline used to build consistent snapshots.
func ResolveIDByName(
	clients map[string]Client,
	assignmentsByClient map[string][]Assignment,
	agentID string,
	agentFleetGroupID string,
	clientName string,
) string {
	for clientID, client := range clients {
		if client.Name != clientName {
			continue
		}
		for _, assignment := range assignmentsByClient[clientID] {
			switch assignment.TargetType {
			case TargetTypeAgent:
				if assignment.AgentID == agentID {
					return clientID
				}
			case TargetTypeFleetGroup:
				if agentFleetGroupID != "" && assignment.FleetGroupID == agentFleetGroupID {
					return clientID
				}
			}
		}
	}
	return ""
}

// AggregateUsage sums a per-agent UsageSnapshot map into a single
// AggregatedUsage. A nil or empty map yields the zero value. Pure.
func AggregateUsage(usageByAgent map[string]UsageSnapshot) AggregatedUsage {
	usage := AggregatedUsage{}
	for _, snapshot := range usageByAgent {
		usage.TrafficUsedBytes += snapshot.TrafficUsedBytes
		usage.UniqueIPsUsed += snapshot.UniqueIPsUsed
		usage.ActiveTCPConns += snapshot.ActiveTCPConns
	}
	return usage
}
