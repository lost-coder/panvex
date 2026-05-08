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
	"github.com/lost-coder/panvex/internal/controlplane/geoip"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/settings"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/webhooks"
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
	sessionCookieNameHostPrefix          = "__Host-panvex_session"
	apiBasePath                          = "/api"
	maxInMemoryMetricSnapshots           = 512
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
	// ipLockout counts failed login attempts per source IP over a 15-minute
	// rolling window and locks the IP for 30 min once the budget is hit
	// (Task 6, S-medium). Runs PARALLEL to loginLockout — usernames and
	// IPs each have their own counter so an attacker who enumerates
	// usernames can no longer lock every account by triggering 5 fails per
	// user. State is in-memory only by design.
	ipLockout                 *ipLockoutTracker
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
	// settings is the operational settings store, loaded at startup from the
	// DB and reloaded on demand. Nil when the server has no persistent store.
	settings         *settings.OperationalStore
	// settingsActive is an immutable snapshot of operational values captured
	// right after the initial Reload. Used to detect pending restart-required
	// changes when comparing against live values.
	settingsActive   *settings.ActiveSnapshot
	// activeSessionIdleTimeout / activeSessionMaxLifetime capture the
	// restart=true session window values at startup. They must not be
	// re-read from the live store on the request path — a change only takes
	// effect after the operator restarts the panel. Set in lifecycle.go
	// right after s.settingsActive is captured.
	activeSessionIdleTimeout time.Duration
	activeSessionMaxLifetime time.Duration
	bootstrap        *settings.Bootstrap
	bootstrapSources settings.SourceMap
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
	// auditChainTail tracks the latest persisted/enqueued audit
	// event_hash so the next append can be chained onto it. Loaded
	// lazily from the store the first time we need it (or initialised
	// to "" on a fresh table, which the verifier treats as the
	// chain-genesis sentinel). Migration 0038 added the underlying
	// column. Read/write under metricsAuditMu.
	auditChainTail   string
	auditChainLoaded bool
	// webhookStorage / webhookProducer power the outbox subsystem.
	// Both are nil when Options.WebhookStorage was unset (test
	// fixtures, or operators with no webhook endpoints configured) —
	// publishWebhookEvent is the nil-safe wrapper every event source
	// uses, so callers don't have to nil-check at every site.
	webhookStorage  webhooks.Storage
	webhookProducer *webhooks.Producer
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
	clients           map[string]managedClient
	clientAssignments map[string][]managedClientAssignment
	clientDeployments map[string]map[string]managedClientDeployment
	clientUsage       map[string]map[string]clientUsageSnapshot
	// agentClientUsage is the inverse of clientUsage's outer/inner key
	// orientation: agentID -> set of clientIDs that this agent has a
	// usage entry for. Maintained in lock-step with clientUsage so
	// zeroLiveGaugesForUntouchedClients can iterate only the clients
	// this agent owns instead of scanning every (clientID, agentID)
	// combination on the entire panel (P-11).
	//
	// Same lock as clientUsage (s.clientsMu).
	agentClientUsage map[string]map[string]struct{}
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
	// geoip owns the live City/ASN MaxMind readers. Constructed in
	// New() (logger only) and reloaded from disk during boot if the
	// configured paths exist; lookups are RWMutex-guarded inside the
	// Manager. Closed during Close() after rollupWg.Wait().
	geoip *geoip.Manager
	// geoipSettings is the operator-managed configuration (mode +
	// per-source enable/URL/local_path). Persisted as opaque JSON via
	// UpdateConfigStore.{Get,Put}GeoIPSettings. Read/written under
	// settingsMu (shared with retention/update settings).
	geoipSettings geoip.Settings
	// geoipState tracks last-checked / last-updated / etag / size /
	// error per source. Persisted independently so the worker can
	// write it without contending with operator edits. Read/written
	// under settingsMu.
	geoipState geoip.State
	// geoipPaths caches the resolved on-disk directory used in
	// auto/URL modes. Computed once at boot from Options.SQLitePath
	// (with PANVEX_GEOIP_DIR override). Read-only after init.
	geoipPaths geoipPaths
	// geoipWorkerCancel cancels the auto/URL refresh worker. Reset
	// when the worker is respawned after a settings change. Held
	// under settingsMu.
	geoipWorkerCancel context.CancelFunc
	// sqlitePath mirrors Options.SQLitePath for geoip directory
	// resolution. Empty when running against Postgres.
	sqlitePath string
	handler    http.Handler
	startupErr error
	// serverCtx is the lifecycle context owned by the Server. It is created
	// in New() and cancelled by Close() so long-lived workers (rollup,
	// metrics poller, fleet-ensure, lockout-restore, batch-writer drain)
	// can abort wedged storage calls during shutdown. Subsequent Plan 3
	// tasks migrate the existing `context.Background()` call sites onto
	// this context. serverCloseOnce guarantees the cancel runs exactly
	// once even under concurrent Close() invocation; bare nil-check +
	// assign would race two competing goroutines.
	serverCtx       context.Context
	serverCancel    context.CancelFunc
	serverCloseOnce sync.Once
	stopRollup   context.CancelFunc
	rollupWg     sync.WaitGroup
	batchWriter  *storeBatchWriter

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

// Context returns the Server's lifecycle context. The context is alive
// between New() and Close(); Close() cancels it so long-lived workers can
// abort in-flight storage calls during shutdown. Callers must NOT cache
// the returned context across a Close — derive child contexts via
// context.WithCancel/WithTimeout from the value returned here at
// goroutine start.
//
// If the Server was constructed via a path that did not initialise the
// lifecycle context (e.g. test helpers using newServerFromOptions
// directly), returns context.Background() so worker code that does
// <-ctx.Done() does not panic on a nil receiver.
func (s *Server) Context() context.Context {
	if s.serverCtx == nil {
		return context.Background()
	}
	return s.serverCtx
}

// New constructs a control-plane server with in-memory state suitable for local development.

// Settings returns the operational settings store.
func (s *Server) Settings() *settings.OperationalStore { return s.settings }

// SetTestBootstrap is for tests only; production wiring goes via the
// constructor in T27.
func (s *Server) SetTestBootstrap(b *settings.Bootstrap, src settings.SourceMap) {
	s.bootstrap = b
	s.bootstrapSources = src
}

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
// Prometheus supervisor-gauge callback, the SPKI cert-pin reader (S-02),
// and the outbound backoff getters (settings Task 4) if metrics / storage
// are available.
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
	// Wire live backoff getters so operator changes to
	// agents.outbound_backoff_initial / agents.outbound_backoff_max are
	// picked up on the next reconnect iteration without a panel restart.
	// When s.settings is nil (no persistent store / tests) the manager
	// falls back to the package constants.
	if s.settings != nil {
		m.SetBackoffGetters(
			s.settings.AgentsOutboundBackoffInitial,
			s.settings.AgentsOutboundBackoffMax,
		)
	}
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
