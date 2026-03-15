package server

import (
	"crypto/tls"
	"context"
	"io/fs"
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
	"github.com/panvex/panvex/internal/security"
)

const (
	sessionCookieName = "panvex_session"
	apiBasePath       = "/api"
)

// Options defines the runtime dependencies used by the control-plane server.
type Options struct {
	Now   func() time.Time
	Users []auth.User
	Store storage.Store
	UIFiles fs.FS
}

// Server wires local-auth, inventory, jobs, and operator APIs into one HTTP surface.
type Server struct {
	auth       *auth.Service
	enrollment *security.EnrollmentService
	store      storage.Store
	uiFiles    fs.FS
	jobs       *jobs.Service
	presence   *presence.Tracker
	events     *eventHub
	authority  *certificateAuthority
	now        func() time.Time

	mu         sync.RWMutex
	agentSeq   uint64
	auditSeq   uint64
	metricSeq  uint64
	deliveredJobs map[string]map[string]bool
	agents     map[string]Agent
	instances  map[string]Instance
	metrics    []MetricSnapshot
	auditTrail []AuditEvent
	handler    http.Handler
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
		agents:     make(map[string]Agent),
		deliveredJobs: make(map[string]map[string]bool),
		instances:  make(map[string]Instance),
		metrics:    make([]MetricSnapshot, 0),
		auditTrail: make([]AuditEvent, 0),
	}
	authority, err := newCertificateAuthority(now())
	if err != nil {
		panic(err)
	}
	server.authority = authority
	if options.Store != nil {
		server.jobs = jobs.NewServiceWithStore(options.Store)
		server.seedUsers(options.Users)
		server.auth = auth.NewServiceWithStore(options.Store)
		server.restoreStoredState()
	} else if len(options.Users) > 0 {
		server.auth.LoadUsers(options.Users)
	}
	server.handler = server.routes()

	return server
}

func (s *Server) seedUsers(users []auth.User) {
	if s.store == nil || len(users) == 0 {
		return
	}

	records, err := s.store.ListUsers(context.Background())
	if err != nil {
		panic(err)
	}
	if len(records) > 0 {
		return
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
			panic(err)
		}
	}
}

func (s *Server) restoreStoredState() {
	agents, err := s.store.ListAgents(context.Background())
	if err != nil {
		panic(err)
	}
	for _, record := range agents {
		agent := agentFromRecord(record)
		s.agents[agent.ID] = agent
		s.agentSeq = maxPrefixedSequence(s.agentSeq, "agent", agent.ID)
	}

	instances, err := s.store.ListInstances(context.Background())
	if err != nil {
		panic(err)
	}
	for _, record := range instances {
		instance := instanceFromRecord(record)
		s.instances[instance.ID] = instance
	}

	metrics, err := s.store.ListMetricSnapshots(context.Background())
	if err != nil {
		panic(err)
	}
	for _, record := range metrics {
		snapshot := metricSnapshotFromRecord(record)
		s.metrics = append(s.metrics, snapshot)
		s.metricSeq = maxPrefixedSequence(s.metricSeq, "metric", snapshot.ID)
	}

	auditEvents, err := s.store.ListAuditEvents(context.Background())
	if err != nil {
		panic(err)
	}
	for _, record := range auditEvents {
		event := auditEventFromRecord(record)
		s.auditTrail = append(s.auditTrail, event)
		s.auditSeq = maxPrefixedSequence(s.auditSeq, "audit", event.ID)
	}
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

// GRPCTLSConfig returns the TLS configuration used by the agent gateway listener.
func (s *Server) GRPCTLSConfig() *tls.Config {
	return s.authority.serverTLSConfig()
}

func (s *Server) routes() http.Handler {
	router := chi.NewRouter()
	router.Route(apiBasePath, func(api chi.Router) {
		api.Get("/auth/me", s.handleMe())
		api.Post("/auth/login", s.handleLogin())
		api.Post("/auth/logout", s.handleLogout())
		api.Post("/auth/totp/setup", s.handleTotpSetup())
		api.Post("/auth/totp/enable", s.handleTotpEnable())
		api.Post("/auth/totp/disable", s.handleTotpDisable())

		api.Get("/fleet", s.handleFleet())
		api.Get("/agents", s.handleAgents())
		api.Get("/instances", s.handleInstances())
		api.Get("/jobs", s.handleJobs())
		api.Post("/jobs", s.handleCreateJob())
		api.Get("/audit", s.handleAudit())
		api.Get("/metrics", s.handleMetrics())
		api.Get("/events", s.handleEvents())
		api.Get("/users", s.handleUsers())
		api.Post("/users/{id}/totp/reset", s.handleResetUserTotp())
		api.Post("/agents/enrollment-tokens", s.handleCreateEnrollmentToken())
	})
	if uiHandler := newUIHandler(s.uiFiles); uiHandler != nil {
		router.NotFound(uiHandler)
	}

	return router
}

func (s *Server) appendAudit(actorID string, action string, targetID string, details map[string]any) {
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
	s.mu.Unlock()

	if s.store != nil {
		if err := s.store.AppendAuditEvent(context.Background(), auditEventToRecord(event)); err != nil {
			panic(err)
		}
	}

	s.mu.Lock()
	s.auditTrail = append(s.auditTrail, event)
	s.mu.Unlock()
	s.events.publish(eventEnvelope{
		Type: "audit.created",
		Data: event,
	})
}
