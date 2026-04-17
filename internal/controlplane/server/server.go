package server

import (
	"crypto/tls"
	"context"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/lost-coder/panvex/internal/security"
)

const (
	sessionCookieName         = "panvex_session"
	apiBasePath               = "/api"
	maxInMemoryMetricSnapshots = 512
	maxInMemoryAuditEvents     = 1024
	// jobsKeyEvictionInterval is how often the jobs service scans for
	// terminal-state idempotency keys to evict. Hourly is a good balance
	// between bounding memory growth under sustained job load and not
	// thrashing the jobs mutex with frequent scans. See P2-PERF-03.
	jobsKeyEvictionInterval = time.Hour
	// jobsKeyEvictionTTL is the age at which a terminal-state idempotency
	// key is evicted. 24h is long enough to dedupe retries from any
	// realistic operator workflow (including overnight runs) while
	// preventing unbounded growth over the lifetime of the process.
	jobsKeyEvictionTTL = 24 * time.Hour
	// jobsAckExpiryInterval is how often the jobs service scans for
	// acknowledged-but-never-resulted targets. Matches the key-eviction
	// cadence so both workers share the same operational signal.
	jobsAckExpiryInterval = time.Hour
	// jobsAckExpiryTTL is the threshold after which an acknowledged target
	// with no result is transitioned to expired. 2h matches the agent-side
	// idempotency cache (defaultCompletedJobRetention) so replaying a lost
	// acknowledged job after this window is never safe — the agent may have
	// already forgotten it. See P2-LOG-05.
	jobsAckExpiryTTL = 2 * time.Hour
	httpLoginRateLimitPerWindow = 30
	httpAgentBootstrapRateLimitPerWindow = 30
	grpcConnectRateLimitPerWindow = 30
	// httpSensitiveRateLimitPerWindow caps how often a single authenticated
	// user (or client IP if no session) may hit privileged write endpoints
	// (TOTP enable/disable/setup, user CRUD, enrollment-token create, client
	// secret rotation). Prevents brute-forcing the 6-digit TOTP enable code
	// and flooding the system with token/rotation churn.
	httpSensitiveRateLimitPerWindow = 10
	defaultRateLimitWindow = time.Minute
)

// Options defines the runtime dependencies used by the control-plane server.
type Options struct {
	Now          func() time.Time
	Users        []auth.User
	Store        storage.Store
	UIFiles      fs.FS
	PanelRuntime PanelRuntime
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
	Version   string
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
}

// Server wires local-auth, inventory, jobs, and operator APIs into one HTTP surface.
type Server struct {
	gatewayrpc.UnimplementedAgentGatewayServer
	auth       *auth.Service
	enrollment *security.EnrollmentService
	store      storage.Store
	uiFiles    fs.FS
	jobs       *jobs.Service
	presence   *presence.Tracker
	events     *eventHub
	authority  *certificateAuthority
	now        func() time.Time
	panelRuntime PanelRuntime
	requestRestart func() error
	loginRateLimiter *fixedWindowRateLimiter
	agentBootstrapRateLimiter *fixedWindowRateLimiter
	grpcConnectRateLimiter *fixedWindowRateLimiter
	sensitiveRateLimiter *fixedWindowRateLimiter
	loginLockout *accountLockoutTracker
	trustedProxyCIDRs []*net.IPNet
	encryptionKey string
	logger *slog.Logger
	version   string
	commitSHA string
	buildTime string

	mu             sync.RWMutex
	sessionMu      sync.RWMutex
	clientsMu      sync.RWMutex
	metricsAuditMu sync.RWMutex
	settingsMu     sync.RWMutex
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
	sessionSeq uint64
	auditSeq   uint64
	metricSeq  uint64
	clientSeq  uint64
	assignmentSeq uint64
	discoveredClientSeq uint64
	// revokedAgentIDs tracks deregistered agent IDs whose mTLS certificates
	// may still be cryptographically valid. The set is checked during gRPC
	// Connect to deny access. It is not persisted: on restart the set is
	// empty, which is acceptable because the CA will not have issued new
	// certificates for deleted agents and existing ones expire within 30 days.
	revokedAgentIDs map[string]struct{}
	agents     map[string]Agent
	detailBoosts map[string]time.Time
	initializationWatchCooldowns map[string]time.Time
	agentSessions map[string]*agentStreamSession
	clients    map[string]managedClient
	clientAssignments map[string][]managedClientAssignment
	clientDeployments map[string]map[string]managedClientDeployment
	clientUsage map[string]map[string]clientUsageSnapshot
	instances  map[string]Instance
	metrics    []MetricSnapshot
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
	auditBuf  [maxInMemoryAuditEvents]AuditEvent
	auditHead int
	auditSize int
	panelSettings  PanelSettings
	updateSettings UpdateSettings
	updateState    UpdateState
	retention      RetentionSettings
	handler      http.Handler
	startupErr   error
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
}

