package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	agentstate "github.com/lost-coder/panvex/internal/agent/state"
)

func TestRecoverRuntimeCredentialsIfNeededRecoversAndPersistsExpiredState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	now := time.Date(2026, time.March, 28, 14, 0, 0, 0, time.UTC)
	privateKeyPEM := generateRecoveryPrivateKeyPEMForTest(t)
	ca := newTestCA(t)
	current := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "expired-cert-pem",
		PrivateKeyPEM:  privateKeyPEM,
		CAPEM:          "old-ca",
		PanelURL:       "http://panel.example.com",
		GRPCEndpoint:   "panel.example.com:8443",
		GRPCServerName: "panel.example.com",
		ExpiresAt:      now.Add(-time.Minute),
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	var issuedCertPEM string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("request.Method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/agent/recover-certificate" {
			t.Fatalf("request.URL.Path = %q, want %q", r.URL.Path, "/api/agent/recover-certificate")
		}

		var request struct {
			AgentID            string `json:"agent_id"`
			CertificatePEM     string `json:"certificate_pem"`
			ProofTimestampUnix int64  `json:"proof_timestamp_unix"`
			ProofNonce         string `json:"proof_nonce"`
			ProofSignature     string `json:"proof_signature"`
			CSRPEM             string `json:"csr_pem"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if request.AgentID != current.AgentID {
			t.Fatalf("request.AgentID = %q, want %q", request.AgentID, current.AgentID)
		}
		if request.CertificatePEM != current.CertificatePEM {
			t.Fatalf("request.CertificatePEM = %q, want %q", request.CertificatePEM, current.CertificatePEM)
		}
		if request.ProofTimestampUnix != now.Unix() {
			t.Fatalf("request.ProofTimestampUnix = %d, want %d", request.ProofTimestampUnix, now.Unix())
		}
		if request.ProofNonce == "" {
			t.Fatal("request.ProofNonce = empty, want proof nonce")
		}
		if request.ProofSignature == "" {
			t.Fatal("request.ProofSignature = empty, want proof signature")
		}
		if request.CSRPEM == "" {
			t.Fatal("request.CSRPEM = empty, want csr_pem")
		}

		// A9: sign the CSR and return a real cert — no private key in response.
		issuedCertPEM = ca.signCSRForTest(t, request.CSRPEM)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"agent_id":         current.AgentID,
			"certificate_pem":  issuedCertPEM,
			"ca_pem":           string(ca.certPEM),
			"grpc_endpoint":    "grpc.panel.example.com:443",
			"grpc_server_name": "grpc.panel.example.com",
			"expires_at_unix":  now.Add(30 * 24 * time.Hour).Unix(),
		}); err != nil {
			t.Fatalf("Encode(response) error = %v", err)
		}
	}))
	defer server.Close()

	current.PanelURL = server.URL
	updated, err := recoverRuntimeCredentialsIfNeeded(context.Background(), statePath, current, nil, now)
	if err != nil {
		t.Fatalf("recoverRuntimeCredentialsIfNeeded() error = %v", err)
	}
	if updated.CertificatePEM != issuedCertPEM {
		t.Fatalf("updated.CertificatePEM = %q, want issued cert", updated.CertificatePEM)
	}
	// A9: private key must be locally generated — validate it pairs with the cert.
	if _, err := tls.X509KeyPair([]byte(updated.CertificatePEM), []byte(updated.PrivateKeyPEM)); err != nil {
		t.Fatalf("updated cert/key do not pair: %v", err)
	}
	if updated.PanelURL != current.PanelURL {
		t.Fatalf("updated.PanelURL = %q, want %q", updated.PanelURL, current.PanelURL)
	}

	persisted, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted.CertificatePEM != issuedCertPEM {
		t.Fatalf("persisted.CertificatePEM = %q, want issued cert", persisted.CertificatePEM)
	}
	if persisted.PanelURL != current.PanelURL {
		t.Fatalf("persisted.PanelURL = %q, want %q", persisted.PanelURL, current.PanelURL)
	}
}

func TestRenewRuntimeCredentialsIfNeededUsesHTTPRecoveryWhenCertificateAlreadyExpired(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	now := time.Date(2026, time.March, 28, 14, 30, 0, 0, time.UTC)
	privateKeyPEM := generateRecoveryPrivateKeyPEMForTest(t)
	ca := newTestCA(t)

	var issuedCertPEM string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/recover-certificate" {
			t.Fatalf("request.URL.Path = %q, want %q", r.URL.Path, "/api/agent/recover-certificate")
		}
		var request struct {
			CSRPEM string `json:"csr_pem"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if request.CSRPEM == "" {
			t.Fatal("request.CSRPEM = empty, want csr_pem")
		}
		// A9: sign the CSR and return a real cert — no private key in response.
		issuedCertPEM = ca.signCSRForTest(t, request.CSRPEM)
		if err := json.NewEncoder(w).Encode(map[string]any{
			"agent_id":         "agent-123",
			"certificate_pem":  issuedCertPEM,
			"ca_pem":           string(ca.certPEM),
			"grpc_endpoint":    "grpc.panel.example.com:443",
			"grpc_server_name": "grpc.panel.example.com",
			"expires_at_unix":  now.Add(30 * 24 * time.Hour).Unix(),
		}); err != nil {
			t.Fatalf("Encode(response) error = %v", err)
		}
	}))
	defer server.Close()

	current := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "not-a-valid-x509-certificate",
		PrivateKeyPEM:  privateKeyPEM,
		CAPEM:          "old-ca",
		PanelURL:       server.URL,
		GRPCEndpoint:   "panel.example.com:8443",
		GRPCServerName: "panel.example.com",
		ExpiresAt:      now.Add(-time.Minute),
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	updated, err := renewRuntimeCredentialsIfNeeded(context.Background(), statePath, "127.0.0.1:1", "panel.example.com", current, now)
	if err != nil {
		t.Fatalf("renewRuntimeCredentialsIfNeeded() error = %v", err)
	}
	if updated.CertificatePEM != issuedCertPEM {
		t.Fatalf("updated.CertificatePEM = %q, want issued cert", updated.CertificatePEM)
	}
}

