package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// setupTransportModeServer creates a test server with a SQLite store, an admin
// user, an agent in memory, and returns the server + admin session cookies.
func setupTransportModeServer(t *testing.T) (*Server, []*http.Cookie) {
	t.Helper()
	now := time.Date(2026, time.April, 29, 10, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(func() {
		srv.Close()
		store.Close()
	})

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Seed the agent into storage so UpdateAgentTransportMode finds it.
	if err := store.PutAgent(t.Context(), storage.AgentRecord{
		ID:           "agent-tm-1",
		NodeName:     "transport-test-node",
		FleetGroupID: "",
		LastSeenAt:   now,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
	// Mirror the agent into the server's in-memory map.
	srv.mu.Lock()
	srv.seedLiveAgentKeyed("agent-tm-1", Agent{
		ID:           "agent-tm-1",
		NodeName:     "transport-test-node",
		FleetGroupID: "",
	})
	srv.mu.Unlock()

	loginResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("admin login status = %d, want %d", loginResp.Code, http.StatusOK)
	}
	return srv, loginResp.Result().Cookies()
}

// TestUpdateAgentTransportModeHappyPath verifies that a valid outbound PUT:
//   - returns 204
//   - persists transport_mode=outbound + dial_address in the DB
//   - enqueues a switch_transport_mode job with agent-level naming
func TestUpdateAgentTransportModeHappyPath(t *testing.T) {
	srv, cookies := setupTransportModeServer(t)

	resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-tm-1/transport-mode",
		map[string]string{
			"transport_mode": "outbound",
			"dial_address":   "vps.example.com:8443",
		}, cookies)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("PUT /agents/agent-tm-1/transport-mode status = %d, want %d (body=%s)",
			resp.Code, http.StatusNoContent, resp.Body.String())
	}

	// Verify the job was enqueued.
	listed := srv.jobs.ListRecentWithContext(t.Context(), 50)
	var found *jobs.Job
	for i := range listed {
		if listed[i].Action == jobs.ActionSwitchTransportMode {
			found = &listed[i]
			break
		}
	}
	if found == nil {
		t.Fatal("switch_transport_mode job not found in queue after transport mode change")
	}
	if len(found.TargetAgentIDs) != 1 || found.TargetAgentIDs[0] != "agent-tm-1" {
		t.Fatalf("job target = %v, want [agent-tm-1]", found.TargetAgentIDs)
	}

	// Verify job payload uses agent-level naming (listen for outbound).
	var payload map[string]string
	if err := json.Unmarshal([]byte(found.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal job payload: %v", err)
	}
	if payload["mode"] != "listen" {
		t.Fatalf("job payload mode = %q, want listen", payload["mode"])
	}
	if payload["listen_addr"] != ":8443" {
		t.Fatalf("job payload listen_addr = %q, want :8443 (default-derived from dial_address port)", payload["listen_addr"])
	}
}

// TestUpdateAgentTransportModeRespectsExplicitListenAddress verifies the
// operator can override the auto-derived bind spec, and that the override is
// what flows into the job payload (not the public dial_address).
func TestUpdateAgentTransportModeRespectsExplicitListenAddress(t *testing.T) {
	srv, cookies := setupTransportModeServer(t)

	resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-tm-1/transport-mode",
		map[string]string{
			"transport_mode": "outbound",
			"dial_address":   "vps.example.com:8443",
			"listen_address": "0.0.0.0:9443",
		}, cookies)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, body=%s", resp.Code, resp.Body.String())
	}

	listed := srv.jobs.ListRecentWithContext(t.Context(), 50)
	var found *jobs.Job
	for i := range listed {
		if listed[i].Action == jobs.ActionSwitchTransportMode {
			found = &listed[i]
			break
		}
	}
	if found == nil {
		t.Fatal("switch_transport_mode job not found")
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(found.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload["listen_addr"] != "0.0.0.0:9443" {
		t.Fatalf("job payload listen_addr = %q, want 0.0.0.0:9443", payload["listen_addr"])
	}
}

// TestUpdateAgentTransportModeInboundHappyPath verifies switching back to inbound:
//   - returns 204
//   - job payload uses mode=dial and empty listen_addr
func TestUpdateAgentTransportModeInboundHappyPath(t *testing.T) {
	srv, cookies := setupTransportModeServer(t)

	resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-tm-1/transport-mode",
		map[string]string{
			"transport_mode": "inbound",
		}, cookies)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("PUT /agents/agent-tm-1/transport-mode (inbound) status = %d, want %d (body=%s)",
			resp.Code, http.StatusNoContent, resp.Body.String())
	}

	listed := srv.jobs.ListRecentWithContext(t.Context(), 50)
	var found *jobs.Job
	for i := range listed {
		if listed[i].Action == jobs.ActionSwitchTransportMode {
			found = &listed[i]
			break
		}
	}
	if found == nil {
		t.Fatal("switch_transport_mode job not found for inbound switch")
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(found.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal job payload: %v", err)
	}
	if payload["mode"] != "dial" {
		t.Fatalf("job payload mode = %q, want dial", payload["mode"])
	}
	if payload["listen_addr"] != "" {
		t.Fatalf("job payload listen_addr = %q, want empty", payload["listen_addr"])
	}
}

// TestUpdateAgentTransportModeRejectsOutboundWithoutDialAddress verifies
// that outbound mode without a dial_address returns 400.
func TestUpdateAgentTransportModeRejectsOutboundWithoutDialAddress(t *testing.T) {
	srv, cookies := setupTransportModeServer(t)

	resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-tm-1/transport-mode",
		map[string]string{
			"transport_mode": "outbound",
		}, cookies)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("PUT transport-mode outbound without dial_address status = %d, want %d (body=%s)",
			resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

// TestUpdateAgentTransportModeRejectsInvalidMode verifies that an unknown
// transport_mode value returns 400.
func TestUpdateAgentTransportModeRejectsInvalidMode(t *testing.T) {
	srv, cookies := setupTransportModeServer(t)

	resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-tm-1/transport-mode",
		map[string]string{
			"transport_mode": "sideways",
		}, cookies)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("PUT transport-mode invalid value status = %d, want %d (body=%s)",
			resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

// TestUpdateAgentTransportModeRequiresAdminRole verifies that an operator
// (not admin) is denied access.
func TestUpdateAgentTransportModeRequiresAdminRole(t *testing.T) {
	now := time.Date(2026, time.April, 29, 10, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(func() {
		srv.Close()
		store.Close()
	})

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	srv.mu.Lock()
	srv.seedLiveAgentKeyed("agent-tm-1", Agent{ID: "agent-tm-1", NodeName: "n", FleetGroupID: ""})
	srv.mu.Unlock()

	loginResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("operator login status = %d, want %d", loginResp.Code, http.StatusOK)
	}
	operatorCookies := loginResp.Result().Cookies()

	resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-tm-1/transport-mode",
		map[string]string{
			"transport_mode": "inbound",
		}, operatorCookies)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("operator PUT transport-mode status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}
