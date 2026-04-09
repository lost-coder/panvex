package server

import (
	"crypto/tls"
	"context"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/jobs"
	"github.com/panvex/panvex/internal/controlplane/presence"
	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/gatewayrpc"
	"github.com/panvex/panvex/internal/security"
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

	mu         sync.RWMutex
	sessionMu  sync.RWMutex
	agentSeq   uint64
	sessionSeq uint64
	auditSeq   uint64
	metricSeq  uint64
	clientSeq  uint64
	assignmentSeq uint64
	discoveredClientSeq uint64
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
	panelSettings PanelSettings
	retention  RetentionSettings
	handler    http.Handler
	startupErr error
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
	server.panelSettings = defaultPanelSettings()
	server.retention = defaultRetentionSettings()
	authority, err := loadOrCreateCertificateAuthority(options.Store, now())
	if err != nil {
		server.startupErr = err
	} else {
		server.authority = authority
	}
	if options.Store != nil {
		server.jobs = jobs.NewServiceWithStore(options.Store)
		server.auth = auth.NewServiceWithStore(options.Store)
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
	} else if len(options.Users) > 0 {
		server.auth.LoadUsers(options.Users)
	}
	server.handler = server.routes()
	server.auth.SetNow(now)
	server.jobs.SetNow(now)
	server.startTimeseriesRollupWorker(context.Background())

	return server
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
		snapshot := metricSnapshotFromRecord(record)
		if len(s.metrics) < maxInMemoryMetricSnapshots {
			s.metrics = append(s.metrics, snapshot)
		} else {
			copy(s.metrics, s.metrics[1:])
			s.metrics[len(s.metrics)-1] = snapshot
		}
		s.metricSeq = maxPrefixedSequence(s.metricSeq, "metric", snapshot.ID)
	}

	auditEvents, err := s.store.ListAuditEvents(context.Background())
	if err != nil {
		return err
	}
	for _, record := range auditEvents {
		event := auditEventFromRecord(record)
		if len(s.auditTrail) < maxInMemoryAuditEvents {
			s.auditTrail = append(s.auditTrail, event)
		} else {
			copy(s.auditTrail, s.auditTrail[1:])
			s.auditTrail[len(s.auditTrail)-1] = event
		}
		s.auditSeq = maxPrefixedSequence(s.auditSeq, "audit", event.ID)
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
	router.Route(apiBasePath, func(api chi.Router) {
		api.With(s.withRateLimit(s.agentBootstrapRateLimiter, requestClientRateLimitKey)).Post("/agent/bootstrap", s.handleAgentBootstrap())
		api.With(s.withRateLimit(s.agentBootstrapRateLimiter, requestClientRateLimitKey)).Post("/agent/recover-certificate", s.handleAgentCertificateRecovery())
		api.With(s.withRateLimit(s.loginRateLimiter, requestClientRateLimitKey)).Post("/auth/login", s.handleLogin())

		api.Group(func(authenticated chi.Router) {
			authenticated.Use(s.requireAuthenticatedSession())
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
			})
		})
	})
	if uiHandler := newUIHandler(s.uiFiles, s.panelRuntime.HTTPRootPath); uiHandler != nil {
		router.NotFound(uiHandler)
	}

	if s.panelRuntime.HTTPRootPath == "" {
		return router
	}

	return stripRootPath(s.panelRuntime.HTTPRootPath, router)
}

func (s *Server) appendAudit(actorID string, action string, targetID string, details map[string]any) {
	s.appendAuditWithContext(context.Background(), actorID, action, targetID, details)
}

func (s *Server) appendAuditWithContext(ctx context.Context, actorID string, action string, targetID string, details map[string]any) {
	s.mu.Lock()
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
	var persistErr error
	if s.store != nil {
		persistErr = s.store.AppendAuditEvent(ctx, auditEventToRecord(event))
	}
	s.mu.Unlock()

	if persistErr != nil {
		log.Printf("control-plane audit persistence failed for action %q: %v", action, persistErr)
	}

	s.events.publish(eventEnvelope{
		Type: "audit.created",
		Data: event,
	})
}
