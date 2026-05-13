package agenttransport

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
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
// can grow without breaking call sites. DialAddress is set for outbound nodes
// and is the host:port the panel will dial.
type NodeMeta struct {
	AgentID      string
	NodeID       string
	NodeName     string
	FleetGroupID string
	DialAddress  string
}

// Transport modes — must match the CHECK constraint on agents.transport_mode
// in db/migrations/{postgres,sqlite}/0030_node_transport_mode.sql.
const (
	TransportModeInbound  = "inbound"
	TransportModeOutbound = "outbound"
)

// Manager owns the lifecycle of agent transports — both inbound (gRPC stream
// initiated by the agent) and outbound (panel dials a listening agent).
// The inbound field is a scaffold; the actual gRPC registration is done by
// cmd/control-plane via the regular Server until a future migration moves
// dispatch through Manager.
//
// Lock order: m.mu → outbound.mu. Never acquire m.mu while holding
// outbound.mu, and never hold m.mu across a DB call or other blocking IO.
type Manager struct {
	// db is consulted by outbound supervisor restoration; nil is tolerated
	// while no outbound transport is active. A non-nil value is required
	// before any outbound flow runs.
	db       transportQueries
	handler  SessionHandler
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
// when db is nil (used in tests and during pre-wiring startup). tlsCfg is the
// TLS configuration used by outbound supervisors when dialing agents; nil is
// acceptable while no outbound supervisors are active. Production passes
// api.GRPCTLSConfig() (the panel's mTLS config) — see cmd/control-plane/serve.go.
// SetCertPinReader (S-02) layers SPKI verification on top of this base config
// per outbound dial.
func NewManager(db *dbsqlc.Queries, handler SessionHandler, tlsCfg *tls.Config, logger *slog.Logger) *Manager {
	var queries transportQueries
	if db != nil {
		queries = db
	}
	return &Manager{
		db:       queries,
		handler:  handler,
		outbound: newOutboundTransport(tlsCfg, handler, logger),
		logger:   logger,
	}
}

// Start launches the configured transports. Idempotent: a second Start after
// a successful first one is a no-op. Returns ErrManagerStopped if Stop has
// already run — Manager is a one-way lifecycle.
func (m *Manager) Start(ctx context.Context) error {
	// Snapshot the guards under m.mu, then release before any blocking IO.
	m.mu.Lock()
	if m.stopped {
		m.mu.Unlock()
		return ErrManagerStopped
	}
	if m.started {
		m.mu.Unlock()
		return nil
	}
	m.started = true
	db := m.db
	m.mu.Unlock()

	// Wire the supervisor lifecycle context BEFORE any ensureSupervisor calls.
	// Context cancellation cascades to every supervisor goroutine when the
	// manager (or its owning server) shuts down. This complements the
	// explicit per-entry cancel issued by stopAll/removeSupervisor as
	// defence-in-depth against missed cancel-paths.
	m.outbound.setLifecycleCtx(ctx)

	// No DB wired yet — Start is a no-op (e.g., main.go currently passes nil
	// during pre-bootstrap startup; outbound supervisors will be reconciled
	// once a real Queries handle is plumbed).
	if db == nil {
		return nil
	}

	rows, err := db.ListAgentsByTransportMode(ctx, TransportModeOutbound)
	if err != nil {
		return fmt.Errorf("agenttransport: list outbound agents: %w", err)
	}
	// Fail-fast if outbound agents need restoration but no TLS config is
	// wired. Otherwise every supervisor would loop with errOutboundTLSMissing
	// and spam logs with no chance of recovery.
	if len(rows) > 0 && m.outbound.tlsCfg == nil {
		return fmt.Errorf("agenttransport: %d outbound agent(s) require a TLS config but none is wired", len(rows))
	}
	for _, row := range rows {
		if !row.DialAddress.Valid {
			m.logger.Warn("agenttransport: outbound agent missing dial_address; skipping",
				"node_id", row.ID)
			continue
		}
		m.outbound.ensureSupervisor(ctx, NodeMeta{
			AgentID:     row.ID,
			NodeID:      row.ID,
			DialAddress: row.DialAddress.String,
		})
	}
	return nil
}

// Stop releases all transport resources. Terminal — once Stop returns, the
// Manager cannot be restarted (Start will return ErrManagerStopped). Safe to
// call before Start; safe to call multiple times.
func (m *Manager) Stop() {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
	m.outbound.stopAll()
}

// OnNodeChanged is invoked by the HTTP PATCH handler that updates
// agents.transport_mode (added in a later phase) and on direct DB updates from
// operator tooling. It looks up the current transport_mode for the agent and
// ensures the outbound supervisor map reflects the new state.
// The ctx parameter governs the DB lookup; callers SHOULD pass the request
// context (HTTP) or the server's lifecycle context (operator tooling) so a
// stalled DB cannot pin the goroutine indefinitely.
func (m *Manager) OnNodeChanged(ctx context.Context, nodeID string) {
	// Snapshot the guards under m.mu, then release before any blocking IO.
	// outbound has its own mutex; the m.outbound pointer itself is set once
	// in NewManager and never reassigned, so it is safe to use without
	// holding m.mu.
	m.mu.Lock()
	if !m.started || m.stopped || m.db == nil {
		m.mu.Unlock()
		return
	}
	db := m.db
	m.mu.Unlock()

	row, err := db.GetAgentTransport(ctx, nodeID)
	if err != nil {
		m.logger.Warn("agenttransport: OnNodeChanged lookup failed",
			"node_id", nodeID, "error", err)
		return
	}
	meta := NodeMeta{
		AgentID:     row.ID,
		NodeID:      row.ID,
		DialAddress: row.DialAddress.String,
	}
	switch row.TransportMode {
	case TransportModeOutbound:
		m.outbound.ensureSupervisor(ctx, meta)
	case TransportModeInbound:
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

// SetSupervisorGaugeDelta wires a callback that is invoked with +1/-1 whenever
// an outbound supervisor is added or removed. This is the seam used by the
// server's Prometheus collector (metricsCollectors.AddOutboundSupervisor) to
// maintain panvex_outbound_supervisors_total. Safe to call before Start.
func (m *Manager) SetSupervisorGaugeDelta(fn SupervisorGaugeDelta) {
	m.outbound.mu.Lock()
	m.outbound.onSupervisorDelta = fn
	m.outbound.mu.Unlock()
}

// SetEnrollCallbacks wires the enrollment pre-flight into every outbound
// supervisor that is started after this call. enrollFn is invoked when
// bootstrapStateFn reports "pending" for a given agent; on success the DB
// transitions to "active" and the supervisor proceeds with the normal mTLS
// dial. Both arguments must be non-nil; passing nil for either is a no-op.
// Safe to call before Start.
func (m *Manager) SetEnrollCallbacks(enrollFn EnrollFunc, bootstrapStateFn BootstrapStateFunc) {
	if enrollFn == nil || bootstrapStateFn == nil {
		return
	}
	m.outbound.mu.Lock()
	m.outbound.enrollFn = enrollFn
	m.outbound.bootstrapStateFn = bootstrapStateFn
	m.outbound.mu.Unlock()
}

// SetCertPinReader wires the storage backend used to read the SPKI pin for
// each agent during the TLS handshake (S-02 dial-time verification). Must be
// called once at startup, before Start, from a single goroutine. The optional
// observer callback is invoked with "ok", "mismatch", or "missing" after each
// verification attempt — used by the server's Prometheus collector to maintain
// panvex_agent_cert_pin_total. If r is nil, pin verification is skipped for
// all outbound dials (backward-compatible: agents enrolled pre-S-02 also
// produce an empty pin which skips verification).
func (m *Manager) SetCertPinReader(r CertPinReader, obs CertPinVerifyObserver) {
	m.outbound.mu.Lock()
	m.outbound.pinReader = r
	m.outbound.pinObserver = obs
	m.outbound.mu.Unlock()
}

// SetEnrollmentRecorder wires the enrollment timeline recorder into every
// outbound supervisor created after this call. Each connectAndServe cycle
// opens a fresh attempt (mode=outbound), records the dial timeline, and
// completes/fails the attempt before returning. A nil recorder disables
// recording — existing supervisors keep using whatever value they were
// initialised with. Safe to call before Start.
func (m *Manager) SetEnrollmentRecorder(rec *enrollment.Recorder) {
	m.outbound.mu.Lock()
	m.outbound.rec = rec
	m.outbound.mu.Unlock()
}

// SetBackoffGetters wires live getters for the outbound supervisor reconnect
// backoff windows. Each getter is called on every reconnect iteration, so an
// operator change to agents.outbound_backoff_initial /
// agents.outbound_backoff_max is picked up without restarting the panel.
// Must be called before Start; safe to call with nil (falls back to package
// constants). The getters should return values from an OperationalStore.
func (m *Manager) SetBackoffGetters(initialFn, maxFn func() time.Duration) {
	m.outbound.mu.Lock()
	m.outbound.backoffInitialFn = initialFn
	m.outbound.backoffMaxFn = maxFn
	m.outbound.mu.Unlock()
}
