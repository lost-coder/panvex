package server

import (
	"context"
	"sort"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// restoreStoredClients populates the server's in-memory client mirrors.
//
// Phase 7 migration: when clientsSvc was wired with NewServiceV2 (i.e.
// HasRepo() is true), this delegates all persistence reads to
// clients.Service.Restore and then syncs the Service's V2 mirror back into
// the server's legacy maps so existing handlers continue to work.
//
// The legacy maps (s.clients, s.clientAssignments, etc.) are KEPT during
// Phase 7 to avoid a too-large cascade; Phase 8 will migrate every reader
// to call s.clientsSvc.DetailSnapshot / ListSnapshot directly and remove
// the maps from the Server struct.
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

	if !s.clientsSvc.HasRepo() {
		// NewServiceV2 not wired (no DB or unknown store type). Run the
		// legacy store-backed restore so context cancellation is still
		// honoured (BP-01: the WithTimeout above is parented to serverCtx).
		return s.restoreStoredClientsLegacy(ctx)
	}

	// Delegate all persistence reads to clients.Service.Restore which uses
	// the domain Repository rather than storage.Store directly.
	if err := s.clientsSvc.Restore(ctx); err != nil {
		return err
	}

	// Sync the Service's V2 mirror into the server's legacy maps.
	// Phase 8 will remove this step once every reader migrates to
	// s.clientsSvc.Get / DetailSnapshot / ListSnapshot.
	snap := s.clientsSvc.MirrorSnapshot()
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	for id, c := range snap.Clients {
		s.clients[string(id)] = c
		s.clientSeq = maxPrefixedSequence(s.clientSeq, "client", string(id))
	}
	for id, as := range snap.Assignments {
		s.clientAssignments[string(id)] = as
		for _, a := range as {
			s.assignmentSeq = maxPrefixedSequence(s.assignmentSeq, "client-assignment", string(a.ID))
		}
	}
	for id, byAgent := range snap.Deployments {
		if s.clientDeployments[string(id)] == nil {
			s.clientDeployments[string(id)] = make(map[string]managedClientDeployment)
		}
		for agentID, d := range byAgent {
			s.clientDeployments[string(id)][agentID] = d
		}
	}
	for id, byAgent := range snap.Usage {
		if s.clientUsage[string(id)] == nil {
			s.clientUsage[string(id)] = make(map[string]clientUsageSnapshot)
		}
		for agentID, u := range byAgent {
			s.clientUsage[string(id)][agentID] = clientUsageSnapshot{
				ClientID:         u.ClientID,
				TrafficUsedBytes: u.TrafficUsedBytes,
				UniqueIPsUsed:    u.UniqueIPsUsed,
				ActiveTCPConns:   u.ActiveTCPConns,
				ActiveUniqueIPs:  u.ActiveUniqueIPs,
				ObservedAt:       u.ObservedAt,
			}
			s.trackClientUsageOwnerLocked(string(id), agentID)
		}
	}
	for agentID, seq := range snap.LastUsageSeq {
		if seq > s.lastUsageSeq[agentID] {
			s.lastUsageSeq[agentID] = seq
		}
	}

	// Discovered-client seeding: when client_usage has no entry for a
	// (clientID, agentID) pair, fall back to discovered_clients.total_octets
	// as the initial traffic counter (mirrors the legacy rehydrateClientAssignmentUsage
	// behaviour).
	if s.discoveredRepo != nil {
		s.seedUsageFromDiscoveredLocked(ctx, snap)
	}
	return nil
}

// seedUsageFromDiscoveredLocked fills in zero-usage entries in s.clientUsage
// using discovered_clients.total_octets where available. Must be called with
// s.clientsMu held for writing.
func (s *Server) seedUsageFromDiscoveredLocked(ctx context.Context, snap clients.MirrorState) {
	dcRecs, err := s.discoveredRepo.List(ctx)
	if err != nil {
		s.logger.Warn("restore: list discovered clients for usage seed failed", "error", err)
		return
	}

	type dcSeed struct {
		totalOctets uint64
		uniqueIPs   int
		tcpConns    int
		updatedAt   time.Time
	}
	// Build index: agentID+\x00+clientName → seed values
	dcIdx := make(map[string]dcSeed, len(dcRecs))
	for _, r := range dcRecs {
		dcIdx[r.AgentID+"\x00"+r.ClientName] = dcSeed{
			totalOctets: r.TotalOctets,
			uniqueIPs:   int(r.ActiveUniqueIPs),    //nolint:gosec
			tcpConns:    int(r.CurrentConnections), //nolint:gosec
			updatedAt:   r.UpdatedAt,
		}
	}

	for id, c := range snap.Clients {
		assignments := snap.Assignments[id]
		for _, a := range assignments {
			if a.AgentID == "" {
				continue
			}
			// Only seed if there's no persisted usage entry.
			if byAgent, ok := s.clientUsage[string(id)]; ok {
				if _, hasUsage := byAgent[a.AgentID]; hasUsage {
					continue
				}
			}
			dc, ok := dcIdx[a.AgentID+"\x00"+c.Name]
			if !ok {
				continue
			}
			if s.clientUsage[string(id)] == nil {
				s.clientUsage[string(id)] = make(map[string]clientUsageSnapshot)
			}
			s.clientUsage[string(id)][a.AgentID] = clientUsageSnapshot{
				ClientID:         clients.ClientID(string(id)),
				TrafficUsedBytes: dc.totalOctets,
				UniqueIPsUsed:    dc.uniqueIPs,
				ActiveTCPConns:   dc.tcpConns,
				ActiveUniqueIPs:  dc.uniqueIPs,
				ObservedAt:       dc.updatedAt,
			}
			s.trackClientUsageOwnerLocked(string(id), a.AgentID)
		}
	}
}

// restoreStoredClientsLegacy is the pre-Wave-4.2 store-backed restore path.
// Only reached when clientsSvc was not wired with NewServiceV2 (i.e. the
// store does not expose DB() — e.g. a wrapped test-double). In practice this
// path is never taken in production; it exists so that the BP-01 context-
// cancellation invariant (the WithTimeout above is parented to serverCtx) is
// exercised by TestClientsState_RestoreHonoursServerCtxCancellation.
func (s *Server) restoreStoredClientsLegacy(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	records, err := s.store.ListClients(ctx) //nolint:staticcheck // legacy path; ClientStore deprecated but retained
	if err != nil {
		return err
	}
	for _, record := range records {
		decoded, err := clients.DecryptClientRecord(record, s.vault())
		if err != nil {
			return err
		}
		client := clients.ClientFromRecord(decoded)
		s.clients[string(client.ID)] = client
		s.clientSeq = maxPrefixedSequence(s.clientSeq, "client", string(client.ID))
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
