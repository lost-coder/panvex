package creds

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
)

func TestLoadRuntimeCredentialsReturnsSavedState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	expected := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "cert-pem",
		PrivateKeyPEM:  "key-pem",
		CAPEM:          "ca-pem",
		GRPCEndpoint:   "grpc.panel.example.com:443",
		GRPCServerName: "grpc.panel.example.com",
	}
	if err := agentstate.Save(statePath, expected); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	credentials, err := LoadCredentials(statePath)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if credentials.AgentID != expected.AgentID {
		t.Fatalf("credentials.AgentID = %q, want %q", credentials.AgentID, expected.AgentID)
	}
	if credentials.GRPCEndpoint != expected.GRPCEndpoint {
		t.Fatalf("credentials.GRPCEndpoint = %q, want %q", credentials.GRPCEndpoint, expected.GRPCEndpoint)
	}
}

func TestLoadRuntimeCredentialsRequiresBootstrapState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "missing-agent-state.json")

	_, err := LoadCredentials(statePath)
	if err == nil {
		t.Fatal("LoadCredentials() error = nil, want bootstrap requirement")
	}
	if !strings.Contains(err.Error(), "bootstrap the agent first") {
		t.Fatalf("LoadCredentials() error = %q, want bootstrap guidance", err.Error())
	}
}

func TestRefreshRuntimeCredentialsIfNeededRenewsAndPersistsExpiringState(t *testing.T) {
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

	// The renewer signs the CSR from the request so the returned cert pairs
	// with the agent-generated key (A9: private keys never leave the agent).
	renewAfter := now.Add(30 * 24 * time.Hour)
	renewer := &fakeCertificateRenewer{
		signCSR: func(csrPEM string) *gatewayrpc.RenewCertificateResponse {
			return &gatewayrpc.RenewCertificateResponse{
				CertificatePem: ca.signCSRForTest(t, csrPEM),
				CaPem:          string(ca.certPEM),
				ExpiresAtUnix:  renewAfter.Unix(),
			}
		},
	}

	updated, err := RefreshIfNeeded(context.Background(), statePath, current, renewer, now)
	if err != nil {
		t.Fatalf("RefreshIfNeeded() error = %v", err)
	}
	if renewer.request == nil {
		t.Fatal("renewer.request = nil, want renewal call")
	}
	if renewer.request.GetAgentId() != current.AgentID {
		t.Fatalf("renewer.request.AgentId = %q, want %q", renewer.request.GetAgentId(), current.AgentID)
	}
	if renewer.request.GetCsrPem() == "" {
		t.Fatal("renewer.request.CsrPem is empty, want a CSR")
	}
	if updated.CertificatePEM == current.CertificatePEM {
		t.Fatal("updated.CertificatePEM is unchanged, want new cert")
	}
	if updated.PrivateKeyPEM == current.PrivateKeyPEM {
		t.Fatal("updated.PrivateKeyPEM is unchanged, want new key")
	}
	// Response must carry NO private key — the agent generated the key locally.
	if renewer.request.GetCsrPem() == "" {
		t.Fatal("no CSR sent in renewal request")
	}

	persisted, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted.CertificatePEM != updated.CertificatePEM {
		t.Fatalf("persisted.CertificatePEM = %q, want %q", persisted.CertificatePEM, updated.CertificatePEM)
	}
}

func TestRefreshRuntimeCredentialsIfNeededSkipsZeroExpiryState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	now := time.Date(2026, time.March, 19, 10, 0, 0, 0, time.UTC)
	current := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "old-cert",
		PrivateKeyPEM:  "old-key",
		CAPEM:          "old-ca",
		GRPCEndpoint:   "panel.example.com:8443",
		GRPCServerName: "panel.example.com",
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	renewer := &fakeCertificateRenewer{
		response: &gatewayrpc.RenewCertificateResponse{
			CertificatePem: "new-cert",
			CaPem:          "new-ca",
			ExpiresAtUnix:  now.Add(30 * 24 * time.Hour).Unix(),
		},
	}

	updated, err := RefreshIfNeeded(context.Background(), statePath, current, renewer, now)
	if err != nil {
		t.Fatalf("RefreshIfNeeded() error = %v", err)
	}
	if renewer.request != nil {
		t.Fatal("renewer.request != nil, want no renewal call")
	}
	if updated != current {
		t.Fatalf("updated = %#v, want %#v", updated, current)
	}

	persisted, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted != current {
		t.Fatalf("persisted = %#v, want %#v", persisted, current)
	}
}

func TestBuildCSRPEMProducesValidCSR(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	csrPEM, err := buildCSRPEM("agent-abc", key)
	if err != nil {
		t.Fatalf("buildCSRPEM: %v", err)
	}

	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil || block.Type != "CERTIFICATE REQUEST" {
		t.Fatalf("expected CERTIFICATE REQUEST PEM block, got %v", block)
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificateRequest: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("CSR signature invalid: %v", err)
	}
	if csr.Subject.CommonName != "agent-abc" {
		t.Fatalf("CSR CN = %q, want %q", csr.Subject.CommonName, "agent-abc")
	}
}

