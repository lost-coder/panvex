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
	// AcceptedAgentCertPins returns every SHA-256 SPKI pin the panel currently
	// accepts for the agent: the pin of the newest issued certificate, plus —
	// while a rotation overlap window is open — the pin of the one it replaced
	// (R11).
	//
	// Returns storage.ErrNotFound if no agent with the given ID exists. An
	// empty result causes the caller to reject the dial (fail-closed) — see
	// ErrCertPinMissing. (A1)
	//
	// The overlap is what keeps a listen-mode node reachable when a renewal
	// exchange is interrupted: the panel has already recorded the new
	// certificate, the agent still holds the old one, and without the overlap
	// the dial verifier below would refuse the only credential the node has.
	AcceptedAgentCertPins(ctx context.Context, agentID string) ([][]byte, error)
}

// CertPinVerifyObserver is called after each cert-pin verification attempt.
// result is one of: "ok", "mismatch", "missing". A nil observer is a no-op.
// This is the seam used by the server's Prometheus collector to maintain
// panvex_agent_cert_pin_total. (S-02)
type CertPinVerifyObserver func(result string)

// verifyCertPin compares the SHA-256 of cert.RawSubjectPublicKeyInfo against
// every accepted pin, in constant time per candidate. A nil cert is a mismatch
// (fail-closed). The caller has already rejected an empty candidate set, so
// reaching here with one is a mismatch too. (S-02)
func verifyCertPin(cert *x509.Certificate, acceptedPins [][]byte) error {
	if cert == nil {
		return ErrCertPinMismatch
	}
	actual := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	for _, pin := range acceptedPins {
		if len(pin) == 0 {
			continue
		}
		if subtle.ConstantTimeCompare(actual[:], pin) == 1 {
			return nil
		}
	}
	return ErrCertPinMismatch
}
