package agenttransport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"errors"
)

// ErrCertPinMismatch is returned when the served certificate's SPKI hash
// does not match the pin recorded at enroll time. Fail-closed: operators
// must re-enroll the agent or rotate its credentials — there is no
// "trust on next dial" fallback. (S-02)
var ErrCertPinMismatch = errors.New("agent cert SPKI pin mismatch")

// ErrCertPinMissing is returned when no SPKI pin is stored for the agent.
// Since every issuance path persists a pin, a missing pin means the agent
// never completed enrollment against this panel (or the row was wiped) —
// fail closed. Operator action: re-enroll the agent or issue a certificate
// recovery grant; do NOT bypass by clearing transport mode.
var ErrCertPinMissing = errors.New("agent cert SPKI pin missing: re-enroll the agent or issue a recovery grant")

// CertPinReader is the subset of storage.FleetStore that the outbound
// supervisor consults when verifying the agent's certificate after the TLS
// handshake completes. Defined here (consumer side) so the agenttransport
// package does not import the storage package directly.
type CertPinReader interface {
	// GetAgentCertPin returns the SHA-256 SPKI pin recorded at enroll time.
	// Returns storage.ErrNotFound if no agent with the given ID exists or the
	// pin has not yet been set. A missing or empty pin causes the caller to
	// reject the dial (fail-closed) — see ErrCertPinMissing. (A1)
	GetAgentCertPin(ctx context.Context, agentID string) ([]byte, error)
}

// CertPinVerifyObserver is called after each cert-pin verification attempt.
// result is one of: "ok", "mismatch", "missing". A nil observer is a no-op.
// This is the seam used by the server's Prometheus collector to maintain
// panvex_agent_cert_pin_total. (S-02)
type CertPinVerifyObserver func(result string)

// verifyCertPin compares the SHA-256 of cert.RawSubjectPublicKeyInfo to
// expectedPin in constant time. An empty expectedPin (len == 0) skips the
// check — used for agents enrolled before S-02 deployment. A nil cert with
// a non-empty pin is treated as a mismatch (fail-closed). (S-02)
func verifyCertPin(cert *x509.Certificate, expectedPin []byte) error {
	if len(expectedPin) == 0 {
		return nil
	}
	if cert == nil {
		return ErrCertPinMismatch
	}
	actual := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	if subtle.ConstantTimeCompare(actual[:], expectedPin) != 1 {
		return ErrCertPinMismatch
	}
	return nil
}
