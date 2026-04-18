package clients

import (
	"sort"
	"time"
)

// BuildDeployments produces the next deployment rows for a client
// given the current set (indexed by AgentID), the resolved target
// agent IDs, and the desired operation. Rows for target agents are
// queued with `desiredOperation`; rows for agents no longer targeted
// are queued with `deleteOperation` so the server can reconcile the
// removal — unless `desiredOperation` already equals
// `deleteOperation`, in which case the stranded rows are left alone
// (they are already being torn down by the enclosing delete flow).
//
// Pure function — no locks, no I/O. Callers typically pass `current`
// from Service.DetailSnapshot and the resulting slice straight to
// Service.ReplaceState.
func BuildDeployments(
	current []Deployment,
	clientID string,
	targetAgentIDs []string,
	desiredOperation string,
	deleteOperation string,
	observedAt time.Time,
) []Deployment {
	currentByAgent := make(map[string]Deployment, len(current))
	for _, deployment := range current {
		currentByAgent[deployment.AgentID] = deployment
	}

	observedAtUTC := observedAt.UTC()

	targetSet := make(map[string]struct{}, len(targetAgentIDs))
	for _, agentID := range targetAgentIDs {
		targetSet[agentID] = struct{}{}
		deployment := currentByAgent[agentID]
		deployment.ClientID = clientID
		deployment.AgentID = agentID
		deployment.DesiredOperation = desiredOperation
		deployment.Status = DeploymentStatusQueued
		deployment.LastError = ""
		deployment.UpdatedAt = observedAtUTC
		currentByAgent[agentID] = deployment
	}

	if desiredOperation != deleteOperation {
		for agentID, deployment := range currentByAgent {
			if _, ok := targetSet[agentID]; ok {
				continue
			}
			deployment.DesiredOperation = deleteOperation
			deployment.Status = DeploymentStatusQueued
			deployment.LastError = ""
			deployment.UpdatedAt = observedAtUTC
			currentByAgent[agentID] = deployment
		}
	}

	result := make([]Deployment, 0, len(currentByAgent))
	for _, deployment := range currentByAgent {
		result = append(result, deployment)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].AgentID < result[right].AgentID
	})
	return result
}

// RemovedTargetAgentIDs returns the agent IDs present in the current
// deployment set but absent from the next target slice. Used to emit
// "remove client from these agents" jobs after an update shrinks the
// target set.
func RemovedTargetAgentIDs(current []Deployment, next []string) []string {
	nextSet := make(map[string]struct{}, len(next))
	for _, agentID := range next {
		nextSet[agentID] = struct{}{}
	}
	removed := make([]string, 0)
	for _, deployment := range current {
		if _, ok := nextSet[deployment.AgentID]; ok {
			continue
		}
		removed = append(removed, deployment.AgentID)
	}
	sort.Strings(removed)
	return removed
}

// DeploymentAgentIDs extracts the sorted unique agent IDs from a
// deployment slice.
func DeploymentAgentIDs(deployments []Deployment) []string {
	agentIDs := make([]string, 0, len(deployments))
	for _, deployment := range deployments {
		agentIDs = append(agentIDs, deployment.AgentID)
	}
	sort.Strings(agentIDs)
	return agentIDs
}
