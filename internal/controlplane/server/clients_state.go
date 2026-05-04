package server

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// restoreClientUsageIndexes builds two lookup tables used during
// restoreStoredClients to rehydrate volatile traffic counters: the
// primary `client_usage` rows keyed by (client_id, agent_id), and a
// fallback index of the latest discovered_clients snapshot keyed by
// (agent_id, client_name).
func (s *Server) restoreClientUsageIndexes(ctx context.Context) (map[string]storage.ClientUsageRecord, map[string]storage.DiscoveredClientRecord) {
	usageIdx := make(map[string]storage.ClientUsageRecord)
	if usage, err := s.store.ListClientUsage(ctx); err == nil {
		for _, u := range usage {
			usageIdx[u.ClientID+"\x00"+u.AgentID] = u
			if u.LastSeq > s.lastUsageSeq[u.AgentID] {
				s.lastUsageSeq[u.AgentID] = u.LastSeq
			}
		}
	}
	discoveredIdx := make(map[string]storage.DiscoveredClientRecord)
	if dc, err := s.store.ListDiscoveredClients(ctx); err == nil {
		for _, r := range dc {
			discoveredIdx[r.AgentID+"\x00"+r.ClientName] = r
		}
	}
	return usageIdx, discoveredIdx
}

// rehydrateClientAssignmentUsage restores the volatile traffic
// counter for a single (client, agent) pair. Prefers the persisted
// client_usage row; falls back to the latest discovered_clients
// snapshot when no usage row exists yet.
func (s *Server) rehydrateClientAssignmentUsage(ctx context.Context, client managedClient, assignment managedClientAssignment, usageIdx map[string]storage.ClientUsageRecord, discoveredIdx map[string]storage.DiscoveredClientRecord) {
	if assignment.AgentID == "" {
		return
	}
	if u, ok := usageIdx[client.ID+"\x00"+assignment.AgentID]; ok {
		if s.clientUsage[client.ID] == nil {
			s.clientUsage[client.ID] = make(map[string]clientUsageSnapshot)
		}
		s.clientUsage[client.ID][assignment.AgentID] = clientUsageSnapshot{
			ClientID:         u.ClientID,
			TrafficUsedBytes: u.TrafficUsedBytes,
			UniqueIPsUsed:    u.UniqueIPsUsed,
			ActiveTCPConns:   u.ActiveTCPConns,
			ActiveUniqueIPs:  u.ActiveUniqueIPs,
			ObservedAt:       u.ObservedAt,
		}
		return
	}
	if dc, ok := discoveredIdx[assignment.AgentID+"\x00"+client.Name]; ok {
		s.seedClientUsage(ctx, client.ID, assignment.AgentID, dc.TotalOctets,
			dc.CurrentConnections, dc.ActiveUniqueIPs, dc.UpdatedAt)
	}
}

// restoreClientAssignments loads + memoises the assignments for one
// client and rehydrates the volatile usage counter for each one.
func (s *Server) restoreClientAssignments(ctx context.Context, client managedClient, usageIdx map[string]storage.ClientUsageRecord, discoveredIdx map[string]storage.DiscoveredClientRecord) error {
	assignments, err := s.store.ListClientAssignments(ctx, client.ID)
	if err != nil {
		return err
	}
	s.clientAssignments[client.ID] = make([]managedClientAssignment, 0, len(assignments))
	for _, assignmentRecord := range assignments {
		assignment := clients.AssignmentFromRecord(assignmentRecord)
		s.clientAssignments[client.ID] = append(s.clientAssignments[client.ID], assignment)
		s.assignmentSeq = maxPrefixedSequence(s.assignmentSeq, "client-assignment", assignment.ID)
		s.rehydrateClientAssignmentUsage(ctx, client, assignment, usageIdx, discoveredIdx)
	}
	return nil
}

// restoreClientDeployments loads + memoises the per-agent deployment
// records for one client.
func (s *Server) restoreClientDeployments(ctx context.Context, clientID string) error {
	deployments, err := s.store.ListClientDeployments(ctx, clientID)
	if err != nil {
		return err
	}
	if s.clientDeployments[clientID] == nil {
		s.clientDeployments[clientID] = make(map[string]managedClientDeployment)
	}
	for _, deploymentRecord := range deployments {
		deployment := clients.DeploymentFromRecord(deploymentRecord)
		s.clientDeployments[clientID][deployment.AgentID] = deployment
	}
	return nil
}

