package agenttransport

import (
	"context"
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
//
// In this task only inbound exists, and its actual gRPC registration still
// happens in cmd/control-plane/main.go via the regular Server. The Manager's
// inbound field is a scaffold for a future migration where the gRPC handler
// dispatches through Manager.
type Manager struct {
	// db is consulted in Task 7+ for outbound supervisor restoration; nil
	// is acceptable while only inbound is active.
	db       *dbsqlc.Queries
	handler  SessionHandler
	inbound  *inboundTransport
	outbound *outboundTransport
	logger   *slog.Logger

	mu      sync.Mutex
	started bool
}

func NewManager(db *dbsqlc.Queries, handler SessionHandler, logger *slog.Logger) *Manager {
	return &Manager{
		db:      db,
		handler: handler,
		logger:  logger,
	}
}

// Start launches the configured transports. In Task 5 this is a no-op
// scaffold — outbound supervisors are restored from DB in Task 7+, and the
// inbound listener still lives in cmd/control-plane/main.go.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	m.started = true
	return nil
}

// Stop releases all transport resources. In Task 5 this is a no-op; later
// tasks cancel outbound supervisor contexts here.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
}
