package server

import (
	"context"
	"crypto/rand"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

func TestHTTPAgentCertificateRecoveryRejectsWithoutActiveGrant(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()
	seedRecoveryTestAgent(t, server, store, now)

	request := newAgentCertificateRecoveryRequestForTest(t, server, "agent-1", now)
	response := performJSONRequestWithHeaders(
		t,
		server.Handler(),
		http.MethodPost,
		"https://panel.example.com/api/agent/recover-certificate",
		request,
		nil,
		nil,
	)
	if response.Code != http.StatusForbidden {
		t.Fatalf("POST /api/agent/recover-certificate without grant status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestHTTPAgentCertificateRecoveryConsumesAdminGrant(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 10, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()
	seedRecoveryTestAgent(t, server, store, now)
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	createResponse := performJSONRequest(
		t,
		server.Handler(),
		http.MethodPost,
		"/api/agents/agent-1/certificate-recovery-grants",
		map[string]any{
			"ttl_seconds": 900,
		},
		cookies,
	)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/agents/{id}/certificate-recovery-grants status = %d, want %d", createResponse.Code, http.StatusCreated)
	}

	request := newAgentCertificateRecoveryRequestForTest(t, server, "agent-1", now)
	recoveryResponse := performJSONRequestWithHeaders(
		t,
		server.Handler(),
		http.MethodPost,
		"https://panel.example.com/api/agent/recover-certificate",
		request,
		nil,
		nil,
	)
	if recoveryResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/agent/recover-certificate with grant status = %d, want %d, body = %s", recoveryResponse.Code, http.StatusOK, recoveryResponse.Body.String())
	}

	reuseResponse := performJSONRequestWithHeaders(
		t,
		server.Handler(),
		http.MethodPost,
		"https://panel.example.com/api/agent/recover-certificate",
		request,
		nil,
		nil,
	)
	if reuseResponse.Code != http.StatusForbidden {
		t.Fatalf("POST /api/agent/recover-certificate after grant use status = %d, want %d", reuseResponse.Code, http.StatusForbidden)
	}
}

func newAgentCertificateRecoveryRequestForTest(t *testing.T, server *Server, agentID string, observedAt time.Time) map[string]any {
	t.Helper()

	issued, err := server.authority.issueClientCertificate(agentID, observedAt.Add(-time.Hour))
	if err != nil {
		t.Fatalf("issueClientCertificate() error = %v", err)
	}
	certificate, err := parseRecoveryCertificate(issued.CertificatePEM)
	if err != nil {
		t.Fatalf("parseRecoveryCertificate() error = %v", err)
	}
	if err := verifyRecoveryCertificate(certificate, agentID, server.authority.certificate, observedAt); err != nil {
		t.Fatalf("verifyRecoveryCertificate() error = %v", err)
	}

	privateKey := parseRecoveryPrivateKeyForTest(t, issued.PrivateKeyPEM)
	proofTimestampUnix := observedAt.Unix()
	proofNonce := "recovery-nonce-123"
	payload := recoveryProofPayload(agentID, proofTimestampUnix, proofNonce)
	digest := sha256.Sum256([]byte(payload))
	signature, err := ecdsa.SignASN1(rand.Reader, privateKey, digest[:])
	if err != nil {
		t.Fatalf("SignASN1() error = %v", err)
	}

	return map[string]any{
		"agent_id":             agentID,
		"certificate_pem":      issued.CertificatePEM,
		"proof_timestamp_unix": proofTimestampUnix,
		"proof_nonce":          proofNonce,
		"proof_signature":      base64.RawURLEncoding.EncodeToString(signature),
	}
}

func parseRecoveryPrivateKeyForTest(t *testing.T, privateKeyPEM string) *ecdsa.PrivateKey {
	t.Helper()

	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		t.Fatal("pem.Decode(private key) = nil, want key block")
	}

	privateKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParseECPrivateKey() error = %v", err)
	}

	return privateKey
}

func seedRecoveryTestAgent(t *testing.T, server *Server, store *sqlite.Store, now time.Time) {
	t.Helper()

	if err := store.PutFleetGroup(context.Background(), storage.FleetGroupRecord{
		ID:        "ams",
		Name:      "Amsterdam",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}

	record := storage.AgentRecord{
		ID:           "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams",
		Version:      "1.0.0",
		LastSeenAt:   now,
	}
	if err := store.PutAgent(context.Background(), record); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}

	server.agents[record.ID] = Agent{
		ID:           record.ID,
		NodeName:     record.NodeName,
		FleetGroupID: record.FleetGroupID,
		Version:      record.Version,
		LastSeenAt:   record.LastSeenAt,
	}
}

