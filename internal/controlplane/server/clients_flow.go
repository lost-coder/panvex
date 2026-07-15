package server

import (
	"context"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
)

// Lock ordering invariant for the Server struct (P2-LOG-11 / M-C11 / L-08):
//
//	s.mu  ->  s.metricsAuditMu
//
// Whenever two of these locks must be observed together, they MUST be taken
// in the order above and released in the reverse order.
//
// Client/usage/deployment state is owned by clients.Service, which guards it
// with its own internal lock. The server takes the Service lock (via Service
// methods) while holding s.mu in applyAgentSnapshot/purgeAgentInMemory, so the
// effective ordering is s.mu -> Service.mu. Functions that need data from BOTH
// the agent maps and the Service snapshot the agent fields under s.mu, release
// it, then call into the Service — they never nest the two. See
// resolveClientTargetAgentIDs below for the snapshot pattern.

// Client-mutation error sentinels are aliases to the clients-package
// sentinels so existing errors.Is checks (handleClientMutationError) keep
// matching after the mutation flows moved onto clients.Service, which
// returns the clients.Err* values directly.
var (
	errClientNameRequired    = clients.ErrNameRequired
	errClientNameInvalid     = clients.ErrNameInvalid
	errClientNameTaken       = clients.ErrNameTaken
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

func (s *Server) replaceClientStateWithContext(ctx context.Context, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) error {
	if s.clientsSvc.HasRepo() {
		// NewService path: SaveState atomically writes to the Repository and
		// updates the Service mirror (the single owner of client state).
		if err := s.clientsSvc.SaveState(ctx, client, assignments, deployments); err != nil {
			return err
		}
	} else {
		// No-repo fallback (test doubles / pre-migrate stores): update the
		// in-memory mirror directly.
		s.replaceClientStateInMemory(client, assignments, deployments)
	}
	s.publishClientsUpdated(client.ID)
	return nil
}

// replaceClientStateInMemory updates the clients.Service mirror for one
// client without touching the store. Factored out of
// replaceClientStateWithContext so callers that drive persistence through a
// UnitOfWork can apply the in-memory update only after the transaction
// commits (see adoptDiscoveredClient, P2-ARCH-01). For the SaveState path
// the mirror was already updated inside SaveState, so this is an idempotent
// re-write with the same (plaintext-secret) values.
func (s *Server) replaceClientStateInMemory(client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment) {
	s.clientsSvc.MirrorReplaceInMemory(client, assignments, deployments)
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

func (s *Server) nextClientID() clients.ClientID {
	return clients.ClientID(s.clientsSvc.NextClientID())
}

func (s *Server) nextClientAssignmentID() clients.AssignmentID {
	return clients.AssignmentID(s.clientsSvc.NextAssignmentID())
}

// normalizedIDs delegates to clients.NormalizedIDs.
func normalizedIDs(values []string) []string {
	return clients.NormalizedIDs(values)
}

// normalizedExpiration delegates to clients.NormalizeExpiration.
func normalizedExpiration(value string) (string, error) {
	out, err := clients.NormalizeExpiration(value)
	if errors.Is(err, clients.ErrExpiration) {
		return "", errClientExpiration
	}
	return out, err
}

// randomHexString delegates to clients.RandomHexString.
func randomHexString(size int) (string, error) {
	return clients.RandomHexString(size)
}

// isValidHexSecret delegates to clients.IsValidHexSecret.
func isValidHexSecret(s string) bool {
	return clients.IsValidHexSecret(s)
}
