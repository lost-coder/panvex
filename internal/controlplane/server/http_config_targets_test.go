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

	// Seed the agent into the live snapshot so the scope-checked handlers
	// resolve it (admin scope is global, so any fleet group passes).
	srv.live.ApplySnapshot(agentID, Agent{ID: agentID, NodeName: "node-override"}, nil)

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

// loginScopedOperator bootstraps a non-admin operator whose fleet scope
// is restricted to allowedGroupIDs, logs them in, and returns their
// session cookies. With explicit scope rows the operator is no longer
// global, so resolveFleetScope yields a narrow FleetScopeAccess and the
// scope-checked config-target handlers enforce IsAllowed.
func loginScopedOperator(t *testing.T, srv *Server, username string, allowedGroupIDs []string) []*http.Cookie {
	t.Helper()
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	user, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: username,
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser(operator) error = %v", err)
	}
	if err := srv.store.SetUserFleetGroupScopes(context.Background(), user.ID, allowedGroupIDs, "admin", now); err != nil {
		t.Fatalf("SetUserFleetGroupScopes() error = %v", err)
	}
	loginResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": username,
		"password": "Operator1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("operator login status = %d, want %d", loginResp.Code, http.StatusOK)
	}
	return loginResp.Result().Cookies()
}

// TestConfigTargetGroupDeniesOutOfScopeOperator asserts a fleet-scoped
// operator whose scope excludes the target group gets the same 404 the
// sibling /fleet-groups/{id} endpoints return — for both GET and PUT.
func TestConfigTargetGroupDeniesOutOfScopeOperator(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	inScope := seedTestFleetGroup(t, srv.store, "cfg-scope-in", time.Time{})
	outOfScope := seedTestFleetGroup(t, srv.store, "cfg-scope-out", time.Time{})
	cookies := loginScopedOperator(t, srv, "scoped-op-group", []string{inScope})

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+outOfScope+"/config", nil, cookies)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("GET out-of-scope group config status = %d, want %d (body: %s)", getResp.Code, http.StatusNotFound, getResp.Body.String())
	}

	body := map[string]any{
		"sections": map[string]any{
			"censorship": map[string]any{"tls_domain": "x"},
		},
	}
	putResp := performJSONRequest(t, srv, http.MethodPut, "/api/fleet-groups/"+outOfScope+"/config", body, cookies)
	if putResp.Code != http.StatusNotFound {
		t.Fatalf("PUT out-of-scope group config status = %d, want %d (body: %s)", putResp.Code, http.StatusNotFound, putResp.Body.String())
	}

	// The in-scope group remains accessible to the same operator.
	if resp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+inScope+"/config", nil, cookies); resp.Code != http.StatusOK {
		t.Fatalf("GET in-scope group config status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}
}

// TestConfigTargetAgentDeniesOutOfScopeOperator asserts a fleet-scoped
// operator whose scope excludes the agent's fleet group gets the agent
// not-found response for both GET and PUT.
func TestConfigTargetAgentDeniesOutOfScopeOperator(t *testing.T) {
	srv, _ := newConfigTargetTestServer(t)
	inScope := seedTestFleetGroup(t, srv.store, "cfg-agent-scope-in", time.Time{})
	outOfScope := seedTestFleetGroup(t, srv.store, "cfg-agent-scope-out", time.Time{})
	const agentID = "agent-out-of-scope-1"
	srv.live.ApplySnapshot(agentID, Agent{ID: agentID, NodeName: "node-oos", FleetGroupID: outOfScope}, nil)
	cookies := loginScopedOperator(t, srv, "scoped-op-agent", []string{inScope})

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/"+agentID+"/config", nil, cookies)
	if getResp.Code != http.StatusNotFound {
		t.Fatalf("GET out-of-scope agent config status = %d, want %d (body: %s)", getResp.Code, http.StatusNotFound, getResp.Body.String())
	}

	body := map[string]any{
		"sections": map[string]any{
			"censorship": map[string]any{"tls_domain": "x"},
		},
	}
	putResp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/"+agentID+"/config", body, cookies)
	if putResp.Code != http.StatusNotFound {
		t.Fatalf("PUT out-of-scope agent config status = %d, want %d (body: %s)", putResp.Code, http.StatusNotFound, putResp.Body.String())
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
