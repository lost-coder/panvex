package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentstate "github.com/panvex/panvex/internal/agent/state"
)

func TestRunBootstrapCommandSavesIssuedState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("request.Method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/agent/bootstrap" {
			t.Fatalf("request.URL.Path = %q, want %q", r.URL.Path, "/api/agent/bootstrap")
		}
		if r.Header.Get("Authorization") != "Bearer bootstrap-token" {
			t.Fatalf("request.Authorization = %q, want %q", r.Header.Get("Authorization"), "Bearer bootstrap-token")
		}

		var request struct {
			NodeName string `json:"node_name"`
			Version  string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if request.NodeName != "node-a" {
			t.Fatalf("request.NodeName = %q, want %q", request.NodeName, "node-a")
		}
		if request.Version != "1.2.3" {
			t.Fatalf("request.Version = %q, want %q", request.Version, "1.2.3")
		}

		if err := json.NewEncoder(w).Encode(map[string]any{
			"agent_id":         "agent-123",
			"certificate_pem":  "cert-pem",
			"private_key_pem":  "key-pem",
			"ca_pem":           "ca-pem",
			"grpc_endpoint":    "grpc.panel.example.com:443",
			"grpc_server_name": "grpc.panel.example.com",
			"expires_at_unix":  time.Date(2026, time.March, 16, 18, 0, 0, 0, time.UTC).Unix(),
		}); err != nil {
			t.Fatalf("Encode(response) error = %v", err)
		}
	}))
	defer server.Close()

	err := runBootstrapCommand([]string{
		"-panel-url", server.URL,
		"-enrollment-token", "bootstrap-token",
		"-state-file", statePath,
		"-node-name", "node-a",
		"-version", "1.2.3",
	}, server.Client())
	if err != nil {
		t.Fatalf("runBootstrapCommand() error = %v", err)
	}

	credentials, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if credentials.AgentID != "agent-123" {
		t.Fatalf("credentials.AgentID = %q, want %q", credentials.AgentID, "agent-123")
	}
	if credentials.GRPCEndpoint != "grpc.panel.example.com:443" {
		t.Fatalf("credentials.GRPCEndpoint = %q, want %q", credentials.GRPCEndpoint, "grpc.panel.example.com:443")
	}
	if credentials.GRPCServerName != "grpc.panel.example.com" {
		t.Fatalf("credentials.GRPCServerName = %q, want %q", credentials.GRPCServerName, "grpc.panel.example.com")
	}
}

func TestRunBootstrapCommandRejectsExistingStateWithoutForce(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	if err := agentstate.Save(statePath, agentstate.Credentials{
		AgentID:        "agent-existing",
		CertificatePEM: "cert",
		PrivateKeyPEM:  "key",
		CAPEM:          "ca",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	err := runBootstrapCommand([]string{
		"-panel-url", "https://panel.example.com",
		"-enrollment-token", "bootstrap-token",
		"-state-file", statePath,
	}, nil)
	if err == nil {
		t.Fatal("runBootstrapCommand() error = nil, want existing state rejection")
	}
	if !strings.Contains(err.Error(), "-force") {
		t.Fatalf("runBootstrapCommand() error = %q, want mention of -force", err.Error())
	}
}

func TestRunBootstrapCommandAllowsOverwriteWithForce(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	if err := agentstate.Save(statePath, agentstate.Credentials{
		AgentID:        "agent-existing",
		CertificatePEM: "old-cert",
		PrivateKeyPEM:  "old-key",
		CAPEM:          "old-ca",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{
			"agent_id":         "agent-new",
			"certificate_pem":  "new-cert",
			"private_key_pem":  "new-key",
			"ca_pem":           "new-ca",
			"grpc_endpoint":    "panel.example.com:8443",
			"grpc_server_name": "panel.example.com",
			"expires_at_unix":  time.Date(2026, time.March, 16, 19, 0, 0, 0, time.UTC).Unix(),
		}); err != nil {
			t.Fatalf("Encode(response) error = %v", err)
		}
	}))
	defer server.Close()

	err := runBootstrapCommand([]string{
		"-panel-url", server.URL,
		"-enrollment-token", "bootstrap-token",
		"-state-file", statePath,
		"-force",
	}, server.Client())
	if err != nil {
		t.Fatalf("runBootstrapCommand() error = %v", err)
	}

	credentials, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if credentials.AgentID != "agent-new" {
		t.Fatalf("credentials.AgentID = %q, want %q", credentials.AgentID, "agent-new")
	}
}
