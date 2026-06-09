package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// newConfigTargetTestServer builds a sqlite-backed server with a
// bootstrapped admin and returns the server plus the admin's session
// cookies. Admin satisfies the operator role guarding the config-target
// endpoints.
func newConfigTargetTestServer(t *testing.T) (*Server, []*http.Cookie) {
	t.Helper()
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := mustNew(t, Options{
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
		t.Fatalf("BootstrapUser() error = %v", err)
	}
	loginResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.Code, http.StatusOK)
	}
	return srv, loginResp.Result().Cookies()
}

func TestConfigTargetGroupPutThenGet(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "cfg-group", time.Time{})

	body := map[string]any{
		"sections": map[string]any{
			"censorship": map[string]any{"tls_domain": "a"},
		},
	}
	putResp := performJSONRequest(t, srv, http.MethodPut, "/api/fleet-groups/"+groupID+"/config", body, cookies)
	if putResp.Code != http.StatusOK {
		t.Fatalf("PUT group config status = %d, want %d (body: %s)", putResp.Code, http.StatusOK, putResp.Body.String())
	}

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupID+"/config", nil, cookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET group config status = %d, want %d", getResp.Code, http.StatusOK)
	}
	var got groupConfigTargetResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode group config response: %v", err)
	}
	if tlsDomain := nestedString(got.Sections, "censorship", "tls_domain"); tlsDomain != "a" {
		t.Fatalf("group sections.censorship.tls_domain = %q, want %q", tlsDomain, "a")
	}
}

func TestConfigTargetGroupPutRejectsNonEditableSection(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "cfg-group-bad", time.Time{})

	body := map[string]any{
		"sections": map[string]any{
			"server": map[string]any{"port": 1},
		},
	}
	resp := performJSONRequest(t, srv, http.MethodPut, "/api/fleet-groups/"+groupID+"/config", body, cookies)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("PUT non-editable section status = %d, want %d (body: %s)", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

func TestConfigTargetAgentPutThenGet(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	const agentID = "agent-override-1"

	body := map[string]any{
		"sections": map[string]any{
			"censorship": map[string]any{"tls_domain": "override.example"},
		},
	}
	putResp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/"+agentID+"/config", body, cookies)
	if putResp.Code != http.StatusOK {
		t.Fatalf("PUT agent config status = %d, want %d (body: %s)", putResp.Code, http.StatusOK, putResp.Body.String())
	}

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/"+agentID+"/config", nil, cookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET agent config status = %d, want %d", getResp.Code, http.StatusOK)
	}
	var got agentConfigTargetResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode agent config response: %v", err)
	}
	if tlsDomain := nestedString(got.Override, "censorship", "tls_domain"); tlsDomain != "override.example" {
		t.Fatalf("agent override.censorship.tls_domain = %q, want %q", tlsDomain, "override.example")
	}
}

// TestConfigTargetAgentEffectiveMergePrefersOverride seeds an agent that
// belongs to a fleet group with a group-level target, then writes an
// agent override and asserts the override wins in the effective merge.
func TestConfigTargetAgentEffectiveMergePrefersOverride(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "cfg-merge-group", time.Time{})
	const agentID = "agent-merge-1"

	// Seed the agent-in-group into the live snapshot so the GET handler
	// can resolve the agent's fleet group id.
	srv.live.ApplySnapshot(agentID, Agent{ID: agentID, NodeName: "node-merge", FleetGroupID: groupID}, nil)

	groupBody := map[string]any{
		"sections": map[string]any{
			"censorship": map[string]any{"tls_domain": "group.example"},
		},
	}
	if resp := performJSONRequest(t, srv, http.MethodPut, "/api/fleet-groups/"+groupID+"/config", groupBody, cookies); resp.Code != http.StatusOK {
		t.Fatalf("PUT group config status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	agentBody := map[string]any{
		"sections": map[string]any{
			"censorship": map[string]any{"tls_domain": "agent.example"},
		},
	}
	if resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/"+agentID+"/config", agentBody, cookies); resp.Code != http.StatusOK {
		t.Fatalf("PUT agent config status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/"+agentID+"/config", nil, cookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET agent config status = %d, want %d", getResp.Code, http.StatusOK)
	}
	var got agentConfigTargetResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode agent config response: %v", err)
	}
	if tlsDomain := nestedString(got.Effective, "censorship", "tls_domain"); tlsDomain != "agent.example" {
		t.Fatalf("effective.censorship.tls_domain = %q, want %q (override should win)", tlsDomain, "agent.example")
	}
}

// nestedString reads m[section][key] as a string, returning "" when any
// step is absent or not the expected type.
func nestedString(m map[string]any, section, key string) string {
	sub, ok := m[section].(map[string]any)
	if !ok {
		return ""
	}
	v, _ := sub[key].(string)
	return v
}
