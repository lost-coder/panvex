package server

import (
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// Compile-time check: *Server is the clients.Deps implementation. Wiring
// only (R8a Task 2) — the orchestration methods on Service that will
// eventually call these (Tasks 3-4) do not exist yet; the existing
// server-side flows in clients_flow.go / clients_jobs.go are unchanged and
// keep being used directly.
var _ clients.Deps = (*Server)(nil)

// Topology snapshots the currently-registered agents and fleet-group
// membership from the live store. VERBATIM copy of the snapshot half of
// resolveClientTargetAgentIDs (clients_flow.go) — the pure
// dedup/sort delegation to clients.Service.ResolveTargetAgentIDs stays on
// that method for now.
func (s *Server) Topology() clients.AgentTopology {
	liveAgents := s.live.List()
	registeredAgents := make(map[string]struct{}, len(liveAgents))
	fleetMembers := make(map[string][]string)
	for _, agent := range liveAgents {
		registeredAgents[agent.ID] = struct{}{}
		if agent.FleetGroupID != "" {
			fleetMembers[agent.FleetGroupID] = append(fleetMembers[agent.FleetGroupID], agent.ID)
		}
	}
	return clients.AgentTopology{
		RegisteredAgents: registeredAgents,
		FleetMembers:     fleetMembers,
	}
}

// ReadOnlyAgents reports the read-only flag for each requested agent that is
// currently registered. VERBATIM copy of the readOnlyAgents-map loop shared
// by enqueueClientResetQuotaJob and enqueueClientJob (clients_jobs.go).
func (s *Server) ReadOnlyAgents(agentIDs []string) map[string]bool {
	readOnlyAgents := make(map[string]bool, len(agentIDs))
	for _, agentID := range agentIDs {
		if agent, ok := s.live.Get(agentID); ok {
			readOnlyAgents[agentID] = agent.ReadOnly
		}
	}
	return readOnlyAgents
}

// AgentFleetGroupID returns the fleet group of a registered agent. VERBATIM
// copy of the s.live.Get half of resolveClientIDByName (clients_flow.go).
func (s *Server) AgentFleetGroupID(agentID string) (string, bool) {
	agent, ok := s.live.Get(agentID)
	if !ok {
		return "", false
	}
	return agent.FleetGroupID, true
}

// NotifyAgentSessions pokes the gRPC sessions of the given agents.
func (s *Server) NotifyAgentSessions(agentIDs []string) { s.notifyAgentSessions(agentIDs) }

// PublishJobCreated forwards to the event bus.
func (s *Server) PublishJobCreated(job jobs.Job) { s.publishJobCreated(job) }

// PublishClientsUpdated forwards to the event bus.
func (s *Server) PublishClientsUpdated(clientID clients.ClientID) { s.publishClientsUpdated(clientID) }

// ClientJobTTL resolves the live client-job TTL. VERBATIM delegation to
// effectiveClientJobTTL (clients_flow.go); effectiveClientJobTTL itself is
// removed in Task 3 once orchestration moves onto Service.
func (s *Server) ClientJobTTL() time.Duration {
	return s.effectiveClientJobTTL()
}
