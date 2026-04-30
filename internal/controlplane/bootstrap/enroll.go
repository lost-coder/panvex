package bootstrap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/lost-coder/panvex/internal/dbsqlc"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	agentCertTTL = 90 * 24 * time.Hour
	// enrollDeadline bounds the entire enrollment exchange. Without it a
	// stalled agent (opens stream, never sends Opening) would block Run()
	// until the caller's ctx is cancelled — and the caller may pass a
	// background context unaware of this requirement.
	enrollDeadline = 30 * time.Second
)

// Errors returned by EnrollDriver.Run, distinct from the underlying token
// validation errors so callers (alerting, metrics, supervisor wrap-around)
// can categorize bootstrap failures cleanly.
var (
	ErrBootstrapTokenExpired  = errors.New("bootstrap: token expired (panel)")
	ErrBootstrapTokenMismatch = errors.New("bootstrap: token mismatch")
	ErrAgentIDMismatch        = errors.New("bootstrap: agent_id mismatch")
	ErrAgentMisbehavior       = errors.New("bootstrap: agent did not send opening")
)

// CertificateAuthority is the subset of CA functionality the enroll driver
// needs. Lives here so tests can supply a fake without depending on the
// panel's full authority package. Real wiring is a follow-up task — the
// existing certificateAuthority in package server doesn't support CSR
// signing yet.
type CertificateAuthority interface {
	// SignCSR validates and signs the given CSR PEM, binding the resulting
	// cert's CN to agentID. Returns the issued cert PEM, the CA cert PEM
	// (so the agent can pin), and the cert's NotAfter.
	SignCSR(csrPEM, agentID string, validFor time.Duration) (certPEM, caPEM string, expiresAt time.Time, err error)
}

// EnrollQueries is the subset of dbsqlc.Queries that EnrollDriver calls.
type EnrollQueries interface {
	GetAgentTransport(ctx context.Context, id string) (dbsqlc.GetAgentTransportRow, error)
	ExpireAgentBootstrapToken(ctx context.Context, id string) error
	ClearAgentBootstrapToken(ctx context.Context, id string) error
}

// EnrollDriver executes the panel-side bootstrap enrollment exchange over a
// reverse (panel-dials-agent) gRPC connection. Wiring it into the outbound
// supervisor when bootstrap_state=pending is the next step.
type EnrollDriver struct {
	queries EnrollQueries
	ca      CertificateAuthority
	logger  *slog.Logger
	now     func() time.Time
}

