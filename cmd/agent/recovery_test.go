package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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

		if err := json.NewEncoder(w).Encode(map[string]any{
			"agent_id":         current.AgentID,
			"certificate_pem":  "new-cert",
			"private_key_pem":  "new-key",
			"ca_pem":           "new-ca",
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
	if updated.CertificatePEM != "new-cert" {
		t.Fatalf("updated.CertificatePEM = %q, want %q", updated.CertificatePEM, "new-cert")
	}
	if updated.PrivateKeyPEM != "new-key" {
		t.Fatalf("updated.PrivateKeyPEM = %q, want %q", updated.PrivateKeyPEM, "new-key")
	}
	if updated.PanelURL != current.PanelURL {
		t.Fatalf("updated.PanelURL = %q, want %q", updated.PanelURL, current.PanelURL)
	}

	persisted, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted.CertificatePEM != "new-cert" {
		t.Fatalf("persisted.CertificatePEM = %q, want %q", persisted.CertificatePEM, "new-cert")
	}
	if persisted.PanelURL != current.PanelURL {
		t.Fatalf("persisted.PanelURL = %q, want %q", persisted.PanelURL, current.PanelURL)
	}
}

func TestRenewRuntimeCredentialsIfNeededUsesHTTPRecoveryWhenCertificateAlreadyExpired(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	now := time.Date(2026, time.March, 28, 14, 30, 0, 0, time.UTC)
	privateKeyPEM := generateRecoveryPrivateKeyPEMForTest(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/recover-certificate" {
			t.Fatalf("request.URL.Path = %q, want %q", r.URL.Path, "/api/agent/recover-certificate")
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"agent_id":         "agent-123",
			"certificate_pem":  "recovered-cert",
			"private_key_pem":  "recovered-key",
			"ca_pem":           "recovered-ca",
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
	if updated.CertificatePEM != "recovered-cert" {
		t.Fatalf("updated.CertificatePEM = %q, want %q", updated.CertificatePEM, "recovered-cert")
	}
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