// New constructs a control-plane server with in-memory state suitable for local development.
func New(options Options) *Server {
	now := options.Now
	if now == nil {
		now = time.Now
	}

	server := &Server{
		auth:       auth.NewService(),
		enrollment: security.NewEnrollmentService(),
		store:      options.Store,
		uiFiles:    options.UIFiles,
		jobs:       jobs.NewService(),
		presence:   presence.NewTracker(30*time.Second, 90*time.Second),
		events:     newEventHub(),
		now:        now,
		panelRuntime: defaultPanelRuntime(options.PanelRuntime),
		requestRestart: options.RequestRestart,
		loginRateLimiter: newFixedWindowRateLimiter(httpLoginRateLimitPerWindow, defaultRateLimitWindow),
		agentBootstrapRateLimiter: newFixedWindowRateLimiter(httpAgentBootstrapRateLimitPerWindow, defaultRateLimitWindow),
		grpcConnectRateLimiter: newFixedWindowRateLimiter(grpcConnectRateLimitPerWindow, defaultRateLimitWindow),
		sensitiveRateLimiter: newFixedWindowRateLimiter(httpSensitiveRateLimitPerWindow, defaultRateLimitWindow),
		loginLockout: newAccountLockoutTracker(),
		trustedProxyCIDRs: options.TrustedProxyCIDRs,
		encryptionKey: options.EncryptionKey,
		logger: options.Logger,
		version:   options.Version,
		commitSHA: options.CommitSHA,
		buildTime: options.BuildTime,
		revokedAgentIDs: make(map[string]struct{}),
		agents:     make(map[string]Agent),
		detailBoosts: make(map[string]time.Time),
		initializationWatchCooldowns: make(map[string]time.Time),
		clients:    make(map[string]managedClient),
		clientAssignments: make(map[string][]managedClientAssignment),
		clientDeployments: make(map[string]map[string]managedClientDeployment),
		clientUsage: make(map[string]map[string]clientUsageSnapshot),
		agentSessions: make(map[string]*agentStreamSession),
		instances:  make(map[string]Instance),
		metrics:    make([]MetricSnapshot, 0, maxInMemoryMetricSnapshots),
	}
	if server.logger == nil {
		server.logger = slog.Default()
	}
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
		if err := server.auth.RestoreSessions(); err != nil && server.startupErr == nil {
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
	} else if len(options.Users) > 0 {
		server.auth.LoadUsers(options.Users)
	}
	// Metrics collectors are always constructed (observation is cheap) but
	// the /metrics HTTP route is only registered when a scrape token is set.
	// This keeps the in-process counters available for internal consumption
	// (e.g. tests, future admin-only endpoints) without exposing them.
	server.obs = newMetricsCollectors()
	server.metricsScrapeToken = options.MetricsScrapeToken
	server.events.setDropHook(func() {
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
	server.jobs.StartKeyEvictionWorker(rollupCtx, jobsKeyEvictionInterval, jobsKeyEvictionTTL, &server.rollupWg)

	// P2-LOG-05 (L-14): expire acknowledged-but-never-resulted targets
	// after 2h so jobs do not stay "acknowledged" forever when the agent
	// restarts between ack and result. The 2h window matches the agent
	// idempotency cache so the CP gives up in sync with the agent's ability
	// to safely deduplicate.
	server.rollupWg.Add(1)
	server.jobs.StartAcknowledgedExpiryWorker(rollupCtx, jobsAckExpiryInterval, jobsAckExpiryTTL, &server.rollupWg)

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
		server.startMetricsPoller(metricsCtx, 5*time.Second)
	}

	if server.store != nil {
		// Pass the Prometheus bundle as the metrics sink so batch writer
		// errors surface to operators (P2-REL-06 / H14). obs is always set
		// earlier in New(); nil would fall back to the no-op sink.
		server.batchWriter = newStoreBatchWriter(server.store, server.obs)
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

func (s *Server) restoreStoredState() error {
	agents, err := s.store.ListAgents(context.Background())
	if err != nil {
		return err
	}
	for _, record := range agents {
		agent := agentFromRecord(record)
		s.agents[agent.ID] = agent
	}

	// Restore persisted agent revocations (P1-SEC-06). Without this, a CP
	// restart silently forgets the revocation and a deleted agent whose
	// 30-day client cert is still valid could reconnect over mTLS.
	revocations, err := s.store.ListAgentRevocations(context.Background())
	if err != nil {
		return err
	}
	now := s.now()
	for _, rec := range revocations {
		if rec.CertExpiresAt.Before(now) {
			// Cert is already past expiry — the TLS handshake will reject
			// it on its own, no need to carry the revocation entry.
			continue
		}
		s.revokedAgentIDs[rec.AgentID] = struct{}{}
	}

	instances, err := s.store.ListInstances(context.Background())
	if err != nil {
		return err
	}
	for _, record := range instances {
		instance := instanceFromRecord(record)
		s.instances[instance.ID] = instance
	}

	metrics, err := s.store.ListMetricSnapshots(context.Background())
	if err != nil {
		return err
	}
	for _, record := range metrics {
		s.metricSeq = maxPrefixedSequence(s.metricSeq, "metric", record.ID)
	}
	// Keep only the most recent entries to avoid O(n²) copy-shift.
	if len(metrics) > maxInMemoryMetricSnapshots {
		metrics = metrics[len(metrics)-maxInMemoryMetricSnapshots:]
	}
	for _, record := range metrics {
		s.metrics = append(s.metrics, metricSnapshotFromRecord(record))
	}

	auditEvents, err := s.store.ListAuditEvents(context.Background(), maxInMemoryAuditEvents)
	if err != nil {
		return err
	}
	for _, record := range auditEvents {
		s.auditSeq = maxPrefixedSequence(s.auditSeq, "audit", record.ID)
	}
	// Keep only the most recent entries to avoid O(n²) copy-shift.
	if len(auditEvents) > maxInMemoryAuditEvents {
		auditEvents = auditEvents[len(auditEvents)-maxInMemoryAuditEvents:]
	}
	for _, record := range auditEvents {
		s.appendAuditTrailLocked(auditEventFromRecord(record))
	}

	return s.restoreStoredTelemetry()
}

func maxPrefixedSequence(current uint64, prefix string, value string) uint64 {
	if !strings.HasPrefix(value, prefix+"-") {
		return current
	}

	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix+"-"), 10, 64)
	if err != nil {
		return current
	}
	if parsed > current {
		return parsed
	}

	return current
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

func (s *Server) routes() http.Handler {
	router := chi.NewRouter()
	// metricsMiddleware must be the outermost user middleware so every
	// response — including 401s from ipWhitelist, 429s from rate-limiters,
	// and 404s from the UI fallback — is observed with its route pattern.
	router.Use(s.metricsMiddleware)
	router.Use(securityHeaders)
	router.Use(maxBodySize)
	router.Use(csrfOriginCheck(s.panelRuntime.HTTPRootPath, s.panelRuntime.AgentHTTPRootPath))
	router.Get("/healthz", handleHealthz())
	router.Get("/readyz", s.handleReadyz())
	// /metrics is registered at the top level (outside the /api group) so
	// Prometheus does not need session cookies. It is bearer-token gated in
	// handleMetrics; when no token is configured, the route is omitted.
	if s.metricsScrapeToken != "" {
		router.Method(http.MethodGet, "/metrics", s.handleScrapeMetrics(s.metricsScrapeToken))
	}

	panelPath := s.panelRuntime.HTTPRootPath
	agentPath := s.panelRuntime.AgentHTTPRootPath

	// Agent routes registered at apiBasePath (no whitelist).
	// When agentPath differs from panelPath, also register under the
	// separate agent prefix so they are reachable without stripRootPath.
	router.Route(apiBasePath, func(api chi.Router) {
		api.With(s.withRateLimit(s.agentBootstrapRateLimiter, s.requestClientRateLimitKey)).
			Post("/agent/bootstrap", s.handleAgentBootstrap())
		api.With(s.withRateLimit(s.agentBootstrapRateLimiter, s.requestClientRateLimitKey)).
			Post("/agent/recover-certificate", s.handleAgentCertificateRecovery())

		// Panel routes — with optional IP whitelist
		api.Group(func(panel chi.Router) {
			if len(s.panelRuntime.PanelAllowedCIDRs) > 0 {
				panel.Use(ipWhitelistMiddleware(s.panelRuntime.PanelAllowedCIDRs, s.trustedProxyCIDRs))
			}
			panel.With(s.withRateLimit(s.loginRateLimiter, s.requestClientRateLimitKey)).
				Post("/auth/login", s.handleLogin())

			panel.Group(func(authenticated chi.Router) {
				authenticated.Use(s.requireAuthenticatedSession())
				authenticated.Get("/version", s.handleVersion())
				authenticated.Get("/auth/me", s.handleMe())
				authenticated.Post("/auth/logout", s.handleLogout())
				// Sensitive per-user rate limiting applied to any endpoint that
				// could be brute-forced (TOTP enable 6-digit code) or abused
				// at scale (enrollment token floods, repeated secret
				// rotations). Key is session.UserID, falling back to client IP.
				sensitive := s.withRateLimit(s.sensitiveRateLimiter, s.requestSessionRateLimitKey)
				authenticated.With(sensitive).Post("/auth/totp/setup", s.handleTotpSetup())
				authenticated.With(sensitive).Post("/auth/totp/enable", s.handleTotpEnable())
				authenticated.With(sensitive).Post("/auth/totp/disable", s.handleTotpDisable())
				authenticated.Get("/control-room", s.handleControlRoom())
				authenticated.Get("/fleet", s.handleFleet())
				authenticated.Get("/agents", s.handleAgents())
				authenticated.Get("/instances", s.handleInstances())
				authenticated.Get("/jobs", s.handleJobs())
				authenticated.Get("/audit", s.handleAudit())
				authenticated.Get("/metrics", s.handleMetrics())
				authenticated.Get("/events", s.handleEvents())
				authenticated.Get("/settings/appearance", s.handleGetUserAppearance())
				authenticated.Put("/settings/appearance", s.handlePutUserAppearance())
				authenticated.Get("/telemetry/dashboard", s.handleTelemetryDashboard())
				authenticated.Get("/telemetry/servers", s.handleTelemetryServers())
				authenticated.Get("/telemetry/servers/{id}", s.handleTelemetryServerDetail())
				authenticated.Post("/telemetry/servers/{id}/detail-boost", s.handleTelemetryServerDetailBoost())
				authenticated.Get("/telemetry/servers/{id}/history/load", s.handleServerLoadHistory())
				authenticated.Get("/telemetry/servers/{id}/history/dc", s.handleDCHealthHistory())
				authenticated.Get("/clients/{id}/history/ips", s.handleClientIPHistory())

				authenticated.Group(func(operator chi.Router) {
					operator.Use(s.requireMinimumRole(auth.RoleOperator))
					operator.Post("/jobs", s.handleCreateJob())
					operator.Get("/clients", s.handleClients())
					operator.Post("/clients", s.handleCreateClient())
					operator.Get("/clients/{id}", s.handleClient())
					operator.Put("/clients/{id}", s.handleUpdateClient())
					operator.Delete("/clients/{id}", s.handleDeleteClient())
					operator.With(sensitive).Post("/clients/{id}/rotate-secret", s.handleRotateClientSecret())
					operator.Get("/discovered-clients", s.handleDiscoveredClients())
					operator.Post("/discovered-clients/{id}/adopt", s.handleAdoptDiscoveredClient())
					operator.Post("/discovered-clients/{id}/ignore", s.handleIgnoreDiscoveredClient())
					operator.Post("/telemetry/servers/{id}/refresh-diagnostics", s.handleTelemetryServerRefreshDiagnostics())
					operator.Get("/fleet-groups", s.handleFleetGroups())
					operator.Patch("/agents/{id}", s.handleRenameAgent())
					operator.Get("/agents/enrollment-tokens", s.handleListEnrollmentTokens())
					operator.With(sensitive).Post("/agents/enrollment-tokens", s.handleCreateEnrollmentToken())
					operator.Post("/agents/enrollment-tokens/{value}/revoke", s.handleRevokeEnrollmentToken())
					operator.Post("/agents/{id}/update", s.handleAgentUpdate())
					operator.Get("/agent/update/binary", s.handleAgentBinaryProxy())
				})

				authenticated.Group(func(admin chi.Router) {
					admin.Use(s.requireMinimumRole(auth.RoleAdmin))
					admin.Get("/users", s.handleUsers())
					admin.With(sensitive).Post("/users", s.handleCreateUser())
					admin.With(sensitive).Put("/users/{id}", s.handleUpdateUser())
					admin.With(sensitive).Delete("/users/{id}", s.handleDeleteUser())
					admin.With(sensitive).Post("/users/{id}/totp/reset", s.handleResetUserTotp())
					admin.Post("/agents/{id}/certificate-recovery-grants", s.handleCreateAgentCertificateRecoveryGrant())
					admin.Post("/agents/{id}/certificate-recovery-grants/revoke", s.handleRevokeAgentCertificateRecoveryGrant())
					admin.Delete("/agents/{id}", s.handleDeregisterAgent())
					admin.Get("/settings/panel", s.handleGetPanelSettings())
					admin.Put("/settings/panel", s.handlePutPanelSettings())
					admin.Post("/settings/panel/restart", s.handleRestartPanel())
					admin.Get("/settings/retention", s.handleGetRetentionSettings())
					admin.Put("/settings/retention", s.handlePutRetentionSettings())
					admin.Get("/settings/updates", s.handleGetUpdateSettings())
					admin.Put("/settings/updates", s.handlePutUpdateSettings())
					admin.Post("/settings/updates/check", s.handleForceUpdateCheck())
					admin.Post("/settings/panel/update", s.handlePanelUpdate())
				})
			})
		})
	})

	if uiHandler := newUIHandler(s.uiFiles, panelPath); uiHandler != nil {
		router.NotFound(uiHandler)
	}

	// When agentPath is separate from panelPath, create an outer mux that
	// routes agent-prefixed requests to the agent endpoints directly and
	// everything else through the normal stripRootPath pipeline.
	if agentPath != "" && agentPath != panelPath {
		outer := chi.NewRouter()
		outer.Use(securityHeaders)
		outer.Use(maxBodySize)
		outer.Use(csrfOriginCheck(s.panelRuntime.HTTPRootPath, s.panelRuntime.AgentHTTPRootPath))
		outer.Route(agentPath+apiBasePath, func(agentAPI chi.Router) {
			agentAPI.With(s.withRateLimit(s.agentBootstrapRateLimiter, s.requestClientRateLimitKey)).
				Post("/agent/bootstrap", s.handleAgentBootstrap())
			agentAPI.With(s.withRateLimit(s.agentBootstrapRateLimiter, s.requestClientRateLimitKey)).
				Post("/agent/recover-certificate", s.handleAgentCertificateRecovery())
		})
		if panelPath != "" {
			outer.NotFound(stripRootPath(panelPath, router))
		} else {
			outer.NotFound(router.ServeHTTP)
		}
		return outer
	}

	if panelPath == "" {
		return router
	}

	return stripRootPath(panelPath, router)
}

func (s *Server) appendAudit(actorID string, action string, targetID string, details map[string]any) {
	s.appendAuditWithContext(context.Background(), actorID, action, targetID, details)
}

func (s *Server) appendAuditWithContext(ctx context.Context, actorID string, action string, targetID string, details map[string]any) {
	s.metricsAuditMu.Lock()
	s.auditSeq++
	event := AuditEvent{
		ID:        newSequenceID("audit", s.auditSeq),
		ActorID:   actorID,
		Action:    action,
		TargetID:  targetID,
		CreatedAt: s.now().UTC(),
		Details:   details,
	}
	s.appendAuditTrailLocked(event)
	s.metricsAuditMu.Unlock()

	// P2-LOG-10 / M-R4 / P7-R6: audit writes no longer block the HTTP
	// request path. The in-memory ring buffer above (PERF-02) already
	// serves the /api/audit read path, and storage persistence now runs
	// asynchronously on the batch writer. Close() drains the queue on
	// shutdown (StopWithTimeout 10s) so in-flight audit events still
	// survive a graceful restart. Persistent failures (NOT NULL, schema
	// mismatch, retry-exhausted) are surfaced by the batch writer with
	// slog.Error + alert=audit_persist_failed so operators can page on
	// the audit pipeline independently of other streams.
	//
	// ctx is intentionally unused here — the batch writer runs under its
	// own long-lived context, and the audit record has already been
	// copied into the ring buffer and captured by the snapshot so there
	// is nothing the HTTP request's cancellation should abort.
	_ = ctx
	if s.batchWriter != nil {
		s.batchWriter.auditEvents.Enqueue(auditEventToRecord(event))
	}

	s.events.publish(eventEnvelope{
		Type: "audit.created",
		Data: event,
	})
}

// appendAuditTrailLocked appends one event to the ring buffer in O(1) time.
// Caller must hold s.metricsAuditMu for writing.
func (s *Server) appendAuditTrailLocked(event AuditEvent) {
	capacity := len(s.auditBuf)
	if s.auditSize < capacity {
		// Ring still filling — insert at the next free slot, which is the
		// element immediately after the current tail.
		s.auditBuf[s.auditSize] = event
		s.auditSize++
		return
	}
	// Ring is full — overwrite the oldest slot (auditHead), then advance
	// auditHead to point at the new oldest slot.
	s.auditBuf[s.auditHead] = event
	s.auditHead++
	if s.auditHead == capacity {
		s.auditHead = 0
	}
}

// snapshotAuditTrailLocked returns a newly allocated slice of the current
// audit events in oldest-to-newest order. Caller must hold metricsAuditMu
// for reading. The returned slice is safe to retain after the lock is
// released; it does not alias s.auditBuf.
func (s *Server) snapshotAuditTrailLocked() []AuditEvent {
	out := make([]AuditEvent, s.auditSize)
	if s.auditSize == 0 {
		return out
	}
	capacity := len(s.auditBuf)
	if s.auditSize < capacity {
		// Head is still 0 while the ring is filling; entries are at [0,size).
		copy(out, s.auditBuf[:s.auditSize])
		return out
	}
	// Ring is full: oldest entry lives at auditHead. Copy the tail segment
	// [head, end) first, then wrap around for [0, head).
	n := copy(out, s.auditBuf[s.auditHead:])
	copy(out[n:], s.auditBuf[:s.auditHead])
	return out
}
