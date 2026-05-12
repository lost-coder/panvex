package bootstrap

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
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

// CertPinWriter is the subset of storage.FleetStore that EnrollDriver uses to
// persist the SPKI pin after a successful enrollment exchange. It is separated
// from EnrollQueries because the dbsqlc-generated Queries type uses a params
// struct signature; the storage layer provides the cleaner (agentID, pin)
// signature that matches this interface.
type CertPinWriter interface {
	// UpdateAgentCertPin persists the SHA-256 SPKI pin for the agent. Returns
	// ErrNotFound if no agent with the given ID exists.
	UpdateAgentCertPin(ctx context.Context, agentID string, pin []byte) error
}

// AttemptRecorder is called by EnrollDriver.Run at each terminal outcome to
// record the result label. The concrete wiring is
// (*metricsCollectors).ObserveBootstrapAttempt in package server. Bounded
// label values: "success", "expired", "mismatch", "agent_id_mismatch",
// "misbehavior", "error". A nil value is treated as a no-op.
type AttemptRecorder func(result string)

// EnrollDriver executes the panel-side bootstrap enrollment exchange over a
// reverse (panel-dials-agent) gRPC connection. Wiring it into the outbound
// supervisor when bootstrap_state=pending is the next step.
type EnrollDriver struct {
	queries   EnrollQueries
	ca        CertificateAuthority
	logger    *slog.Logger
	now       func() time.Time
	recorder  AttemptRecorder               // optional; nil → no-op
	notifier  EventNotifier                 // optional; nil → no-op
	pinWriter atomic.Pointer[CertPinWriter] // optional; nil → cert pinning skipped (logged)
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

// EventNotifier is called by EnrollDriver.Run to publish audit/eventbus
// events. The action string follows the "subsystem.event_name" convention
// used elsewhere in the codebase (e.g. "bootstrap.enrollment_attempted").
// A nil value is treated as a no-op.
type EventNotifier func(action, agentID string)

// SetAttemptRecorder wires a callback that is invoked at each terminal outcome
// of Run with the result label. Safe to call before the first Run.
func (d *EnrollDriver) SetAttemptRecorder(r AttemptRecorder) {
	d.recorder = r
}

// SetEventNotifier wires a callback that is invoked to publish audit/eventbus
// events at key points in Run. Safe to call before the first Run.
func (d *EnrollDriver) SetEventNotifier(n EventNotifier) {
	d.notifier = n
}

// SetCertPinWriter wires the storage backend that persists agent SPKI pins
// after successful enrollment (S-02). Safe to call concurrently with active
// Run invocations — atomic.Pointer guarantees a tear-free swap, so callers
// may rewire the writer at any point in the driver's lifecycle. If not set,
// cert pinning is skipped and a warning is logged; enrollment still
// completes. Callers SHOULD always set this in production to enable pin
// verification on subsequent dials (Task 10).
func (d *EnrollDriver) SetCertPinWriter(w CertPinWriter) {
	if w == nil {
		d.pinWriter.Store(nil)
		return
	}
	d.pinWriter.Store(&w)
}

// record invokes d.recorder if set. Inlined at every return path in Run.
func (d *EnrollDriver) record(result string) {
	if d.recorder != nil {
		d.recorder(result)
	}
}

// notify invokes d.notifier if set.
func (d *EnrollDriver) notify(action, agentID string) {
	if d.notifier != nil {
		d.notifier(action, agentID)
	}
}

// persistCertPin computes the SHA-256 of the agent's serving certificate's
// SubjectPublicKeyInfo and persists it via the storage layer.
//
// The pin is bound to the public key (not the full DER cert) so a renewed
// certificate carrying the same keypair still passes verification — exactly
// the behaviour needed for rolling cert renewals. Subsequent dials (Task 10)
// verify the served leaf cert hashes to this value (S-02).
//
// Returns an error if pinWriter is nil (caller must set it via SetCertPinWriter).
func (d *EnrollDriver) persistCertPin(ctx context.Context, agentID string, cert *x509.Certificate) error {
	if cert == nil {
		return errors.New("persistCertPin: nil certificate")
	}
	pw := d.pinWriter.Load()
	if pw == nil {
		return errors.New("persistCertPin: no CertPinWriter configured")
	}
	pin := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return (*pw).UpdateAgentCertPin(ctx, agentID, pin[:])
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
// Production wiring: cmd/control-plane/serve.go constructs an enrollFn closure
// over Run and passes it to agenttransport.Manager.SetEnrollCallbacks. The
// outbound supervisor invokes enrollFn before connectAndServe whenever the
// agent's bootstrap_state is "pending" (see agenttransport/outbound.go).
func (d *EnrollDriver) Run(ctx context.Context, agentAddr string, tlsCfg *tls.Config, agentID string) error {
	d.notify("bootstrap.enrollment_attempted", agentID)

	row, err := d.queries.GetAgentTransport(ctx, agentID)
	if err != nil {
		d.record("error")
		return fmt.Errorf("enroll: lookup: %w", err)
	}
	if row.BootstrapState != "pending" {
		d.record("error")
		return status.Errorf(codes.FailedPrecondition,
			"agent %s bootstrap_state=%s", agentID, row.BootstrapState)
	}
	if !row.BootstrapExpiresAt.Valid {
		d.record("error")
		return status.Errorf(codes.FailedPrecondition,
			"agent %s bootstrap_expires_at not set", agentID)
	}

	ctx, cancel := context.WithTimeout(ctx, enrollDeadline)
	defer cancel()

	// Clone tlsCfg so we can add a VerifyConnection hook without mutating the
	// caller's config. The hook captures the agent's leaf cert (SPKI) for
	// pinning; it fires inside the gRPC transport TLS handshake, before any
	// RPC data is exchanged.
	var agentLeafCert *x509.Certificate
	enrollTLS := tlsCfg.Clone()
	prevVerifyConn := enrollTLS.VerifyConnection
	enrollTLS.VerifyConnection = func(cs tls.ConnectionState) error {
		if len(cs.PeerCertificates) > 0 {
			agentLeafCert = cs.PeerCertificates[0]
		}
		if prevVerifyConn != nil {
			return prevVerifyConn(cs)
		}
		return nil
	}

	conn, err := grpc.NewClient(agentAddr, grpc.WithTransportCredentials(credentials.NewTLS(enrollTLS)))
	if err != nil {
		d.record("error")
		return fmt.Errorf("enroll: dial: %w", err)
	}
	defer conn.Close()
	client := gatewayrpc.NewAgentGatewayClient(conn)
	stream, err := client.EnrollOutbound(ctx)
	if err != nil {
		d.record("error")
		return fmt.Errorf("enroll: open stream: %w", err)
	}

	msg, err := stream.Recv()
	if err != nil {
		d.record("error")
		return fmt.Errorf("enroll: recv opening: %w", err)
	}
	opening := msg.GetOpening()
	if opening == nil {
		d.record("misbehavior")
		return ErrAgentMisbehavior
	}
	if opening.AgentId != row.ID {
		d.record("agent_id_mismatch")
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
			d.record("expired")
			d.notify("bootstrap.enrollment_expired", agentID)
			return ErrBootstrapTokenExpired
		}
		d.record("mismatch")
		return ErrBootstrapTokenMismatch
	}

	certPEM, caPEM, expiresAt, err := d.ca.SignCSR(opening.CsrPem, opening.AgentId, agentCertTTL)
	if err != nil {
		d.record("error")
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
		d.record("error")
		return fmt.Errorf("enroll: send cert: %w", err)
	}
	certIssued = true
	if err := stream.CloseSend(); err != nil {
		d.logger.Warn("bootstrap: cert was issued but close-send failed; manual cleanup may be needed",
			"agent_id", agentID, "error", err)
		d.record("error")
		return fmt.Errorf("enroll: close send: %w", err)
	}

	// Wait for the agent to close its half (signal of success). EOF = OK.
	if _, err := stream.Recv(); err != nil && !errors.Is(err, io.EOF) {
		if certIssued {
			d.logger.Warn("bootstrap: cert was issued but final recv failed; manual cleanup may be needed",
				"agent_id", agentID, "error", err)
		}
		d.record("error")
		return fmt.Errorf("enroll: wait close: %w", err)
	}

	if err := d.queries.ClearAgentBootstrapToken(ctx, agentID); err != nil {
		d.logger.Warn("bootstrap: cert was issued but DB clear failed; manual cleanup may be needed",
			"agent_id", agentID, "error", err)
		d.record("error")
		return fmt.Errorf("enroll: clear token: %w", err)
	}

	// Persist the SPKI pin for the agent's serving cert (S-02). This must
	// happen after the bootstrap token is cleared (agent is now active) and
	// before Run returns nil so callers can rely on the pin being set.
	//
	// Fail-closed: if storage is broken the operator must know; the agent will
	// retry enrollment and the panel will re-attempt pinning on the next Run.
	// If no pinWriter is configured (e.g. legacy wiring), log a warning and
	// continue — cert-serial verification (Q4.U-S-04) still provides layered
	// defence; operators should wire SetCertPinWriter for full S-02 coverage.
	if d.pinWriter.Load() != nil {
		if agentLeafCert == nil {
			d.record("error")
			return fmt.Errorf("enroll: TLS handshake completed but no peer certificate captured for agent %s", agentID)
		}
		if err := d.persistCertPin(ctx, agentID, agentLeafCert); err != nil {
			d.record("error")
			return fmt.Errorf("enroll: persist cert pin: %w", err)
		}
	} else {
		d.logger.Warn("bootstrap: CertPinWriter not configured; SPKI pin not persisted (S-02 disabled)",
			"agent_id", agentID,
			"hint", "call SetCertPinWriter on EnrollDriver at startup")
	}

	d.record("success")
	d.notify("bootstrap.enrollment_completed", agentID)
	return nil
}
