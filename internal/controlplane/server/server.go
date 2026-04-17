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
	httpLoginRateLimitPerWindow = 30
	httpAgentBootstrapRateLimitPerWindow = 30
	grpcConnectRateLimitPerWindow = 30
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
	agentSeq   uint64
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
	auditTrail []AuditEvent
	panelSettings  PanelSettings
	updateSettings UpdateSettings
	updateState    UpdateState
	retention      RetentionSettings
	handler      http.Handler
	startupErr   error
	stopRollup   context.CancelFunc
	rollupWg     sync.WaitGroup
	batchWriter  *storeBatchWriter
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
		auditTrail: make([]AuditEvent, 0, maxInMemoryAuditEvents),
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
	} else if len(options.Users) > 0 {
		server.auth.LoadUsers(options.Users)
	}
	server.handler = server.routes()
	server.auth.SetNow(now)
	server.jobs.SetNow(now)
	rollupCtx, rollupCancel := context.WithCancel(context.Background())
	server.stopRollup = rollupCancel
	server.startTimeseriesRollupWorker(rollupCtx)
	server.startUpdateCheckerWorker(rollupCtx)

	if server.store != nil {
		server.batchWriter = newStoreBatchWriter(server.store)
		server.batchWriter.Start()
	}

	return server
}

// Close stops background workers and drains pending writes. It should be
// called before closing the storage backend.
func (s *Server) Close() {
	if s.stopRollup != nil {
		s.stopRollup()
	}
	// Wait for the rollup goroutine to finish before closing the store,
	// so it does not query a closed storage backend.
	s.rollupWg.Wait()
	if s.batchWriter != nil {
		s.batchWriter.Stop()
	}
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
		s.agentSeq = maxPrefixedSequence(s.agentSeq, "agent", agent.ID)
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
		s.auditTrail = append(s.auditTrail, auditEventFromRecord(record))
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
	router.Use(securityHeaders)
	router.Use(maxBodySize)
	router.Use(csrfOriginCheck)
	router.Get("/healthz", handleHealthz())
	router.Get("/readyz", s.handleReadyz())

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
				authenticated.Post("/auth/totp/setup", s.handleTotpSetup())
				authenticated.Post("/auth/totp/enable", s.handleTotpEnable())
				authenticated.Post("/auth/totp/disable", s.handleTotpDisable())
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
					operator.Post("/clients/{id}/rotate-secret", s.handleRotateClientSecret())
					operator.Get("/discovered-clients", s.handleDiscoveredClients())
					operator.Post("/discovered-clients/{id}/adopt", s.handleAdoptDiscoveredClient())
					operator.Post("/discovered-clients/{id}/ignore", s.handleIgnoreDiscoveredClient())
					operator.Post("/telemetry/servers/{id}/refresh-diagnostics", s.handleTelemetryServerRefreshDiagnostics())
					operator.Get("/fleet-groups", s.handleFleetGroups())
					operator.Patch("/agents/{id}", s.handleRenameAgent())
					operator.Get("/agents/enrollment-tokens", s.handleListEnrollmentTokens())
					operator.Post("/agents/enrollment-tokens", s.handleCreateEnrollmentToken())
					operator.Post("/agents/enrollment-tokens/{value}/revoke", s.handleRevokeEnrollmentToken())
					operator.Post("/agents/{id}/update", s.handleAgentUpdate())
					operator.Get("/agent/update/binary", s.handleAgentBinaryProxy())
				})

				authenticated.Group(func(admin chi.Router) {
					admin.Use(s.requireMinimumRole(auth.RoleAdmin))
					admin.Get("/users", s.handleUsers())
					admin.Post("/users", s.handleCreateUser())
					admin.Put("/users/{id}", s.handleUpdateUser())
					admin.Delete("/users/{id}", s.handleDeleteUser())
					admin.Post("/users/{id}/totp/reset", s.handleResetUserTotp())
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
		outer.Use(csrfOriginCheck)
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
	if len(s.auditTrail) < maxInMemoryAuditEvents {
		s.auditTrail = append(s.auditTrail, event)
	} else {
		copy(s.auditTrail, s.auditTrail[1:])
		s.auditTrail[len(s.auditTrail)-1] = event
	}
	s.metricsAuditMu.Unlock()

	// Audit events are immutable records that must survive crashes, so they
	// are written to storage synchronously instead of via the batch writer.
	if s.store != nil {
		if err := s.store.AppendAuditEvent(ctx, auditEventToRecord(event)); err != nil {
			s.logger.Error("persist audit event failed", "action", action, "error", err)
		}
	}

	s.events.publish(eventEnvelope{
		Type: "audit.created",
		Data: event,
	})
}
