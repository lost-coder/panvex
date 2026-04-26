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

// Options defines the runtime dependencies used by the control-plane server.
type Options struct {
	Now            func() time.Time
	Users          []auth.User
	Store          storage.Store
	UIFiles        fs.FS
	PanelRuntime   PanelRuntime
	RequestRestart func() error
	// TrustedProxyCIDRs lists additional CIDR ranges whose X-Forwarded-For
	// header is trusted for rate-limit key extraction. Loopback addresses
	// are always trusted regardless of this setting.
	//
	// WARNING: When the control-plane runs behind a non-loopback reverse
	// proxy and this list is empty, every inbound request resolves to the
	// proxy's IP for rate-limit keying. All clients then share a single
	// bucket and will be throttled collectively. Always configure this
	// field to include every intermediate proxy/load-balancer CIDR.
	TrustedProxyCIDRs []*net.IPNet
	// EncryptionKey, when set, encrypts the CA private key at rest using
	// AES-256-GCM. The key is derived from this passphrase via SHA-256.
	// Existing unencrypted keys are transparently migrated on next save.
	EncryptionKey string
	// Logger is the structured logger for the server. If nil, slog.Default() is used.
	Logger *slog.Logger
	// Version is the panel version string (e.g. "v1.2.3" or "dev").
	Version string
	// CommitSHA is the git commit hash baked in at build time.
	CommitSHA string
	// BuildTime is the RFC3339 build timestamp baked in at build time.
	BuildTime string
	// MetricsScrapeToken, when non-empty, enables the GET /metrics endpoint
	// and requires callers to present `Authorization: Bearer <token>` with a
	// byte-for-byte match. When empty, the /metrics route is not registered
	// at all (silent opt-in) so production deployments that never set the env
	// var cannot accidentally expose runtime telemetry.
	MetricsScrapeToken string
	// Intervals overrides the default worker / poller cadences. Zero-valued
	// fields fall back to DefaultIntervals(). Tests use this to compress
	// retention sweeps and rollup scans into milliseconds.
	Intervals Intervals
}

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
func New(options Options) *Server {
	now := options.Now
	if now == nil {
		now = time.Now
	}

	csrfSecret, err := loadOrCreateCSRFSecret(options.Store)
	if err != nil {
		// crypto/rand.Read returning an error means the OS entropy
		// pool is unavailable — there is nothing meaningful the panel
		// can do without it (sessions, certs all need it too). Fail
		// loudly so an operator notices instead of falling back to
		// CSRF-disabled mode.
		panic("control-plane: cannot initialise CSRF secret: " + err.Error())
	}

	// Build the secret vault once from the operator passphrase. A nil
	// or empty passphrase yields a disabled vault so existing dev
	// fixtures keep using plaintext at-rest.
	vault, vaultErr := secretvault.New(options.EncryptionKey, secretvault.AllDomains)
	if vaultErr != nil {
		panic("control-plane: cannot initialise secret vault: " + vaultErr.Error())
	}

	server := &Server{
		auth:                         auth.NewService(),
		store:                        options.Store,
		uiFiles:                      options.UIFiles,
		jobs:                         jobs.NewService(),
		presence:                     presence.NewTracker(30*time.Second, 90*time.Second),
		events:                       eventbus.NewHub(),
		now:                          now,
		panelRuntime:                 defaultPanelRuntime(options.PanelRuntime),
		requestRestart:               options.RequestRestart,
		loginRateLimiter:             newFixedWindowRateLimiter(httpLoginRateLimitPerWindow, defaultRateLimitWindow),
		agentBootstrapRateLimiter:    newFixedWindowRateLimiter(httpAgentBootstrapRateLimitPerWindow, defaultRateLimitWindow),
		grpcConnectRateLimiter:       newFixedWindowRateLimiter(grpcConnectRateLimitPerWindow, defaultRateLimitWindow),
		sensitiveRateLimiter:         newFixedWindowRateLimiter(httpSensitiveRateLimitPerWindow, defaultRateLimitWindow),
		loginLockout:                 newAccountLockoutTracker(),
		trustedProxyCIDRs:            options.TrustedProxyCIDRs,
		encryptionKey:                options.EncryptionKey,
		secretVault:                  vault,
		logger:                       options.Logger,
		version:                      options.Version,
		commitSHA:                    options.CommitSHA,
		buildTime:                    options.BuildTime,
		csrfSecret:                   csrfSecret,
		revokedAgentIDs:              make(map[string]struct{}),
		agents:                       make(map[string]Agent),
		detailBoosts:                 make(map[string]time.Time),
		initializationWatchCooldowns: make(map[string]time.Time),
		clients:                      make(map[string]managedClient),
		clientAssignments:            make(map[string][]managedClientAssignment),
		clientDeployments:            make(map[string]map[string]managedClientDeployment),
		clientUsage:                  make(map[string]map[string]clientUsageSnapshot),
		lastUsageSeq:                 make(map[string]uint64),
		sessions:                     agents.NewSessionManager(),
		clientsSvc:                   clients.NewServiceWithVault(options.Store, now, vault),
		fleetSvc:                     fleet.NewService(options.Store, func() time.Time { return now().UTC() }),
		instances:                    make(map[string]Instance),
		metrics:                      make([]MetricSnapshot, 0, maxInMemoryMetricSnapshots),
		intervals:                    options.Intervals.withDefaults(),
	}
	if server.logger == nil {
		server.logger = slog.Default()
	}
	// R-S-09: route lockout-tracker warnings through the same HMAC
	// redaction that http_auth.go uses so log aggregators never see raw
	// usernames even when the persistent store fails.
	server.loginLockout.SetRedactor(server.logUsername)
	server.panelSettings = defaultPanelSettings()
	server.updateSettings = defaultUpdateSettings()
	server.retention = defaultRetentionSettings()
	authority, err := loadOrCreateCertificateAuthority(options.Store, now(), options.EncryptionKey)
	if err != nil {
		server.startupErr = err
	} else {
		server.authority = authority
	}
	if options.Store != nil {
		server.jobs = jobs.NewServiceWithStore(options.Store)
		server.auth = auth.NewServiceWithStore(options.Store)
		server.auth.SetSessionStore(options.Store)
		server.auth.SetVault(vault)
		server.auth.SetConsumedTotpStore(options.Store)
		if err := server.auth.RestoreSessions(); err != nil && server.startupErr == nil {
			server.startupErr = err
		}
		// S7: wire the lockout tracker to the persistent backend and
		// load any state that survived a restart. Restore errors go to
		// startupErr so the operator sees them at boot, but we still
		// attach the store so subsequent writes are persisted even if
		// the initial restore was empty.
		server.loginLockout.SetStore(newLockoutStoreAdapter(options.Store))
		if err := server.loginLockout.Restore(context.Background(), server.now()); err != nil && server.startupErr == nil {
			server.startupErr = err
		}
		if err := server.jobs.StartupError(); err != nil && server.startupErr == nil {
			server.startupErr = err
		}
		if server.startupErr == nil {
			if err := server.seedUsers(options.Users); err != nil {
				server.startupErr = err
			}
		}
		if server.startupErr == nil {
			if err := server.restoreStoredState(); err != nil {
				server.startupErr = err
			}
		}
		if server.startupErr == nil {
			if err := server.restoreStoredClients(); err != nil {
				server.startupErr = err
			}
		}
		if server.startupErr == nil {
			if err := server.restoreStoredDiscoveredClients(); err != nil {
				server.startupErr = err
			}
		}
		if server.startupErr == nil {
			if err := server.restoreStoredPanelSettings(); err != nil {
				server.startupErr = err
			}
		}
		if server.startupErr == nil {
			if err := server.restoreUpdateSettings(); err != nil {
				server.startupErr = err
			}
		}
		if server.startupErr == nil {
			if err := server.restoreRetentionSettings(); err != nil {
				server.startupErr = err
			}
		}
		// Fresh databases need at least one fleet group so enrollment
		// tokens can reference it. Operators can rename the label
		// afterwards via the HTTP API; the `default` slug is kept so
		// docs and scripts can rely on a predictable name.
		if server.startupErr == nil {
			if _, err := server.fleetSvc.EnsureDefault(context.Background()); err != nil {
				server.startupErr = err
			}
		}
	} else if len(options.Users) > 0 {
		server.auth.LoadUsers(options.Users)
	}
	// Metrics collectors are always constructed (observation is cheap) but
	// the /metrics HTTP route is only registered when a scrape token is set.
	// This keeps the in-process counters available for internal consumption
	// (e.g. tests, future admin-only endpoints) without exposing them.
	server.obs = newMetricsCollectors()
	server.metricsScrapeToken = options.MetricsScrapeToken
	server.events.SetDropHook(func() {
		server.obs.eventHubDropTotal.Inc()
	})
	server.handler = server.routes()
	server.auth.SetNow(now)
	server.jobs.SetNow(now)
	rollupCtx, rollupCancel := context.WithCancel(context.Background())
	server.stopRollup = rollupCancel
	server.startTimeseriesRollupWorker(rollupCtx)
	server.startUpdateCheckerWorker(rollupCtx)

	// Evict idempotency keys for terminal jobs on an hourly tick to keep
	// jobs.Service.keys bounded. See P2-PERF-03. TTL of 24h matches the
	// operational expectation that clients will not retry the same
	// idempotency key after a full day.
	server.rollupWg.Add(1)
	server.jobs.StartKeyEvictionWorker(rollupCtx, server.intervals.JobsKeyEviction, server.intervals.JobsKeyEvictionTTL, &server.rollupWg)

	// P2-LOG-05 (L-14): expire acknowledged-but-never-resulted targets
	// after 2h so jobs do not stay "acknowledged" forever when the agent
	// restarts between ack and result. The 2h window matches the agent
	// idempotency cache so the CP gives up in sync with the agent's ability
	// to safely deduplicate.
	server.rollupWg.Add(1)
	server.jobs.StartAcknowledgedExpiryWorker(rollupCtx, server.intervals.JobsAckExpiry, server.intervals.JobsAckExpiryTTL, &server.rollupWg)

	// The metrics poller samples derived gauges (agent connected count,
	// event-hub subscribers, job queue depth, lockout count) on a 5-second
	// interval. Runs in its own context so Close() can stop it independently
	// of the rollup workers.
	//
	// Gate on MetricsScrapeToken: if no token is configured, the /metrics
	// endpoint is not registered, so nobody can observe these gauges. Starting
	// the poller anyway costs little but forces every test using a closure-
	// captured time source to synchronise the closure with a background
	// goroutine that samples s.now(). Skipping the poller when metrics are
	// disabled keeps the race-free clock-mock pattern working for tests.
	if server.metricsScrapeToken != "" {
		metricsCtx, metricsCancel := context.WithCancel(context.Background())
		server.metricsPollerCancel = metricsCancel
		server.startMetricsPoller(metricsCtx, server.intervals.MetricsPoller)
	}

	if server.store != nil {
		// Pass the Prometheus bundle as the metrics sink so batch writer
		// errors surface to operators (P2-REL-06 / H14). obs is always set
		// earlier in New(); nil would fall back to the no-op sink.
		server.batchWriter = newStoreBatchWriter(server.store, server.obs, server.now)
		server.batchWriter.Start()
	}

	return server
}

