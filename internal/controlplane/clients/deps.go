package clients

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// Deps is the narrow surface the clients orchestration needs from the
// hosting process (the control-plane server). Defined on the domain side
// so the dependency edge stays server -> clients (the gateway.Deps
// pattern, P8.2d). Implemented by *server.Server in server/clients_deps.go.
type Deps interface {
	// Topology snapshots the currently-registered agents and fleet-group
	// membership from the live store. Callers never hold Service.mu while
	// calling this (lock ordering: Server.mu -> Service.mu).
	Topology() AgentTopology
	// ReadOnlyAgents reports the read-only flag for each requested agent
	// that is currently registered. Missing agents are simply absent.
	ReadOnlyAgents(agentIDs []string) map[string]bool
	// AgentFleetGroupID returns the fleet group of a registered agent
	// (used by usage-snapshot name resolution).
	AgentFleetGroupID(agentID string) (string, bool)
	// NotifyAgentSessions pokes the gRPC sessions of the given agents so
	// they pull the freshly-enqueued job without waiting for a tick.
	NotifyAgentSessions(agentIDs []string)
	// PublishJobCreated / PublishClientsUpdated forward to the event bus.
	PublishJobCreated(job jobs.Job)
	PublishClientsUpdated(clientID ClientID)
	// ClientJobTTL resolves the live client-job TTL (operator settings,
	// fallback to the compiled-in default).
	ClientJobTTL() time.Duration
}

// JobQueue is the jobs.Service subset the orchestration uses.
// *jobs.Service satisfies it directly (asserted below).
type JobQueue interface {
	Enqueue(ctx context.Context, input jobs.CreateJobInput, now time.Time) (jobs.Job, error)
	Get(jobID string) (jobs.Job, bool)
}

// Compile-time proof that *jobs.Service structurally satisfies JobQueue.
// Safe here (non-test file): jobs does not import clients, so this does
// not create an import cycle.
var _ JobQueue = (*jobs.Service)(nil)
