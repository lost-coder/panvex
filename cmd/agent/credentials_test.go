package main

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// signCertForCNForTest signs a leaf certificate for csrPEM's public key but
// with an arbitrary CommonName, ignoring whatever CN the CSR itself
// requested. Used to simulate a misrouted/malicious panel renewal response
// that returns a certificate for the WRONG agent identity (3.6).
func (ca *testCA) signCertForCNForTest(t *testing.T, csrPEM, cn string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil {
		t.Fatal("signCertForCNForTest: invalid PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("signCertForCNForTest: ParseCertificateRequest: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("signCertForCNForTest: CheckSignature: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("signCertForCNForTest: random serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, csr.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("signCertForCNForTest: CreateCertificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// TestRefreshRuntimeCredentialsIfNeededRejectsCNMismatch pins the 3.6 fix on
// the unary renewal path: a renewal response whose leaf certificate CN does
// not match the agent's own identity must be rejected, and the existing
// (still-valid-in-memory) credentials must not be replaced or persisted.
func TestRefreshRuntimeCredentialsIfNeededRejectsCNMismatch(t *testing.T) {
	ca := newTestCA(t)
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	now := time.Date(2026, time.March, 19, 10, 0, 0, 0, time.UTC)
	current := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "old-cert",
		PrivateKeyPEM:  "old-key",
		CAPEM:          "old-ca",
		GRPCEndpoint:   "panel.example.com:8443",
		GRPCServerName: "panel.example.com",
		ExpiresAt:      now.Add(30 * time.Minute),
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// The renewer signs the CSR but for a DIFFERENT agent identity than the
	// one that requested it — simulates a misrouted/malicious panel response.
	renewer := &fakeCertificateRenewer{
		signCSR: func(csrPEM string) *gatewayrpc.RenewCertificateResponse {
			return &gatewayrpc.RenewCertificateResponse{
				CertificatePem: ca.signCertForCNForTest(t, csrPEM, "agent-attacker"),
				CaPem:          string(ca.certPEM),
				ExpiresAtUnix:  now.Add(30 * 24 * time.Hour).Unix(),
			}
		},
	}

	updated, err := refreshRuntimeCredentialsIfNeeded(context.Background(), statePath, current, renewer, now)
	if err == nil {
		t.Fatal("refreshRuntimeCredentialsIfNeeded() error = nil, want CN mismatch rejection")
	}
	if !errors.Is(err, errRenewalCNMismatch) {
		t.Fatalf("refreshRuntimeCredentialsIfNeeded() error = %v, want errRenewalCNMismatch", err)
	}
	if updated != current {
		t.Fatalf("updated = %#v, want unchanged %#v", updated, current)
	}

	persisted, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted.CertificatePEM != current.CertificatePEM {
		t.Fatalf("persisted.CertificatePEM = %q, want unchanged %q (mismatched cert must not be persisted)", persisted.CertificatePEM, current.CertificatePEM)
	}
}

// TestRenewCertificateInStreamRejectsCNMismatch pins the 3.6 fix on the
// in-stream renewal path.
func TestRenewCertificateInStreamRejectsCNMismatch(t *testing.T) {
	ca := newTestCA(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	current := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "old-cert",
		PrivateKeyPEM:  "old-key",
		CAPEM:          string(ca.certPEM),
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save: %v", err)
	}

	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	renewalResponses := make(chan *gatewayrpc.RenewalResponse, 1)

	go func() {
		msg := <-criticalOutbound
		req := msg.GetRenewalRequest()
		if req == nil {
			return
		}
		// Sign for a different agent identity than requested.
		signed := ca.signCertForCNForTest(t, req.GetCsrPem(), "agent-attacker")
		renewalResponses <- &gatewayrpc.RenewalResponse{
			CertificatePem: signed,
			CaPem:          string(ca.certPEM),
			ExpiresAtUnix:  time.Now().Add(30 * 24 * time.Hour).Unix(),
		}
	}()

	updated, err := renewCertificateInStream(context.Background(), current, statePath, criticalOutbound, renewalResponses)
	if err == nil {
		t.Fatal("renewCertificateInStream() error = nil, want CN mismatch rejection")
	}
	if !errors.Is(err, errRenewalCNMismatch) {
		t.Fatalf("renewCertificateInStream() error = %v, want errRenewalCNMismatch", err)
	}
	if updated != current {
		t.Fatalf("updated = %#v, want unchanged %#v", updated, current)
	}

	persisted, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.CertificatePEM != current.CertificatePEM {
		t.Fatalf("persisted.CertificatePEM = %q, want unchanged %q (mismatched cert must not be persisted)", persisted.CertificatePEM, current.CertificatePEM)
	}
}
