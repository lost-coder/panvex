package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// bootstrapPinReader is the storage subset newBootstrapTLSConfig consults to
// fetch the SPKI pin previously persisted by EnrollDriver for a given agent.
//
// Defined locally (not re-using agenttransport.CertPinReader) so this package
// can construct a verifier without importing agenttransport — the panel-side
// outbound supervisor is the consumer of that interface, not us. The shape is
// identical: storage.Store satisfies both via its GetAgentCertPin method.
type bootstrapPinReader interface {
	// GetAgentCertPin returns the SHA-256 SPKI pin recorded at enroll time.
	// Returns storage.ErrNotFound if no agent with the given ID exists or no
	// pin has been written yet. An empty (nil / zero-length) pin means the
	// agent enrolled before S-02 was deployed, or first contact has not yet
	// completed — both treated as "no pin → TOFU first contact" by the
	// verifier callback.
	GetAgentCertPin(ctx context.Context, agentID string) ([]byte, error)
}

// errBootstrapNoPeerCert is returned by the VerifyPeerCertificate callback
// when the agent's TLS handshake did not include any certificate. Should be
// impossible against a real gRPC agent (it always presents its self-signed
// leaf during enrollment), but we fail-closed anyway.
var errBootstrapNoPeerCert = errors.New("bootstrap: peer presented no certificate")

// errBootstrapPinMismatch is returned when an SPKI pin is stored for the
// agent but the leaf cert presented during the enrollment dial does not
// match it. This is the MITM-detection branch (S-2): the panel previously
// observed a different agent cert and refuses to talk to a swapped one.
var errBootstrapPinMismatch = errors.New("bootstrap: SPKI pin mismatch for agent")

// newBootstrapTLSConfig builds the TLS configuration the panel uses to dial
// an agent during the enrollment exchange (panel-dials-agent, i.e. reverse
// transport mode bootstrap).
//
// Why InsecureSkipVerify is still set: the agent presents a self-signed
// certificate at enrollment time — the cert's whole point of existing is
// that the panel has not yet signed one for it. There is no chain of trust
// to verify against, so the standard x509 chain verifier cannot run.
//
// What replaces it: a VerifyPeerCertificate callback that pins the leaf's
// SubjectPublicKeyInfo (SPKI) hash on a Trust-On-First-Use (TOFU) basis,
// keyed by agent ID. (S-2)
//
//   - If a pin is already stored for this agent (set by a prior successful
//     enrollment via EnrollDriver.persistCertPin), the callback REQUIRES
//     the presented leaf's SPKI to match, in constant time. A mismatch
//     fails the handshake — the panel refuses to proceed even if the
//     bootstrap token would otherwise be valid.
//   - If no pin is stored (first contact, fresh agent), the callback
//     allows the handshake. EnrollDriver.persistCertPin records the SPKI
//     pin AFTER the bootstrap token is validated — so a pin can only be
//     persisted when the operator-issued token also passed. Subsequent
//     dials lock to that pin.
//
// Threat model: a network attacker that swaps the agent's cert during the
// VERY FIRST enrollment is undetectable here (there is nothing to compare
// against). The bootstrap token still gates the actual cert issuance, and
// any swap on subsequent dials is detected by the pin check. Production
// deployments that need full coverage should pre-distribute the agent's
// expected SPKI pin.
//
// agentID is captured by closure so the callback can scope the storage
// lookup. ctx scopes the storage query — if the caller cancels enrollment,
// the callback's lookup unwinds with ctx.Err().
func newBootstrapTLSConfig(ctx context.Context, agentID string, pinReader bootstrapPinReader) *tls.Config {
	v := &bootstrapPinVerifier{
		ctx:       ctx,
		agentID:   agentID,
		pinReader: pinReader,
	}
	return &tls.Config{
		// Standard x509 chain verification is disabled because the agent
		// presents a self-signed cert at enrollment; there is no trust
		// anchor yet. SPKI pinning via VerifyPeerCertificate substitutes,
		// gated by the bootstrap token EnrollDriver.Run validates in-band.
		InsecureSkipVerify:    true, //nolint:gosec // S-2: replaced with VerifyPeerCertificate SPKI pin check below
		VerifyPeerCertificate: v.verifyPeerCertificate,
	}
}

// bootstrapPinVerifier carries the per-call dependencies the
// VerifyPeerCertificate callback needs. Kept as its own type so the callback
// can be exercised in unit tests with synthesized raw DER cert bytes — far
// simpler than spinning up a real TLS server.
type bootstrapPinVerifier struct {
	ctx       context.Context
	agentID   string
	pinReader bootstrapPinReader
}

// verifyPeerCertificate is installed as VerifyPeerCertificate on the
// bootstrap TLS dialer. Returns:
//
//   - nil on first contact (pin not stored) — TOFU; or on match of stored pin.
//   - errBootstrapNoPeerCert if rawCerts is empty.
//   - errBootstrapPinMismatch (wrapped) if a stored pin disagrees with the leaf.
//   - any storage lookup error other than ErrNotFound.
func (v *bootstrapPinVerifier) verifyPeerCertificate(rawCerts [][]byte, _ [][]*x509.Certificate) error {
	if len(rawCerts) == 0 {
		return errBootstrapNoPeerCert
	}
	leaf, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("bootstrap: parse peer leaf: %w", err)
	}
	actual := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)

	if v.pinReader == nil {
		// No reader wired (e.g. legacy test path) — fall through to TOFU.
		// EnrollDriver still persists via SetCertPinWriter on success.
		return nil
	}
	stored, err := v.pinReader.GetAgentCertPin(v.ctx, v.agentID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("bootstrap: pin lookup for agent %s: %w", v.agentID, err)
	}
	if len(stored) == 0 {
		// First contact for this agent — TOFU. EnrollDriver.persistCertPin
		// records the pin after the bootstrap token is validated, so a
		// pin can only be persisted when the operator-issued token also
		// passed. Subsequent dials enforce match (the branch below).
		return nil
	}
	if subtle.ConstantTimeCompare(actual[:], stored) != 1 {
		return fmt.Errorf("%w %s", errBootstrapPinMismatch, v.agentID)
	}
	return nil
}
