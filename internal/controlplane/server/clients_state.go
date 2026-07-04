package server

import (
	"context"
	"sort"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// restoreStoredClients rehydrates the clients.Service mirror from the
// Repository and seeds the Service's monotonic ID sequences so post-restart
// allocations never collide with persisted IDs. clients.Service is the
// single owner of client/assignment/deployment/usage state — the Server no
// longer keeps its own copy.
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
		// NewService not wired (no DB or unknown store type) — nothing to restore.
		return nil
	}

	// Delegate all persistence reads to clients.Service.Restore which uses
	// the domain Repository rather than storage.Store directly. Restore also
	// seeds the monotonic ID sequence counters internally.
	if err := s.clientsSvc.Restore(ctx); err != nil {
		return err
	}

	// Backfill subscription tokens for clients that pre-date the feature.
	// Non-fatal: a partial failure is logged but does not abort startup — the
	// panel must still serve, and missing tokens are repaired on next restart.
	if n, err := s.clientsSvc.BackfillSubscriptionTokens(ctx); err != nil {
		s.logger.ErrorContext(ctx, "startup: subscription token backfill failed", "error", err, "updated_so_far", n)
	} else if n > 0 {
		s.logger.InfoContext(ctx, "startup: subscription token backfill complete", "clients_updated", n)
	}

	// Discovered-client seeding: when client_usage has no entry for a
	// (clientID, agentID) pair, fall back to discovered_clients.total_octets
	// as the initial traffic counter (mirrors the legacy
	// rehydrateClientAssignmentUsage behaviour). Mirror-only, no write-through.
	if s.discoveredRepo != nil {
		snap := s.clientsSvc.MirrorSnapshot()
		s.seedUsageFromDiscovered(ctx, snap)
	}
	return nil
}

// seedUsageFromDiscovered fills in zero-usage entries in the clients.Service
// mirror using discovered_clients.total_octets where available. Mirror-only
// (SeedUsageMirror writes no DB row), preserving the legacy non-persisting
// fallback semantics.
func (s *Server) seedUsageFromDiscovered(ctx context.Context, snap clients.MirrorState) {
	dcRecs, err := s.discoveredRepo.List(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "restore: list discovered clients for usage seed failed", "error", err)
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
			if byAgent, ok := snap.Usage[id]; ok {
				if _, hasUsage := byAgent[a.AgentID]; hasUsage {
					continue
				}
			}
			dc, ok := dcIdx[a.AgentID+"\x00"+c.Name]
			if !ok {
				continue
			}
			// Seed the discovered-fallback usage into the clients.Service
			// mirror. Mirror-only (no write-through) to preserve the legacy
			// non-persisting fallback semantics. SeedUsageMirror is a no-op
			// when a row already exists.
			s.clientsSvc.SeedUsageMirror(string(id), a.AgentID, dc.totalOctets, dc.tcpConns, dc.uniqueIPs, dc.updatedAt)
		}
	}
}

// listClientsListingSnapshot returns every field handleClients needs in
// one pass. It sources all listing data from the clients.Service mirror
// (the single owner of client/assignment/deployment/usage state). The mirror
// is kept current on every write path (SaveState / PersistDeployment /
// UpsertUsage*), so the projected JSON is identical to the prior server-map
// read.
func (s *Server) listClientsListingSnapshot() clientListingSnapshot {
	// MirrorSnapshot returns a deep copy under the Service's own read lock.
	mirror := s.clientsSvc.MirrorSnapshot()

	clientsList := make([]managedClient, 0, len(mirror.Clients))
	assignments := make(map[string][]managedClientAssignment, len(mirror.Clients))
	deployments := make(map[string][]managedClientDeployment, len(mirror.Clients))
	usage := make(map[string]aggregatedClientUsage, len(mirror.Clients))

	for clientID, client := range mirror.Clients {
		if client.DeletedAt != nil {
			continue
		}
		id := string(clientID)
		clientsList = append(clientsList, client)

		if rows := mirror.Assignments[clientID]; len(rows) > 0 {
			copyRows := append([]managedClientAssignment(nil), rows...)
			sort.Slice(copyRows, func(left, right int) bool {
				if copyRows[left].CreatedAt.Equal(copyRows[right].CreatedAt) {
					return copyRows[left].ID < copyRows[right].ID
				}
				return copyRows[left].CreatedAt.Before(copyRows[right].CreatedAt)
			})
			assignments[id] = copyRows
		}

		if depMap := mirror.Deployments[clientID]; len(depMap) > 0 {
			deps := make([]managedClientDeployment, 0, len(depMap))
			for _, deployment := range depMap {
				deps = append(deps, deployment)
			}
			sort.Slice(deps, func(left, right int) bool {
				return deps[left].AgentID < deps[right].AgentID
			})
			deployments[id] = deps
		}

		if usageByAgent := mirror.Usage[clientID]; len(usageByAgent) > 0 {
			snapshot := make(map[string]clients.UsageSnapshot, len(usageByAgent))
			for agentID, value := range usageByAgent {
				snapshot[agentID] = clients.UsageSnapshot{
					ClientID:           value.ClientID,
					TrafficUsedBytes:   value.TrafficUsedBytes,
					UniqueIPsUsed:      value.UniqueIPsUsed,
					ActiveTCPConns:     value.ActiveTCPConns,
					ActiveUniqueIPs:    value.ActiveUniqueIPs,
					QuotaUsedBytes:     value.QuotaUsedBytes,
					QuotaLastResetUnix: value.QuotaLastResetUnix,
					ObservedAt:         value.ObservedAt,
				}
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
	// Sources detail data from the clients.Service mirror (the single
	// owner of client/assignment/deployment state). The mirror is kept
	// current on every write path (SaveState / PersistDeployment /
	// UpsertUsage*), so the projected shape and sort order are identical to
	// the prior server-map read.
	mirror := s.clientsSvc.MirrorSnapshot()

	cid := clients.ClientID(clientID)
	client, ok := mirror.Clients[cid]
	if !ok {
		return managedClient{}, nil, nil, storage.ErrNotFound
	}

	assignments := append([]managedClientAssignment(nil), mirror.Assignments[cid]...)
	sort.Slice(assignments, func(left, right int) bool {
		if assignments[left].CreatedAt.Equal(assignments[right].CreatedAt) {
			return assignments[left].ID < assignments[right].ID
		}
		return assignments[left].CreatedAt.Before(assignments[right].CreatedAt)
	})

	deploymentsMap := mirror.Deployments[cid]
	deployments := make([]managedClientDeployment, 0, len(deploymentsMap))
	for _, deployment := range deploymentsMap {
		deployments = append(deployments, deployment)
	}
	sort.Slice(deployments, func(left, right int) bool {
		return deployments[left].AgentID < deployments[right].AgentID
	})

	return client, assignments, deployments, nil
}