func TestHTTPAgentCertificateRecoveryGrantCreateResponseRoundTrip(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 20, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()
	seedRecoveryTestAgent(t, server, store, now)
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	createResponse := performJSONRequest(
		t,
		server.Handler(),
		http.MethodPost,
		"/api/agents/agent-1/certificate-recovery-grants",
		map[string]any{
			"ttl_seconds": 900,
		},
		loginResponse.Result().Cookies(),
	)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/agents/{id}/certificate-recovery-grants status = %d, want %d", createResponse.Code, http.StatusCreated)
	}

	var payload struct {
		AgentID       string `json:"agent_id"`
		Status        string `json:"status"`
		IssuedAtUnix  int64  `json:"issued_at_unix"`
		ExpiresAtUnix int64  `json:"expires_at_unix"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(createResponse) error = %v", err)
	}
	if payload.AgentID != "agent-1" {
		t.Fatalf("payload.AgentID = %q, want %q", payload.AgentID, "agent-1")
	}
	if payload.Status != "allowed" {
		t.Fatalf("payload.Status = %q, want %q", payload.Status, "allowed")
	}
	if payload.IssuedAtUnix != now.Unix() {
		t.Fatalf("payload.IssuedAtUnix = %d, want %d", payload.IssuedAtUnix, now.Unix())
	}
	if payload.ExpiresAtUnix != now.Add(15*time.Minute).Unix() {
		t.Fatalf("payload.ExpiresAtUnix = %d, want %d", payload.ExpiresAtUnix, now.Add(15*time.Minute).Unix())
	}
}

func TestHTTPAgentCertificateRecoveryGrantRejectsExcessiveTTL(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 25, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()
	seedRecoveryTestAgent(t, server, store, now)
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	createResponse := performJSONRequest(
		t,
		server.Handler(),
		http.MethodPost,
		"/api/agents/agent-1/certificate-recovery-grants",
		map[string]any{
			"ttl_seconds": 86400,
		},
		loginResponse.Result().Cookies(),
	)
	if createResponse.Code != http.StatusBadRequest {
		t.Fatalf("POST /api/agents/{id}/certificate-recovery-grants excessive ttl status = %d, want %d", createResponse.Code, http.StatusBadRequest)
	}
}

func TestHTTPAgentsExposeCertificateRecoveryStatus(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 40, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()
	seedRecoveryTestAgent(t, server, store, now)
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	createResponse := performJSONRequest(
		t,
		server.Handler(),
		http.MethodPost,
		"/api/agents/agent-1/certificate-recovery-grants",
		map[string]any{
			"ttl_seconds": 900,
		},
		loginResponse.Result().Cookies(),
	)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/agents/{id}/certificate-recovery-grants status = %d, want %d", createResponse.Code, http.StatusCreated)
	}

	agentsResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/agents", nil, loginResponse.Result().Cookies())
	if agentsResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/agents status = %d, want %d", agentsResponse.Code, http.StatusOK)
	}

	var payload []struct {
		ID                  string `json:"id"`
		CertificateRecovery struct {
			Status        string `json:"status"`
			ExpiresAtUnix int64  `json:"expires_at_unix"`
		} `json:"certificate_recovery"`
	}
	if err := json.Unmarshal(agentsResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal(agentsResponse) error = %v", err)
	}
	if len(payload) != 1 {
		t.Fatalf("len(payload) = %d, want %d", len(payload), 1)
	}
	if payload[0].CertificateRecovery.Status != "allowed" {
		t.Fatalf("payload[0].CertificateRecovery.Status = %q, want %q", payload[0].CertificateRecovery.Status, "allowed")
	}
	if payload[0].CertificateRecovery.ExpiresAtUnix != now.Add(15*time.Minute).Unix() {
		t.Fatalf("payload[0].CertificateRecovery.ExpiresAtUnix = %d, want %d", payload[0].CertificateRecovery.ExpiresAtUnix, now.Add(15*time.Minute).Unix())
	}
}