func (s *Server) restoreStoredClients() error {
	if s.store == nil {
		return nil
	}

	// Q2.U-P-09: bound the entire restore sequence so a stuck DB cannot
	// hang startup forever. 60s covers a multi-thousand-row clients
	// table on commodity hardware with comfortable headroom. The parent
	// is the lifecycle context (Server.serverCtx) so a Close() during a
	// slow restore aborts rather than waiting the full 60s (BP-01).
	ctx, cancel := context.WithTimeout(s.serverCtx, 60*time.Second)
	defer cancel()

	records, err := s.store.ListClients(ctx)
	if err != nil {
		return err
	}

	usageIdx, discoveredIdx := s.restoreClientUsageIndexes(ctx)

	for _, record := range records {
		decoded, err := clients.DecryptClientRecord(record, s.vault())
		if err != nil {
			return fmt.Errorf("decrypt client %s: %w", record.ID, err)
		}
		client := clients.ClientFromRecord(decoded)
		s.clients[client.ID] = client
		s.clientSeq = maxPrefixedSequence(s.clientSeq, "client", client.ID)

		if err := s.restoreClientAssignments(ctx, client, usageIdx, discoveredIdx); err != nil {
			return err
		}
		if err := s.restoreClientDeployments(ctx, client.ID); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) listClientsSnapshot() []managedClient {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	result := make([]managedClient, 0, len(s.clients))
	for _, client := range s.clients {
		if client.DeletedAt != nil {
			continue
		}
		result = append(result, client)
	}

	sort.Slice(result, func(left, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].CreatedAt.Before(result[right].CreatedAt)
	})

	return result
}

// listClientsListingSnapshot returns every field handleClients needs in
// one pass under a single clientsMu RLock. It exists to fold the prior
// N×{clientDetailSnapshot, aggregatedClientUsage} pattern into a single
// lock acquire — under heavy lock contention the cumulative wall-clock
// difference is an order of magnitude on big fleets.
func (s *Server) listClientsListingSnapshot() clientListingSnapshot {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	clientsList := make([]managedClient, 0, len(s.clients))
	assignments := make(map[string][]managedClientAssignment, len(s.clients))
	deployments := make(map[string][]managedClientDeployment, len(s.clients))
	usage := make(map[string]aggregatedClientUsage, len(s.clients))

	for id, client := range s.clients {
		if client.DeletedAt != nil {
			continue
		}
		clientsList = append(clientsList, client)

		if rows := s.clientAssignments[id]; len(rows) > 0 {
			copyRows := append([]managedClientAssignment(nil), rows...)
			sort.Slice(copyRows, func(left, right int) bool {
				if copyRows[left].CreatedAt.Equal(copyRows[right].CreatedAt) {
					return copyRows[left].ID < copyRows[right].ID
				}
				return copyRows[left].CreatedAt.Before(copyRows[right].CreatedAt)
			})
			assignments[id] = copyRows
		}

		if depMap := s.clientDeployments[id]; len(depMap) > 0 {
			deps := make([]managedClientDeployment, 0, len(depMap))
			for _, deployment := range depMap {
				deps = append(deps, deployment)
			}
			sort.Slice(deps, func(left, right int) bool {
				return deps[left].AgentID < deps[right].AgentID
			})
			deployments[id] = deps
		}

		if usageByAgent := s.clientUsage[id]; len(usageByAgent) > 0 {
			snapshot := make(map[string]clients.UsageSnapshot, len(usageByAgent))
			for agentID, value := range usageByAgent {
				snapshot[agentID] = value
			}
			usage[id] = s.clientsSvc.AggregateUsage(snapshot)
		}
	}

	sort.Slice(clientsList, func(left, right int) bool {
		if clientsList[left].CreatedAt.Equal(clientsList[right].CreatedAt) {
			return clientsList[left].ID < clientsList[right].ID
		}
		return clientsList[left].CreatedAt.Before(clientsList[right].CreatedAt)
	})

	return clientListingSnapshot{
		clients:     clientsList,
		assignments: assignments,
		deployments: deployments,
		usage:       usage,
	}
}

func (s *Server) clientDetailSnapshot(clientID string) (managedClient, []managedClientAssignment, []managedClientDeployment, error) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	client, ok := s.clients[clientID]
	if !ok {
		return managedClient{}, nil, nil, storage.ErrNotFound
	}

	assignments := append([]managedClientAssignment(nil), s.clientAssignments[clientID]...)
	sort.Slice(assignments, func(left, right int) bool {
		if assignments[left].CreatedAt.Equal(assignments[right].CreatedAt) {
			return assignments[left].ID < assignments[right].ID
		}
		return assignments[left].CreatedAt.Before(assignments[right].CreatedAt)
	})

	deploymentsMap := s.clientDeployments[clientID]
	deployments := make([]managedClientDeployment, 0, len(deploymentsMap))
	for _, deployment := range deploymentsMap {
		deployments = append(deployments, deployment)
	}
	sort.Slice(deployments, func(left, right int) bool {
		return deployments[left].AgentID < deployments[right].AgentID
	})

	return client, assignments, deployments, nil
}
