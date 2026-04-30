package agenttransport

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/lost-coder/panvex/internal/dbsqlc"
)

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
	db       *dbsqlc.Queries
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

func NewManager(db *dbsqlc.Queries, handler SessionHandler, logger *slog.Logger) *Manager {
	return &Manager{
		db:      db,
		handler: handler,
		logger:  logger,
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