// Close stops background workers and drains pending writes. It should be
// called before closing the storage backend.
//
// Shutdown ordering (P2-LOG-10 / M-R4 / P7-R6):
//  1. batchWriter.StopWithTimeout(10s) FIRST — drains the audit-events
//     queue (and the 7 other streams) before any background goroutine that
//     might still be producing audits is shut down. This bounds the
//     worst-case shutdown time at 10s so a wedged DB cannot hang the
//     process indefinitely, while still giving the DB a realistic window
//     to absorb in-flight rows.
//  2. metrics / rollup goroutines stop afterwards.
//
// Events enqueued AFTER this point may race with the final drain and can
// be dropped — upstream callers (HTTP handlers, gRPC streams) must stop
// before Close() runs to guarantee zero loss.
func (s *Server) Close() {
	if s.batchWriter != nil {
		if err := s.batchWriter.StopWithTimeout(10 * time.Second); err != nil {
			s.logger.Error("batch writer drain timed out on shutdown",
				"error", err,
				"alert", "audit_persist_failed",
			)
		}
	}
	s.metricsShutdown()
	if s.stopRollup != nil {
		s.stopRollup()
	}
	// Wait for the rollup goroutine to finish before closing the store,
	// so it does not query a closed storage backend.
	s.rollupWg.Wait()
}

func (s *Server) seedUsers(users []auth.User) error {
	if s.store == nil || len(users) == 0 {
		return nil
	}

	records, err := s.store.ListUsers(context.Background())
	if err != nil {
		return err
	}
	if len(records) > 0 {
		return nil
	}

	for _, user := range users {
		if err := s.store.PutUser(context.Background(), storage.UserRecord{
			ID:           user.ID,
			Username:     user.Username,
			PasswordHash: user.PasswordHash,
			Role:         string(user.Role),
			TotpEnabled:  user.TotpEnabled,
			TotpSecret:   user.TotpSecret,
			CreatedAt:    user.CreatedAt.UTC(),
		}); err != nil {
			return err
		}
	}

	return nil
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


