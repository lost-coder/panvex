package server

import (
	"context"
	"crypto/tls"
	"database/sql"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/bootstrap"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

const (
	// sessionCookieName is the bare cookie name used when the cookie cannot be
	// marked Secure (plain-HTTP dev / loopback). The browser accepts it without
	// the __Host- prefix's strict requirements, at the cost of weaker isolation.
	sessionCookieName = "panvex_session"
	// sessionCookieNameHostPrefix is the production cookie name. The __Host-
	// prefix forces the browser to enforce three constraints:
	//   1. Secure flag is set
	//   2. Path is "/"
	//   3. Domain attribute is empty (origin-bound — no sibling/subdomain leak)
	// We only emit this name when sessionCookieSecure(r) is true; otherwise the
	// browser would refuse it. Reads accept either form so a session issued
	// under one prefix still works while a deployment toggles Secure.
	sessionCookieNameHostPrefix = "__Host-panvex_session"
	apiBasePath                 = "/api"
	maxInMemoryMetricSnapshots  = 512
	maxInMemoryAuditEvents               = 1024
	httpLoginRateLimitPerWindow          = 30
	httpAgentBootstrapRateLimitPerWindow = 30
	grpcConnectRateLimitPerWindow        = 30
	// httpSensitiveRateLimitPerWindow caps how often a single authenticated
	// user (or client IP if no session) may hit privileged write endpoints
	// (TOTP enable/disable/setup, user CRUD, enrollment-token create, client
	// secret rotation). Prevents brute-forcing the 6-digit TOTP enable code
	// and flooding the system with token/rotation churn.
	httpSensitiveRateLimitPerWindow = 10
	defaultRateLimitWindow          = time.Minute
)

// Server wires local-auth, inventory, jobs, and operator APIs into one HTTP surface.
type Server struct {
	gatewayrpc.UnimplementedAgentGatewayServer
	auth                      *auth.Service
	store                     storage.Store
	uiFiles                   fs.FS
	jobs                      *jobs.Service
	presence                  *presence.Tracker
	events                    *eventbus.Hub
	authority                 *certificateAuthority
	now                       func() time.Time
	panelRuntime              PanelRuntime
	requestRestart            func() error
	loginRateLimiter          *fixedWindowRateLimiter
	agentBootstrapRateLimiter *fixedWindowRateLimiter
	grpcConnectRateLimiter    *fixedWindowRateLimiter
	sensitiveRateLimiter      *fixedWindowRateLimiter
	loginLockout              *accountLockoutTracker
	totpLockout               *totpLockoutTracker
	// wsConnLimiter caps the number of live /events WebSocket connections
	// per user-id (and per-IP for unauthenticated callers, defence-in-depth).
	// Goroutine exhaustion otherwise — every accepted socket holds a reader
	// goroutine, a writer goroutine, and an event-bus subscription. See
	// ws_conn_limit.go.
	wsConnLimiter             *wsConnLimiter
	trustedProxyCIDRs         []*net.IPNet
	encryptionKey             string
	secretVault               *secretvault.Vault
	logger                    *slog.Logger
	version                   string
	commitSHA                 string
	buildTime                 string
	intervals                 Intervals

	mu             sync.RWMutex
	clientsMu      sync.RWMutex
	metricsAuditMu sync.RWMutex
	settingsMu     sync.RWMutex
	// sessions multiplexes live gRPC stream sessions keyed by agent ID.
	// Extracted into controlplane/agents.SessionManager by P3-ARCH-01a —
	// this field replaces the previous sessionMu + agentSessions + sessionSeq
	// trio. All agent-stream wake/done/terminate bookkeeping now lives in
	// the new package; the server only holds a pointer.
	sessions *agents.SessionManager
	// clientsSvc is the managed-client service introduced by P3-ARCH-01b.
	// It currently exposes the pure helpers (ResolveTargetAgentIDs,
	// ResolveIDByName, AggregateUsage, ValidateHexSecret) plus the
	// persistence + deployment-builder helpers via package-level
	// functions. Future work will migrate the in-memory maps + mutation
	// flows (createClient, updateClient, rotateClientSecret,
	// deleteClient, adoptDiscoveredClient, reconcileDiscoveredClients)
	// from Server onto this struct.
	clientsSvc *clients.Service
	// fleetSvc owns the create/update/delete lifecycle for fleet
	// groups and the per-group integrations table. HTTP handlers
	// delegate every mutation through it so validation, uniqueness
	// checks, and multi-table reassignment transactions stay in one
	// place. See internal/controlplane/fleet.
	fleetSvc *fleet.Service
	// adoptMu serializes adopt/merge-adopt of discovered clients. It closes
	// the TOCTOU window between reading a discovered record's status,
	// checking it, creating/updating the managed client, and marking the
	// discovered record as adopted (P2-LOG-03 / P2-LOG-04; audit findings
	// L-11, L-12). A single global mutex is acceptable because adopt is
	// operator-initiated and contention is low. Full Store.Transact wiring
	// is deferred to P2-ARCH-01.
	adoptMu sync.Mutex
	// agentSeq removed (P1-SEC-05): agent IDs are now UUIDv7 so a process
	// restart cannot re-issue a previously-used ID. Other entity sequences
	// (session/audit/metric/client) are still monotonic because they do not
	// participate in mTLS identity.
	auditSeq            uint64
	metricSeq           uint64
	clientSeq           uint64
	assignmentSeq       uint64
	discoveredClientSeq uint64
	// revokedAgentIDs tracks deregistered agent IDs whose mTLS certificates
	// may still be cryptographically valid. The set is checked during gRPC
	// Connect to deny access. It is not persisted: on restart the set is
	// empty, which is acceptable because the CA will not have issued new
	// certificates for deleted agents and existing ones expire within 30 days.
	revokedAgentIDs              map[string]struct{}
	agents                       map[string]Agent
	detailBoosts                 map[string]time.Time
	initializationWatchCooldowns map[string]time.Time
	// fallbackEnteredAt mirrors agent_fallback_state in memory. Hydrated on
	// Run(); updated synchronously under mu and persisted asynchronously via
	// the batch writer. Crash-window caveat: see spec.
	fallbackEnteredAt map[string]time.Time
	clients                      map[string]managedClient
	clientAssignments            map[string][]managedClientAssignment
	clientDeployments            map[string]map[string]managedClientDeployment
	clientUsage                  map[string]map[string]clientUsageSnapshot
	// lastUsageSeq tracks the highest client-usage snapshot sequence number
	// applied per agent. Snapshots whose seq is <= the stored value are
	// discarded (duplicate/replay). seq == 1 after a non-zero stored value
	// signals an agent restart: the CP records the new baseline without
	// double-counting the deltas. See P2-LOG-06 / L-07.
	lastUsageSeq map[string]uint64
	instances    map[string]Instance
	metrics      []MetricSnapshot
	// auditTrail is a fixed-size ring buffer of the most recent audit events.
	// Append is O(1) — we overwrite auditBuf[auditHead] and advance the head
	// index, rather than performing an O(N) slice shift on every overflow.
	//
	// Layout: auditBuf is a pre-allocated array of length
	// maxInMemoryAuditEvents. auditSize is the number of valid entries
	// (<= maxInMemoryAuditEvents). When auditSize < len(auditBuf) the ring
	// is still filling and valid entries live at indices [0, auditSize).
	// Once full, auditHead points at the next slot to overwrite (which
	// equals the oldest entry); valid entries in oldest-to-newest order
	// are at indices auditHead, auditHead+1, ... (mod len).
	//
	// Callers must read/write this structure under metricsAuditMu and use
	// snapshotAuditTrailLocked / appendAuditTrailLocked helpers.
	auditBuf       [maxInMemoryAuditEvents]AuditEvent
	auditHead      int
	auditSize      int
	panelSettings  PanelSettings
	updateSettings UpdateSettings
	updateState    UpdateState
	retention      RetentionSettings
	handler        http.Handler
	startupErr     error
	stopRollup     context.CancelFunc
	rollupWg       sync.WaitGroup
	batchWriter    *storeBatchWriter

	// obs holds the Prometheus collectors exposed at /metrics. Nil when the
	// server is constructed without a scrape token — the /metrics route is
	// not registered in that case, but the field is still nil-checked by the
	// middleware so HTTP serving remains cheap.
	obs                 *metricsCollectors
	metricsScrapeToken  string
	metricsPollerCancel context.CancelFunc
	metricsPollerWG     sync.WaitGroup

	// pprofListenerAddr is non-empty when the operator has opted into the
	// separate-listener pprof mode (S-07). When set, the admin-router
	// /debug/pprof registration is skipped — see http_pprof.go.
	pprofListenerAddr string

	// N-1: detached operator-driven background goroutines (panel
	// self-update, manual update-check) register themselves with this
	// WaitGroup so Shutdown can wait for them to finish before exiting
	// the process. Without it, a graceful restart could race a partial
	// binary write or an in-flight GitHub download.
	bgWG sync.WaitGroup

	// Phase-2 §2.1: previous database/sql pool snapshot. Used by the
	// metrics poller to compute Prometheus counter deltas — sql.DBStats
	// counters are absolute since pool creation, but Prometheus wants
	// per-process monotonic increments.
	poolStatsMu   sync.Mutex
	prevPoolStats sql.DBStats

	// Phase-2 §2.5: per-server CSRF HMAC secret. Random 32 bytes
	// generated at startup; rotated implicitly on every restart (which
	// just makes the FE re-fetch /api/auth/csrf-token).
	csrfSecret []byte

	// loginTimingFloor is the wall-clock minimum the login handler pads
	// every response to (R-S-19). Per-Server field instead of a package
	// global so tests can pass 0 via Options without touching shared
	// state. Production callers leave it at defaultLoginTimingFloor.
	loginTimingFloor time.Duration

	// Phase-3 §3.4: HMAC key for log-line username pseudonymisation.
	// Lazy-initialised by Server.usernameHashKey() on first call;
	// derived from EncryptionKey when set, random per-process otherwise.
	usernameHashMu       sync.Mutex
	usernameHashKeyBytes []byte

	// installCommandHandler issues one-shot curl | bash install commands for
	// outbound (reverse-mode) agents. Nil until wired in via
	// SetInstallCommandHandler; the route returns 503 when nil. atomic.Pointer
	// keeps load/store race-free even though the setter is currently only
	// called once at startup.
	installCommandHandler atomic.Pointer[bootstrap.InstallCommandHandler]

	// agentTransportManager owns the lifecycle of outbound (reverse-mode)
	// supervisors. Nil until wired via SetAgentTransportManager; the
	// transport-mode change handler notifies it when an agent's mode
	// changes so outbound supervisors can be spawned or torn down.
	agentTransportManager atomic.Pointer[agenttransport.Manager]
}

// vault exposes the secret vault initialised from EncryptionKey. A nil
// or disabled return value means encryption is off and callers should
// continue to operate on plaintext (legacy mode).
func (s *Server) vault() *secretvault.Vault {
	if s == nil {
		return nil
	}
	return s.secretVault
}

// New constructs a control-plane server with in-memory state suitable for local development.

// Handler returns the configured HTTP handler for the control-plane API.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// StartupError reports the first initialization error encountered while restoring persisted state.
func (s *Server) StartupError() error {
	return s.startupErr
}

// GRPCTLSConfig returns the TLS configuration used by the agent gateway listener.
func (s *Server) GRPCTLSConfig() *tls.Config {
	return s.authority.serverTLSConfig()
}

// SetInstallCommandHandler wires the bootstrap install-command handler. Safe
// to call concurrently with HTTP requests. Nil h is accepted — the route
// returns 503 until a non-nil handler is provided.
func (s *Server) SetInstallCommandHandler(h *bootstrap.InstallCommandHandler) {
	s.installCommandHandler.Store(h)
}

// SetAgentTransportManager wires the agenttransport.Manager so the
// transport-mode change handler can notify it when an agent's mode is
// updated. Safe to call concurrently with HTTP requests. Also wires the
// Prometheus supervisor-gauge callback and the SPKI cert-pin reader (S-02)
// if metrics / storage are available.
func (s *Server) SetAgentTransportManager(m *agenttransport.Manager) {
	s.agentTransportManager.Store(m)
	if m == nil {
		return
	}
	if s.obs != nil {
		m.SetSupervisorGaugeDelta(s.obs.AddOutboundSupervisor)
	}
	// Wire SPKI pin verification (S-02): use the server's storage backend as
	// the CertPinReader and the metrics collector as the observer. Both may
	// be nil (e.g., in tests without full wiring) — SetCertPinReader handles
	// nil reader safely (skips verification for all dials).
	var obs agenttransport.CertPinVerifyObserver
	if s.obs != nil {
		obs = s.obs.ObserveAgentCertPin
	}
	m.SetCertPinReader(s.store, obs)
}

// notifyTransportManager calls Manager.OnNodeChanged if a manager has
// been wired. No-op when the manager is nil (e.g. in unit tests that
// do not wire the full transport stack).
func (s *Server) notifyTransportManager(agentID string) {
	if m := s.agentTransportManager.Load(); m != nil {
		m.OnNodeChanged(agentID)
	}
}

// handleAgentInstallCommand returns an http.HandlerFunc that delegates to the
// install-command handler. Returns 503 if the handler has not been configured.
// Emits a bootstrap.token_issued audit event on success (HTTP 200).
func (s *Server) handleAgentInstallCommand() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		h := s.installCommandHandler.Load()
		if h == nil {
			http.Error(w, "install-command endpoint not configured", http.StatusServiceUnavailable)
			return
		}
		// Wrap the response writer so we can detect a successful response and
		// emit an audit event without touching the bootstrap package.
		rw := &statusCapture{ResponseWriter: w}
		h.ServeHTTP(rw, r)
		if rw.status == 0 || rw.status == http.StatusOK {
			agentID := chi.URLParam(r, "id")
			s.appendAuditWithContext(r.Context(), session.UserID, "bootstrap.token_issued", agentID, nil)
		}
	}
}

