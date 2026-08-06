package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// newTelemtUpdateStrategyTestServer builds a sqlite-backed server with a
// bootstrapped admin and operator, and returns the server plus each
// user's session cookies (admin satisfies the admin-only strategy
// endpoints; operator is used to prove the role gate rejects it).
func newTelemtUpdateStrategyTestServer(t *testing.T) (srv *Server, adminCookies, operatorCookies []*http.Cookie) {
	t.Helper()
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv = mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(srv.Close)

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}
	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(operator) error = %v", err)
	}

	adminLogin := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("admin login status = %d, want %d", adminLogin.Code, http.StatusOK)
	}
	operatorLogin := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1password",
	}, nil)
	if operatorLogin.Code != http.StatusOK {
		t.Fatalf("operator login status = %d, want %d", operatorLogin.Code, http.StatusOK)
	}

	return srv, adminLogin.Result().Cookies(), operatorLogin.Result().Cookies()
}

// seedLiveAgentWithProbe registers agentID in the live store with the given
// probe (nil is a valid "agent predates the probe" state).
func seedLiveAgentWithProbe(srv *Server, agentID string, probe *TelemtUpdateProbe) {
	srv.live.ApplySnapshot(agentID, Agent{
		ID:                agentID,
		NodeName:          "node-" + agentID,
		TelemtUpdateProbe: probe,
	}, nil)
}

// seedTestAgentRow inserts a persisted agent row for agentID, in the
// server-seeded "default" fleet group. Needed alongside
// seedLiveAgentWithProbe whenever a test's PUT is expected to reach the
// store — agent_update_strategies.agent_id is a foreign key into agents,
// and the live store alone (an in-memory presentation cache) does not
// satisfy it.
func seedTestAgentRow(t *testing.T, srv *Server, agentID string, now time.Time) {
	t.Helper()
	groupID := resolveTestFleetGroupID(t, srv.store, "default")
	if err := srv.store.PutAgent(context.Background(), storage.AgentRecord{
		ID:           agentID,
		NodeName:     "node-" + agentID,
		FleetGroupID: groupID,
		LastSeenAt:   now,
	}); err != nil {
		t.Fatalf("seedTestAgentRow(%q) error = %v", agentID, err)
	}
}

func TestTelemtUpdateStrategyGetUnconfiguredReturnsNullStrategy(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	probe := &TelemtUpdateProbe{
		Mode:                 "binary",
		SuggestedRestartSpec: "systemd:telemt",
		BinaryPath:           "/usr/local/bin/telemt",
		Available:            true,
	}
	seedLiveAgentWithProbe(srv, "agent-1", probe)

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/agent-1/telemt/update-strategy", nil, adminCookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	var got telemtUpdateStrategyResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Strategy != nil {
		t.Errorf("Strategy = %+v, want nil for an unconfigured agent", got.Strategy)
	}
	if got.Probe == nil || got.Probe.Mode != "binary" {
		t.Errorf("Probe = %+v, want the seeded probe", got.Probe)
	}
}

func TestTelemtUpdateStrategyPutThenGetRoundtrips(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	seedLiveAgentWithProbe(srv, "agent-1", nil)
	seedTestAgentRow(t, srv, "agent-1", time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC))

	body := map[string]string{
		"mode":         "binary",
		"restart_spec": "systemd:telemt",
		"binary_path":  "/usr/local/bin/telemt",
		"asset_flavor": "v3",
	}
	putResp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-1/telemt/update-strategy", body, adminCookies)
	if putResp.Code != http.StatusNoContent {
		t.Fatalf("PUT status = %d, want %d (body: %s)", putResp.Code, http.StatusNoContent, putResp.Body.String())
	}

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/agent-1/telemt/update-strategy", nil, adminCookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d (body: %s)", getResp.Code, http.StatusOK, getResp.Body.String())
	}
	var got telemtUpdateStrategyResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Strategy == nil {
		t.Fatal("Strategy = nil, want the strategy just PUT")
	}
	want := telemtUpdateStrategyPayload{
		Mode:        "binary",
		RestartSpec: "systemd:telemt",
		BinaryPath:  "/usr/local/bin/telemt",
		AssetFlavor: "v3",
	}
	if *got.Strategy != want {
		t.Errorf("Strategy = %+v, want %+v", *got.Strategy, want)
	}
}

func TestTelemtUpdateStrategyPutInvalidReturns400(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	seedLiveAgentWithProbe(srv, "agent-1", nil)

	tests := []struct {
		name string
		body map[string]string
	}{
		{
			name: "unknown mode",
			body: map[string]string{"mode": "bogus", "restart_spec": "", "binary_path": "", "asset_flavor": ""},
		},
		{
			name: "binary missing restart_spec",
			body: map[string]string{"mode": "binary", "restart_spec": "", "binary_path": "/usr/local/bin/telemt", "asset_flavor": ""},
		},
		{
			name: "binary relative binary_path",
			body: map[string]string{"mode": "binary", "restart_spec": "systemd:telemt", "binary_path": "telemt", "asset_flavor": ""},
		},
		{
			name: "invalid asset_flavor",
			body: map[string]string{"mode": "none", "restart_spec": "", "binary_path": "", "asset_flavor": "v9"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-1/telemt/update-strategy", tt.body, adminCookies)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("PUT status = %d, want %d (body: %s)", resp.Code, http.StatusBadRequest, resp.Body.String())
			}
		})
	}
}

func TestTelemtUpdateStrategyPutRejectsNonAdmin(t *testing.T) {
	srv, _, operatorCookies := newTelemtUpdateStrategyTestServer(t)
	seedLiveAgentWithProbe(srv, "agent-1", nil)

	body := map[string]string{"mode": "none", "restart_spec": "", "binary_path": "", "asset_flavor": ""}
	resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/agent-1/telemt/update-strategy", body, operatorCookies)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("PUT status = %d, want %d (body: %s)", resp.Code, http.StatusForbidden, resp.Body.String())
	}
}

func TestTelemtUpdateStrategyGetRejectsNonAdmin(t *testing.T) {
	srv, _, operatorCookies := newTelemtUpdateStrategyTestServer(t)
	seedLiveAgentWithProbe(srv, "agent-1", nil)

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/agent-1/telemt/update-strategy", nil, operatorCookies)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("GET status = %d, want %d (body: %s)", resp.Code, http.StatusForbidden, resp.Body.String())
	}
}

func TestTelemtUpdateStrategyUnknownAgentReturns404(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/does-not-exist/telemt/update-strategy", nil, adminCookies)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("GET status = %d, want %d", getResp.Code, http.StatusNotFound)
	}

	body := map[string]string{"mode": "none", "restart_spec": "", "binary_path": "", "asset_flavor": ""}
	putResp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/does-not-exist/telemt/update-strategy", body, adminCookies)
	if putResp.Code != http.StatusNotFound {
		t.Fatalf("PUT status = %d, want %d", putResp.Code, http.StatusNotFound)
	}
}
