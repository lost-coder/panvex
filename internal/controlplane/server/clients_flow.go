package server

import (
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
)

// See the lock-ordering invariant comment above the Server struct
// (server.go) for the s.mu -> s.metricsAuditMu -> Service.mu discipline that
// resolveClientTargetAgentIDs below relies on.

// Client-mutation error sentinels are aliases to the clients-package
// sentinels so existing errors.Is checks (handleClientMutationError) keep
// matching after the mutation flows moved onto clients.Service, which
// returns the clients.Err* values directly.
var (
	errClientNameRequired    = clients.ErrNameRequired
	errClientNameInvalid     = clients.ErrNameInvalid
	errClientNameTaken       = clients.ErrNameTaken
	errClientNameImmutable   = clients.ErrNameImmutable
	errClientUserADTag       = clients.ErrUserADTag
	errClientExpiration      = clients.ErrExpiration
	errClientTargetsRequired = clients.ErrTargetsRequired
	errClientLimitNegative   = clients.ErrLimitNegative
)

// clientJobTTL is the compiled-in default TTL for client-mutation jobs.
// The live value is resolved via s.effectiveClientJobTTL() so operator
// changes to jobs.client_job_ttl take effect without a panel restart.
const clientJobTTL = 10 * time.Minute

// effectiveClientJobTTL returns the current client-job TTL. When the
// operational settings store is wired, the live value is used; falls back
// to the compiled-in constant otherwise.
func (s *Server) effectiveClientJobTTL() time.Duration {
	if s.settings != nil {
		return s.settings.JobsClientJobTTL()
	}
	return clientJobTTL
}

// resolveClientTargetAgentIDs snapshots the current agent topology under
// the live store and delegates the deterministic deduplication + sorting to
// clients.Service.ResolveTargetAgentIDs.
//
// Lock discipline (P2-LOG-11 / M-C11 / L-08): the assignments are read from
// the clients.Service mirror (its own lock) by the caller. The registered-
// agent IDs and fleet-group membership are snapshotted from s.live (its own
// lock) into local maps, and the pure helper iterates against those local
// snapshots. The snapshot can race with a concurrent agent mutation, but callers tolerate
// that: the result builds deployment rows that are re-reconciled on the next
// snapshot. The race is benign and lock-order-safe.
func (s *Server) resolveClientTargetAgentIDs(assignments []managedClientAssignment) []string {
	liveAgents := s.live.List()
	registeredAgents := make(map[string]struct{}, len(liveAgents))
	fleetMembers := make(map[string][]string)
	for _, agent := range liveAgents {
		registeredAgents[agent.ID] = struct{}{}
		if agent.FleetGroupID != "" {
			fleetMembers[agent.FleetGroupID] = append(fleetMembers[agent.FleetGroupID], agent.ID)
		}
	}

	return s.clientsSvc.ResolveTargetAgentIDs(assignments, clients.AgentTopology{
		RegisteredAgents: registeredAgents,
		FleetMembers:     fleetMembers,
	})
}

// normalizedIDs delegates to clients.NormalizedIDs.
func normalizedIDs(values []string) []string {
	return clients.NormalizedIDs(values)
}