func TestRecoverListenCredentialsIfExpiredCallsRecovery(t *testing.T) {
	privateKeyPEM := generateRecoveryPrivateKeyPEMForTest(t)
	now := time.Date(2026, time.March, 28, 14, 0, 0, 0, time.UTC)
	ca := newTestCA(t)

	// Case 1: expired cert — stub must be hit exactly once.
	t.Run("expired", func(t *testing.T) {
		statePath := filepath.Join(t.TempDir(), "agent-state.json")
		current := agentstate.Credentials{
			AgentID:        "agent-456",
			CertificatePEM: "expired-cert-pem",
			PrivateKeyPEM:  privateKeyPEM,
			CAPEM:          "old-ca",
			PanelURL:       "http://placeholder",
			GRPCEndpoint:   "panel.example.com:8443",
			GRPCServerName: "panel.example.com",
			ExpiresAt:      now.Add(-time.Hour),
			TransportMode:  "listen",
		}
		if err := agentstate.Save(statePath, current); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		hits := 0
		var issuedCertPEM string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits++
			if r.URL.Path != "/api/agent/recover-certificate" {
				t.Fatalf("request.URL.Path = %q, want %q", r.URL.Path, "/api/agent/recover-certificate")
			}
			var request struct {
				CSRPEM string `json:"csr_pem"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("Decode(request) error = %v", err)
			}
			if request.CSRPEM == "" {
				t.Fatal("request.CSRPEM = empty, want csr_pem")
			}
			// A9: sign the CSR and return a real cert — no private key in response.
			issuedCertPEM = ca.signCSRForTest(t, request.CSRPEM)
			if err := json.NewEncoder(w).Encode(map[string]any{
				"agent_id":         current.AgentID,
				"certificate_pem":  issuedCertPEM,
				"ca_pem":           string(ca.certPEM),
				"grpc_endpoint":    "grpc.panel.example.com:443",
				"grpc_server_name": "grpc.panel.example.com",
				"expires_at_unix":  now.Add(30 * 24 * time.Hour).Unix(),
			}); err != nil {
				t.Fatalf("Encode(response) error = %v", err)
			}
		}))
		defer server.Close()

		current.PanelURL = server.URL
		updated, err := recoverListenCredentialsIfExpired(context.Background(), statePath, current, nil, now)
		if err != nil {
			t.Fatalf("recoverListenCredentialsIfExpired() error = %v", err)
		}
		if hits != 1 {
			t.Fatalf("stub hit %d times, want 1", hits)
		}
		if updated.CertificatePEM != issuedCertPEM {
			t.Fatalf("updated.CertificatePEM = %q, want issued cert", updated.CertificatePEM)
		}
		if updated.ExpiresAt.Before(now) {
			t.Fatalf("updated.ExpiresAt = %v, want future", updated.ExpiresAt)
		}

		persisted, err := agentstate.Load(statePath)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if persisted.CertificatePEM != issuedCertPEM {
			t.Fatalf("persisted.CertificatePEM = %q, want issued cert", persisted.CertificatePEM)
		}
	})

	// Case 2: cert still valid — stub must NOT be hit, credentials unchanged.
	t.Run("valid", func(t *testing.T) {
		statePath := filepath.Join(t.TempDir(), "agent-state.json")
		current := agentstate.Credentials{
			AgentID:        "agent-456",
			CertificatePEM: "valid-cert-pem",
			PrivateKeyPEM:  privateKeyPEM,
			CAPEM:          "ca",
			PanelURL:       "http://placeholder",
			GRPCEndpoint:   "panel.example.com:8443",
			GRPCServerName: "panel.example.com",
			ExpiresAt:      now.Add(24 * time.Hour),
			TransportMode:  "listen",
		}
		if err := agentstate.Save(statePath, current); err != nil {
			t.Fatalf("Save() error = %v", err)
		}

		hits := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits++
			t.Fatalf("stub unexpectedly called for valid cert")
		}))
		defer server.Close()

		current.PanelURL = server.URL
		updated, err := recoverListenCredentialsIfExpired(context.Background(), statePath, current, nil, now)
		if err != nil {
			t.Fatalf("recoverListenCredentialsIfExpired() error = %v", err)
		}
		if hits != 0 {
			t.Fatalf("stub hit %d times, want 0", hits)
		}
		if updated.CertificatePEM != current.CertificatePEM {
			t.Fatalf("updated.CertificatePEM = %q, want %q (unchanged)", updated.CertificatePEM, current.CertificatePEM)
		}
	})
}

func generateRecoveryPrivateKeyPEMForTest(t *testing.T) string {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	privateKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}

	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privateKeyDER,
	}))
}
