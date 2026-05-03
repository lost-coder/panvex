package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// fakePinReader is a tiny in-memory bootstrapPinReader for the SPKI-pin
// verifier tests. nil/empty stored slice is the "no pin" / TOFU branch;
// presence of getErr forces the lookup to fail with a non-NotFound error.
type fakePinReader struct {
	pins   map[string][]byte
	getErr error
}

func (f *fakePinReader) GetAgentCertPin(_ context.Context, agentID string) ([]byte, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	pin, ok := f.pins[agentID]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return pin, nil
}

// newSelfSignedRawDER returns the DER bytes of a fresh ECDSA P-256
// self-signed cert and the SHA-256 of its SubjectPublicKeyInfo. Mirrors the
// helper in internal/controlplane/bootstrap/enroll_test.go but kept local so
// this test file has no cross-package fixture dependency.
func newSelfSignedRawDER(t *testing.T) (rawDER []byte, spkiPin []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "panvex-agent-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pin := sha256.Sum256(parsed.RawSubjectPublicKeyInfo)
	return der, pin[:]
}

// TestBootstrapVerifier_FirstContact asserts the TOFU branch: when the pin
// store has nothing for agentID, the verifier accepts any leaf cert. This
// is the path the very first enrollment exchange takes.
func TestBootstrapVerifier_FirstContact(t *testing.T) {
	der, _ := newSelfSignedRawDER(t)
	reader := &fakePinReader{pins: map[string][]byte{}}

	cfg := newBootstrapTLSConfig(context.Background(), "agent-1", reader)
	if cfg.VerifyPeerCertificate == nil {
		t.Fatalf("VerifyPeerCertificate not installed")
	}
	if !cfg.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify must remain true (chain verification cannot run on self-signed enrollment cert)")
	}
	if err := cfg.VerifyPeerCertificate([][]byte{der}, nil); err != nil {
		t.Fatalf("first contact must succeed (TOFU); got: %v", err)
	}
}

// TestBootstrapVerifier_MatchingPin asserts re-enrollment with the same key
// passes — the stored SPKI matches the presented leaf's SPKI.
func TestBootstrapVerifier_MatchingPin(t *testing.T) {
	der, pin := newSelfSignedRawDER(t)
	reader := &fakePinReader{pins: map[string][]byte{"agent-1": pin}}

	cfg := newBootstrapTLSConfig(context.Background(), "agent-1", reader)
	if err := cfg.VerifyPeerCertificate([][]byte{der}, nil); err != nil {
		t.Fatalf("matching pin must succeed; got: %v", err)
	}
}

// TestBootstrapVerifier_MismatchedPin is the MITM-detection branch (S-2):
// the store has a pin from a prior enrollment, but the leaf cert presented
// during this dial does not match it. The verifier MUST refuse the
// handshake — even though a bootstrap token might still be embedded in
// the in-band exchange, that exchange is short-circuited at the TLS layer.
func TestBootstrapVerifier_MismatchedPin(t *testing.T) {
	der, _ := newSelfSignedRawDER(t)         // leaf the agent presents
	_, otherPin := newSelfSignedRawDER(t)    // unrelated pin already stored
	reader := &fakePinReader{pins: map[string][]byte{"agent-1": otherPin}}

	cfg := newBootstrapTLSConfig(context.Background(), "agent-1", reader)
	err := cfg.VerifyPeerCertificate([][]byte{der}, nil)
	if err == nil {
		t.Fatalf("mismatched pin must reject the handshake")
	}
	if !errors.Is(err, errBootstrapPinMismatch) {
		t.Fatalf("expected errBootstrapPinMismatch, got: %v", err)
	}
}

// TestBootstrapVerifier_NoPeerCert ensures the fail-closed branch when the
// peer presents zero certificates (should not happen in practice but the
// callback must not deference an empty slice).
func TestBootstrapVerifier_NoPeerCert(t *testing.T) {
	cfg := newBootstrapTLSConfig(context.Background(), "agent-1", &fakePinReader{})
	err := cfg.VerifyPeerCertificate(nil, nil)
	if !errors.Is(err, errBootstrapNoPeerCert) {
		t.Fatalf("expected errBootstrapNoPeerCert, got: %v", err)
	}
}

// TestBootstrapVerifier_LookupError ensures a non-NotFound storage failure
// fails the handshake (rather than silently TOFU-ing past a broken DB).
func TestBootstrapVerifier_LookupError(t *testing.T) {
	der, _ := newSelfSignedRawDER(t)
	bad := errors.New("synthetic storage unavailable")
	reader := &fakePinReader{getErr: bad}

	cfg := newBootstrapTLSConfig(context.Background(), "agent-1", reader)
	err := cfg.VerifyPeerCertificate([][]byte{der}, nil)
	if err == nil {
		t.Fatalf("storage lookup error must propagate, not silently TOFU")
	}
	if !errors.Is(err, bad) {
		t.Fatalf("expected wrapped storage error, got: %v", err)
	}
}

// TestBootstrapVerifier_NilReader exercises the legacy/test-double path
// where no pin reader is wired — the verifier must default to TOFU rather
// than panicking on a nil interface.
func TestBootstrapVerifier_NilReader(t *testing.T) {
	der, _ := newSelfSignedRawDER(t)
	cfg := newBootstrapTLSConfig(context.Background(), "agent-1", nil)
	if err := cfg.VerifyPeerCertificate([][]byte{der}, nil); err != nil {
		t.Fatalf("nil reader must default to TOFU; got: %v", err)
	}
}