func TestRenewCertificateInStreamSuccessPath(t *testing.T) {
	ca := newTestCA(t)

	// Sign a CSR with the test CA — simulates what the panel would do.
	newKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	csrPEM, err := buildCSRPEM("agent-123", newKey)
	if err != nil {
		t.Fatalf("buildCSRPEM: %v", err)
	}
	signedCertPEM := ca.signCSRForTest(t, csrPEM)
	newKeyDER, err := x509.MarshalECPrivateKey(newKey)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey: %v", err)
	}
	newKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: newKeyDER}))

	statePath := t.TempDir() + "/state.json"
	current := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "old-cert",
		PrivateKeyPEM:  newKeyPEM, // initially same key so we can build the response
		CAPEM:          string(ca.certPEM),
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Simulate the renewal flow: criticalOutbound receives the request;
	// renewalResponses simulates the panel's response.
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	renewalResponses := make(chan *gatewayrpc.RenewalResponse, 1)
	renewalResponses <- &gatewayrpc.RenewalResponse{
		CertificatePem: signedCertPEM,
		CaPem:          string(ca.certPEM),
		ExpiresAtUnix:  time.Now().Add(30 * 24 * time.Hour).Unix(),
	}

	// We test RenewInStream directly, providing the new key
	// via a custom implementation. Since RenewInStream generates
	// its own key, we can't inject the key — instead we test the full
	// observable behaviour: that it reads from criticalOutbound + renewalResponses
	// and persists updated credentials.
	//
	// Use a real CA sign to produce a response that pairs with whatever
	// key RenewInStream generates internally.
	ctx := context.Background()

	// We need the channel-based roundtrip to work: let a goroutine act as
	// the "panel", reading the renewal request and sending back a signed cert.
	go func() {
		msg := <-criticalOutbound
		req := msg.GetRenewalRequest()
		if req == nil {
			return
		}
		signed := ca.signCSRForTest(t, req.GetCsrPem())
		renewalResponses <- &gatewayrpc.RenewalResponse{
			CertificatePem: signed,
			CaPem:          string(ca.certPEM),
			ExpiresAtUnix:  time.Now().Add(30 * 24 * time.Hour).Unix(),
		}
	}()
	// Drain the pre-loaded response so only the goroutine's response counts.
	<-renewalResponses

	updated, err := RenewInStream(ctx, current, statePath, criticalOutbound, renewalResponses)
	if err != nil {
		t.Fatalf("RenewInStream() error = %v", err)
	}
	if updated.CertificatePEM == current.CertificatePEM {
		t.Fatal("updated cert is unchanged, expected new cert PEM")
	}
	if updated.PrivateKeyPEM == current.PrivateKeyPEM {
		t.Fatal("updated private key is unchanged, expected new key PEM")
	}
	if updated.ExpiresAt.IsZero() {
		t.Fatal("updated ExpiresAt is zero")
	}

	// Verify persisted to disk.
	persisted, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.CertificatePEM != updated.CertificatePEM {
		t.Fatal("persisted cert differs from returned cert")
	}
}

func TestRenewCertificateInStreamPanelErrorPath(t *testing.T) {
	statePath := t.TempDir() + "/state.json"
	current := agentstate.Credentials{
		AgentID:   "agent-xyz",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save: %v", err)
	}

	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	renewalResponses := make(chan *gatewayrpc.RenewalResponse, 1)

	go func() {
		<-criticalOutbound
		renewalResponses <- &gatewayrpc.RenewalResponse{
			Error: "CSR signature invalid",
		}
	}()

	_, err := RenewInStream(context.Background(), current, statePath, criticalOutbound, renewalResponses)
	if err == nil {
		t.Fatal("expected error from panel rejection, got nil")
	}
	if !strings.Contains(err.Error(), "panel rejected") {
		t.Fatalf("error = %q, want 'panel rejected' in message", err.Error())
	}
}

func TestRenewCertificateInStreamTimeout(t *testing.T) {
	statePath := t.TempDir() + "/state.json"
	current := agentstate.Credentials{
		AgentID:   "agent-xyz",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save: %v", err)
	}

	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	renewalResponses := make(chan *gatewayrpc.RenewalResponse) // no sender

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	// Drain the outbound so it doesn't block the send.
	go func() { <-criticalOutbound }()

	_, err := RenewInStream(ctx, current, statePath, criticalOutbound, renewalResponses)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

type fakeCertificateRenewer struct {
	request  *gatewayrpc.RenewCertificateRequest
	response *gatewayrpc.RenewCertificateResponse
	err      error
	// signCSR, when non-nil, is called with the CSR from the request and
	// its return value replaces response. Use this to produce a cert that
	// actually pairs with the agent-generated key (A9 unary path).
	signCSR func(csrPEM string) *gatewayrpc.RenewCertificateResponse
}

func (r *fakeCertificateRenewer) RenewCertificate(_ context.Context, request *gatewayrpc.RenewCertificateRequest, _ ...grpc.CallOption) (*gatewayrpc.RenewCertificateResponse, error) {
	r.request = request
	if r.err != nil {
		return nil, r.err
	}
	if r.signCSR != nil {
		return r.signCSR(request.GetCsrPem()), nil
	}
	return r.response, nil
}

// signCSRForTest signs csrPEM with the testCA and returns a cert PEM.
func (ca *testCA) signCSRForTest(t *testing.T, csrPEM string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil {
		t.Fatal("signCSRForTest: invalid PEM")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		t.Fatalf("signCSRForTest: ParseCertificateRequest: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Fatalf("signCSRForTest: CheckSignature: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("signCSRForTest: random serial: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      csr.Subject,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, csr.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("signCSRForTest: CreateCertificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