// NewEnrollDriver constructs an EnrollDriver. If now is nil, time.Now is used;
// if logger is nil, slog.Default() is used.
func NewEnrollDriver(q EnrollQueries, ca CertificateAuthority, logger *slog.Logger, now func() time.Time) *EnrollDriver {
	if now == nil {
		now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &EnrollDriver{queries: q, ca: ca, logger: logger, now: now}
}

// Run executes the panel-side enrollment exchange against an agent listening
// at agentAddr. The protocol is server-speaks-first from the panel's perspective:
//
//	panel → agent: opens stream (panel is the gRPC client)
//	agent → panel: EnrollOpening { token, agent_id, csr_pem }
//	panel: validates token + agent_id → signs CSR
//	panel → agent: EnrollCertificate { cert, ca, expiresAt }
//	panel: closes send-half; agent closes its half (EOF) on success
//
// State transitions:
//
//	ErrBootstrapTokenExpired  → bootstrap_state=expired in DB
//	any other error           → bootstrap_state=pending (retry possible)
//	nil                       → bootstrap_state cleared (active)
//
// TLS requirement: tlsCfg MUST NOT require a client certificate. Enrollment
// is one-way TLS — the agent has no cert to present yet (that's what this
// exchange produces). Use a separate tlsCfg from the post-enrollment mTLS
// one used by the outbound supervisor.
//
// Run applies an internal enrollDeadline so a stalled agent does not block
// indefinitely. The caller's ctx is still respected; whichever fires first
// terminates the exchange.
//
// TODO: invoke EnrollDriver before connectAndServe in outboundSupervisor.run
// when bootstrap_state=pending (agenttransport/outbound.go).
func (d *EnrollDriver) Run(ctx context.Context, agentAddr string, tlsCfg *tls.Config, agentID string) error {
	row, err := d.queries.GetAgentTransport(ctx, agentID)
	if err != nil {
		return fmt.Errorf("enroll: lookup: %w", err)
	}
	if row.BootstrapState != "pending" {
		return status.Errorf(codes.FailedPrecondition,
			"agent %s bootstrap_state=%s", agentID, row.BootstrapState)
	}
	if !row.BootstrapExpiresAt.Valid {
		return status.Errorf(codes.FailedPrecondition,
			"agent %s bootstrap_expires_at not set", agentID)
	}

	ctx, cancel := context.WithTimeout(ctx, enrollDeadline)
	defer cancel()

	conn, err := grpc.NewClient(agentAddr, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	if err != nil {
		return fmt.Errorf("enroll: dial: %w", err)
	}
	defer conn.Close()
	client := gatewayrpc.NewAgentGatewayClient(conn)
	stream, err := client.EnrollOutbound(ctx)
	if err != nil {
		return fmt.Errorf("enroll: open stream: %w", err)
	}

	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("enroll: recv opening: %w", err)
	}
	opening := msg.GetOpening()
	if opening == nil {
		return ErrAgentMisbehavior
	}
	if opening.AgentId != row.ID {
		return ErrAgentIDMismatch
	}

	var hash [32]byte
	copy(hash[:], row.BootstrapTokenHash)
	if vErr := VerifyToken(opening.BootstrapToken, hash, row.BootstrapExpiresAt.Time, d.now()); vErr != nil {
		if errors.Is(vErr, ErrTokenExpired) {
			if expErr := d.queries.ExpireAgentBootstrapToken(ctx, agentID); expErr != nil {
				// Don't mask the operator-actionable expired-token error,
				// but make sure the failed state transition is visible so
				// we don't silently re-flip indefinitely on retry.
				d.logger.Warn("bootstrap: failed to mark token expired in DB",
					"agent_id", agentID, "error", expErr)
			}
			return ErrBootstrapTokenExpired
		}
		return ErrBootstrapTokenMismatch
	}

	certPEM, caPEM, expiresAt, err := d.ca.SignCSR(opening.CsrPem, opening.AgentId, agentCertTTL)
	if err != nil {
		return fmt.Errorf("enroll: sign csr: %w", err)
	}

	// certIssued tracks that the agent has received the cert; from this
	// point on a transport failure leaves the panel and agent in
	// inconsistent states (agent has a cert, panel still thinks pending).
	// We log loudly so an operator can recover (re-issue / revoke).
	var certIssued bool
	if err := stream.Send(&gatewayrpc.EnrollClientMessage{
		Body: &gatewayrpc.EnrollClientMessage_Certificate{
			Certificate: &gatewayrpc.EnrollCertificate{
				CertificatePem: certPEM,
				CaPem:          caPEM,
				ExpiresAtUnix:  expiresAt.Unix(),
			},
		},
	}); err != nil {
		return fmt.Errorf("enroll: send cert: %w", err)
	}
	certIssued = true
	if err := stream.CloseSend(); err != nil {
		d.logger.Warn("bootstrap: cert was issued but close-send failed; manual cleanup may be needed",
			"agent_id", agentID, "error", err)
		return fmt.Errorf("enroll: close send: %w", err)
	}

	// Wait for the agent to close its half (signal of success). EOF = OK.
	if _, err := stream.Recv(); err != nil && !errors.Is(err, io.EOF) {
		if certIssued {
			d.logger.Warn("bootstrap: cert was issued but final recv failed; manual cleanup may be needed",
				"agent_id", agentID, "error", err)
		}
		return fmt.Errorf("enroll: wait close: %w", err)
	}

	if err := d.queries.ClearAgentBootstrapToken(ctx, agentID); err != nil {
		d.logger.Warn("bootstrap: cert was issued but DB clear failed; manual cleanup may be needed",
			"agent_id", agentID, "error", err)
		return fmt.Errorf("enroll: clear token: %w", err)
	}
	return nil
}
