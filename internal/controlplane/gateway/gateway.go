// Package gateway hosts the agent-facing gRPC gateway: the Connect
// bidi-stream (dispatch/receive/snapshot/audit/result loops), unary
// certificate renewal, and enrollment-step ingestion. It was extracted
// from the controlplane/server god-package (P8.2d). Cross-domain
// operations whose bodies must stay in server (authorization, revocation,
// snapshot application, audit, client reconciliation, cert issuance) are
// reached through the Deps interface, implemented by *server.Server in
// server/gateway_deps.go. The gateway package MUST NOT import server.
package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/batchwriter"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/metrics"
	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/controlplane/runtimeevents"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// Log messages and audit action strings emitted from the agent stream.
// Duplicated verbatim from server/http_messages.go where the HTTP layer
// keeps its own copies; the values are the stable audit/log contract and
// must not drift.
const (
	logAgentStreamClosed  = "agent stream closed"
	logMessageReceived    = "message received"
	auditJobsResult       = "jobs.result"
	auditJobsAcknowledged = "jobs.acknowledged"
)

// InstanceSnapshot is the internal view of a wire InstanceSnapshot.
// Moved verbatim from server (fields already exported); server keeps an
// alias (instanceSnapshot = gateway.InstanceSnapshot).
type InstanceSnapshot struct {
	ID                string
	Name              string
	Version           string
	ConfigFingerprint string
	ManagedConfigHash string
	ManagedConfigJSON string
	Connections       int
	ReadOnly          bool
}

// AgentSnapshot is the internal representation of one agent runtime
// snapshot/heartbeat applied against panel state. Moved verbatim from
// server (agentSnapshot); server keeps an alias.
type AgentSnapshot struct {
	AgentID string
	// AgentBootID scopes the cumulative usage totals in Clients to one
	// agent process incarnation (P4). Empty only in unit tests that do
	// not exercise the usage path.
	AgentBootID              string
	NodeName                 string
	FleetGroupID             string
	Version                  string
	ReadOnly                 bool
	Instances                []InstanceSnapshot
	Clients                  []clients.UsageReport
	HasClients               bool
	ClientIPs                []ClientIPSnapshot
	HasClientIPs             bool
	Runtime                  *gatewayrpc.RuntimeSnapshot
	HasRuntime               bool
	RuntimeDiagnostics       *gatewayrpc.RuntimeDiagnosticsSnapshot
	RuntimeSecurityInventory *gatewayrpc.RuntimeSecurityInventorySnapshot
	Metrics                  map[string]uint64
	ObservedAt               time.Time
	// Partial=true when the agent could not collect a full telemt snapshot;
	// the panel preserves last-known version/connections/read_only/uptime
	// rather than overwriting them with blanks (IN-H6).
	Partial bool
}

// ClientIPSnapshot is the internal view of a wire ClientIPSnapshot. Moved
// verbatim from server (clientIPSnapshot); server keeps an alias.
type ClientIPSnapshot struct {
	ClientID  string
	ActiveIPs []string
}

// Deps are the reverse (cross-domain) dependencies the gateway needs from
// the server package. Implemented by *server.Server (see
// server/gateway_deps.go). The bodies of the authorization/revocation/
// renewal/enrollment methods physically live in server because they touch
// server-only state (s.mu, s.revokedAgentIDs, s.authority, s.enrollmentRec,
// s.grpcConnectRateLimiter, s.transportSwitchPendingAt); the rest are thin
// exported wrappers over existing private server methods.
type Deps interface {
	// AuthorizeAgentConnect is the former Server.authorizeAgentConnect:
	// identity + in-memory revocation + cert-serial pin + connect rate limit.
	AuthorizeAgentConnect(ctx context.Context, sess agenttransport.AgentSession) (agentID, presentedSerial string, err error)
	// ShouldTerminateForRevocation is the former shouldTerminateForRevocation.
	ShouldTerminateForRevocation(ctx context.Context, agentID, presentedSerial string) bool
	// MarkTransportSwitchResolved clears the A2 transport-switch marker.
	MarkTransportSwitchResolved(agentID string)
	// RegisterAgentSession / NotifyAgentSession proxy agents.SessionManager.
	RegisterAgentSession(agentID string, cancelConn context.CancelFunc) (*agents.Session, func())
	NotifyAgentSession(agentID string)
	// ApplyAgentSnapshot applies a runtime snapshot against panel state.
	ApplyAgentSnapshot(ctx context.Context, snap AgentSnapshot) error
	// AppendAudit records one audit-trail entry (best-effort).
	AppendAudit(ctx context.Context, actorID, action, targetID string, details map[string]any)
	// RecordClientJobResult updates client deployment state from a job result.
	RecordClientJobResult(ctx context.Context, agentID, jobID string, success bool, message, resultJSON string, observedAt time.Time)
	// ReconcileDiscoveredClients reconciles a full client-list response.
	ReconcileDiscoveredClients(ctx context.Context, agentID string, records []*gatewayrpc.ClientDetailRecord, telemtUnreachable bool, observedAt time.Time)
	// ResolveClientIDByName resolves a client_id from an agent-scoped name.
	ResolveClientIDByName(agentID, clientName string) string
	// RenewAgentCertificate is the post-authentication core of the unary
	// Server.RenewCertificate (issue-from-CSR + re-pin + in-memory update).
	RenewAgentCertificate(ctx context.Context, agentID string, req *gatewayrpc.RenewCertificateRequest) (*gatewayrpc.RenewCertificateResponse, error)
	// RecordEnrollmentSteps is the former Server.ReportEnrollmentSteps body.
	RecordEnrollmentSteps(ctx context.Context, req *gatewayrpc.ReportEnrollmentStepsRequest) (*gatewayrpc.ReportEnrollmentStepsResponse, error)
	// HandleInStreamRenewalRequest is the former handleInStreamRenewalRequest:
	// it signs the CSR, re-pins the serial, updates in-memory cert dates under
	// s.mu, and sends the RenewalResponse back over the stream.
	HandleInStreamRenewalRequest(ctx context.Context, agentID string, sess agenttransport.AgentSession, req *gatewayrpc.RenewalRequest)
}

// Config carries the direct subsystems and reverse dependencies the
// gateway needs. Writer may be nil when there is no persistent store; the
// existing g.writer != nil guards handle that.
type Config struct {
	Deps          Deps
	Logger        *slog.Logger
	Store         storage.Store
	Jobs          *jobs.Service
	Writer        *batchwriter.Writer
	Events        *eventbus.Hub
	RuntimeEvents *runtimeevents.Buffer
	Presence      *presence.Tracker
	Obs           *metrics.Collectors
	Now           func() time.Time
}

// Gateway implements gatewayrpc.AgentGatewayServer for the agent stream.
type Gateway struct {
	gatewayrpc.UnimplementedAgentGatewayServer
	deps          Deps
	logger        *slog.Logger
	store         storage.Store
	jobs          *jobs.Service
	writer        *batchwriter.Writer
	events        *eventbus.Hub
	runtimeEvents *runtimeevents.Buffer
	presence      *presence.Tracker
	obs           *metrics.Collectors
	now           func() time.Time
}

// New constructs a Gateway from cfg.
func New(cfg Config) *Gateway {
	return &Gateway{
		deps:          cfg.Deps,
		logger:        cfg.Logger,
		store:         cfg.Store,
		jobs:          cfg.Jobs,
		writer:        cfg.Writer,
		events:        cfg.Events,
		runtimeEvents: cfg.RuntimeEvents,
		presence:      cfg.Presence,
		obs:           cfg.Obs,
		now:           cfg.Now,
	}
}
