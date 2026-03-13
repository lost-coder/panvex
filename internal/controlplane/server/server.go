package server

import (
	"crypto/tls"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/jobs"
	"github.com/panvex/panvex/internal/controlplane/presence"
	"github.com/panvex/panvex/internal/security"
)

const sessionCookieName = "panvex_session"

// Options defines the runtime dependencies used by the control-plane server.
type Options struct {
	Now   func() time.Time
	Users []auth.User
}

// Server wires local-auth, inventory, jobs, and operator APIs into one HTTP surface.
type Server struct {
	auth       *auth.Service
	enrollment *security.EnrollmentService
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
	if len(options.Users) > 0 {
		server.auth.LoadUsers(options.Users)
	}
	server.handler = server.routes()

	return server
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
	router.Get("/auth/me", s.handleMe())
	router.Post("/auth/login", s.handleLogin())
	router.Post("/auth/logout", s.handleLogout())

	router.Get("/fleet", s.handleFleet())
	router.Get("/agents", s.handleAgents())
	router.Get("/instances", s.handleInstances())
	router.Get("/jobs", s.handleJobs())
	router.Post("/jobs", s.handleCreateJob())
	router.Get("/audit", s.handleAudit())
	router.Get("/metrics", s.handleMetrics())
	router.Get("/events", s.handleEvents())
	router.Post("/agents/enrollment-tokens", s.handleCreateEnrollmentToken())

	return router
}

func (s *Server) appendAudit(actorID string, action string, targetID string, details map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.auditSeq++
	s.auditTrail = append(s.auditTrail, AuditEvent{
		ID:        newSequenceID("audit", s.auditSeq),
		ActorID:   actorID,
		Action:    action,
		TargetID:  targetID,
		CreatedAt: s.now().UTC(),
		Details:   details,
	})
	s.events.publish(eventEnvelope{
		Type: "audit.created",
		Data: s.auditTrail[len(s.auditTrail)-1],
	})
}