// CertificateAuthority returns the panel's CA, which implements
// bootstrap.CertificateAuthority (SignCSR). Used by main.go to wire the
// EnrollDriver for outbound-supervisor bootstrap exchanges.
func (s *Server) CertificateAuthority() bootstrap.CertificateAuthority {
	return s.authority
}

// CACN returns the panel CA's Common Name. Agents verify the panel's TLS
// certificate against this name during enrollment.
func (s *Server) CACN() string {
	if s.authority == nil {
		return ""
	}
	return s.authority.certificate.Subject.CommonName
}

// CAPINHex returns the lower-hex SHA-256 fingerprint of the panel's CA DER
// bytes. Agents that receive this value via the install command pin the panel
// CA against it on first connect.
func (s *Server) CAPINHex() string {
	if s.authority == nil {
		return ""
	}
	return caFingerprint(s.authority.certificate)
}

// WireEnrollDriver attaches the server's Prometheus counter and audit-event
// hooks to an EnrollDriver so its Run outcomes are recorded. Call this
// immediately after constructing the driver and before starting the outbound
// supervisor. Safe to call with a nil driver (no-op).
func (s *Server) WireEnrollDriver(d *bootstrap.EnrollDriver) {
	if d == nil {
		return
	}
	if s.obs != nil {
		d.SetAttemptRecorder(s.obs.ObserveBootstrapAttempt)
	}
	d.SetEventNotifier(func(action, agentID string) {
		s.appendAudit("", action, agentID, nil)
	})
}
