package agenttransport

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// transportQueries is the subset of dbsqlc.Queries that Manager calls. Lives
// in agenttransport so tests can supply a fake without depending on dbsqlc
// internals.
type transportQueries interface {
	GetAgentTransport(ctx context.Context, id string) (dbsqlc.GetAgentTransportRow, error)
	ListAgentsByTransportMode(ctx context.Context, transportMode string) ([]dbsqlc.ListAgentsByTransportModeRow, error)
}

// SessionHandler is the application-layer per-session handler. It runs the
// agent protocol over the given session and returns when the session ends.
type SessionHandler func(ctx context.Context, sess AgentSession, meta NodeMeta) error

// NodeMeta is the transport-layer view of an agent identity. Domain language
// uses "node" (per spec); the DB table is `agents`. NodeID == AgentID for the
// current single-agent-per-node schema; both fields are kept so the contract
// can grow without breaking call sites.
type NodeMeta struct {
	AgentID      string
	NodeID       string
	NodeName     string
	FleetGroupID string
}

// Manager owns the lifecycle of agent transports — both inbound (gRPC stream
// initiated by the agent) and outbound (panel dials a listening agent).
// The inbound field is a scaffold; the actual gRPC registration is done by
// cmd/control-plane via the regular Server until a future migration moves
// dispatch through Manager.
type Manager struct {
	// db is consulted by outbound supervisor restoration; nil is tolerated
	// while no outbound transport is active. A non-nil value is required
	// before any outbound flow runs.
	db       transportQueries
	handler  SessionHandler
	inbound  *inboundTransport
	outbound *outboundTransport
	logger   *slog.Logger

	mu      sync.Mutex
	started bool
	stopped bool
}

// ErrManagerStopped is returned by Start after Stop has been called. The
// Manager is a process-lifetime resource and is not designed to be restarted.
var ErrManagerStopped = errors.New("agenttransport: manager already stopped")

// NewManager creates a new Manager. db may be nil — OnNodeChanged returns early
// when db is nil (used in tests and during pre-wiring startup).
func NewManager(db *dbsqlc.Queries, handler SessionHandler, logger *slog.Logger) *Manager {
	var queries transportQueries
	if db != nil {
		queries = db
	}
	return &Manager{
		db:       queries,
		handler:  handler,
		outbound: newOutboundTransport(),
		logger:   logger,
	}
}

// Start launches the configured transports. Idempotent: a second Start after
// a successful first one is a no-op. Returns ErrManagerStopped if Stop has
// already run — Manager is a one-way lifecycle.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return ErrManagerStopped
	}
	if m.started {
		return nil
	}
	m.started = true
	return nil
}

// Stop releases all transport resources. Terminal — once Stop returns, the
// Manager cannot be restarted (Start will return ErrManagerStopped). Safe to
// call before Start; safe to call multiple times.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
}

// OnNodeChanged is invoked by the HTTP PATCH handler that updates
// agents.transport_mode (added in a later phase) and on direct DB updates from
// operator tooling. It looks up the current transport_mode for the agent and
// ensures the outbound supervisor map reflects the new state.
func (m *Manager) OnNodeChanged(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started || m.stopped {
		return
	}
	if m.db == nil {
		return // not wired yet (e.g., main.go currently passes nil)
	}
	row, err := m.db.GetAgentTransport(context.Background(), nodeID)
	if err != nil {
		m.logger.Warn("agenttransport: OnNodeChanged lookup failed",
			"node_id", nodeID, "error", err)
		return
	}
	meta := NodeMeta{AgentID: row.ID, NodeID: row.ID}
	switch row.TransportMode {
	case "outbound":
		m.outbound.ensureSupervisor(meta)
	case "inbound":
		m.outbound.removeSupervisor(nodeID)
	default:
		m.logger.Warn("agenttransport: unknown transport_mode",
			"node_id", nodeID, "mode", row.TransportMode)
	}
}

// HasOutboundSupervisor reports whether an outbound supervisor entry exists for
// the given node. Used in tests and health-check handlers.
func (m *Manager) HasOutboundSupervisor(nodeID string) bool {
	return m.outbound.has(nodeID)
}
