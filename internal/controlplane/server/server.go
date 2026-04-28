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
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
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
	sessionCookieName          = "panvex_session"
	apiBasePath                = "/api"
	maxInMemoryMetricSnapshots = 512
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


