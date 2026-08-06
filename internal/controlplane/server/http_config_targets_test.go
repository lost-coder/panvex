package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
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

// TestConfigTargetPutRejectsShowLinkSection guards telemt-audit F4: Telemt's
// PATCH /v1/config editable sections are exactly general, timeouts,
// censorship, upstreams, dc_overrides. show_link is a legacy top-level key
// Telemt auto-migrates to general.links.show on load, rejects on PATCH with
// 400 section_not_editable, and never returns from GET. A stored show_link
// target therefore made every config-apply fail permanently and showed as
// perpetual drift — the panel must reject it up front like any other
// non-editable section.
func TestConfigTargetPutRejectsShowLinkSection(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	groupID := seedTestFleetGroup(t, srv.store, "cfg-group-showlink", time.Time{})

	body := map[string]any{
		"sections": map[string]any{
			"show_link": map[string]any{"show": true},
		},
	}
	resp := performJSONRequest(t, srv, http.MethodPut, "/api/fleet-groups/"+groupID+"/config", body, cookies)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("PUT show_link section status = %d, want %d (body: %s)", resp.Code, http.StatusBadRequest, resp.Body.String())
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
	if tlsDomain := nestedString(got.Desired, "censorship", "tls_domain"); tlsDomain != "override.example" {
		t.Fatalf("agent desired.censorship.tls_domain = %q, want %q", tlsDomain, "override.example")
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

// TestAgentConfigResponseUsesDesiredAndSnapshotDrift seeds a P2 full-snapshot
// desired config directly (carrying the schema-version marker, the way
// seedDesiredConfig would) that differs from the observed config, and
// asserts: the GET response's `desired` field carries the stored sections
// under the new `desired` JSON key, the schema-version marker never reaches
// the client in any field, and drift is computed desired-vs-observed (not
// effective-vs-observed) — status "drifted" with the mismatching path listed.
func TestAgentConfigResponseUsesDesiredAndSnapshotDrift(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	const agentID = "agent-desired-drift"

	managed, _ := json.Marshal(map[string]any{
		"general": map[string]any{"log_level": "silent"},
	})
	srv.live.ApplySnapshot(agentID, Agent{ID: agentID, NodeName: "node-desired-drift"}, []Instance{
		{ID: "telemt-primary", AgentID: agentID, ManagedConfigJSON: string(managed)},
	})

	// Seed the agent's desired snapshot directly through the config-targets
	// store (bypassing the PUT handler's editable-section allowlist, which
	// would reject the schema-version marker key), mirroring what
	// seedDesiredConfig writes on first observation.
	if err := srv.configTargets.Upsert(context.Background(), storage.ConfigScopeAgent, agentID, map[string]any{
		"general":           map[string]any{"log_level": "normal"},
		schemaVersionMarker: "1.2.3",
	}); err != nil {
		t.Fatalf("seed desired snapshot: %v", err)
	}

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/"+agentID+"/config", nil, cookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET agent config status = %d, want %d (body: %s)", getResp.Code, http.StatusOK, getResp.Body.String())
	}
	rawBody := getResp.Body.String()
	if strings.Contains(rawBody, schemaVersionMarker) {
		t.Fatalf("response leaked schema version marker: %s", rawBody)
	}

	var got agentConfigTargetResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode agent config response: %v", err)
	}
	if logLevel := nestedString(got.Desired, "general", "log_level"); logLevel != "normal" {
		t.Fatalf("desired.general.log_level = %q, want %q", logLevel, "normal")
	}
	if got.Drift.Status != "drifted" {
		t.Fatalf("drift.status = %q, want %q", got.Drift.Status, "drifted")
	}
	found := false
	for _, f := range got.Drift.Fields {
		if f == "general.log_level" {
			found = true
		}
	}
	if !found {
		t.Fatalf("drift.fields = %v, want to contain %q", got.Drift.Fields, "general.log_level")
	}
}

// TestPutMergesIntoSnapshot (P2 Task 5): PUTing a sparse sections map onto an
// agent that already has a full desired snapshot must MERGE the new leaf
// into the stored snapshot, not replace it wholesale — a sibling field
// (general.ad_tag) untouched by the request, and the __schema_version
// marker seedDesiredConfig stamps, must both survive the PUT.
func TestPutMergesIntoSnapshot(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	const agentID = "agent-put-merge"
	srv.live.ApplySnapshot(agentID, Agent{ID: agentID, NodeName: "node-" + agentID}, nil)

	if err := srv.configTargets.Upsert(context.Background(), storage.ConfigScopeAgent, agentID, map[string]any{
		"general":           map[string]any{"log_level": "silent", "ad_tag": "x"},
		schemaVersionMarker: "1.2.3",
	}); err != nil {
		t.Fatalf("seed desired snapshot: %v", err)
	}

	putBody := map[string]any{
		"sections": map[string]any{
			"general": map[string]any{"log_level": "normal"},
		},
	}
	if resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/"+agentID+"/config", putBody, cookies); resp.Code != http.StatusOK {
		t.Fatalf("PUT agent config status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}

	got := getAgentConfig(t, srv, cookies, agentID)
	if logLevel := nestedString(got.Desired, "general", "log_level"); logLevel != "normal" {
		t.Fatalf("desired.general.log_level = %q, want %q", logLevel, "normal")
	}
	if adTag := nestedString(got.Desired, "general", "ad_tag"); adTag != "x" {
		t.Fatalf("desired.general.ad_tag = %q, want %q (merge must preserve fields untouched by the PUT)", adTag, "x")
	}

	stored, err := srv.configTargets.Sections(context.Background(), storage.ConfigScopeAgent, agentID)
	if err != nil {
		t.Fatalf("configTargets.Sections() error = %v", err)
	}
	if v, _ := stored[schemaVersionMarker].(string); v != "1.2.3" {
		t.Fatalf("stored schema-version marker = %q, want %q (merge must preserve it)", v, "1.2.3")
	}
}

// seedGroupTargetAndAgent seeds a fleet group, PUTs a group config target
// (censorship.tls_domain = groupDomain), and seeds the agent into the live
// snapshot with the supplied observed instances. Returns the group id.
func seedGroupTargetAndAgent(t *testing.T, srv *Server, cookies []*http.Cookie, groupName, groupDomain, agentID string, instances []Instance) string {
	t.Helper()
	groupID := seedTestFleetGroup(t, srv.store, groupName, time.Time{})
	srv.live.ApplySnapshot(agentID, Agent{ID: agentID, NodeName: "node-" + agentID, FleetGroupID: groupID}, instances)
	body := map[string]any{
		"sections": map[string]any{
			"censorship": map[string]any{"tls_domain": groupDomain},
		},
	}
	if resp := performJSONRequest(t, srv, http.MethodPut, "/api/fleet-groups/"+groupID+"/config", body, cookies); resp.Code != http.StatusOK {
		t.Fatalf("PUT group config status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}
	return groupID
}

// observedInstance builds a live Instance carrying a censorship.tls_domain
// observed value as canonical JSON.
func observedInstance(agentID, tlsDomain string) Instance {
	managed, _ := json.Marshal(map[string]any{
		"censorship": map[string]any{"tls_domain": tlsDomain},
	})
	return Instance{ID: "telemt-primary", AgentID: agentID, ManagedConfigJSON: string(managed)}
}

// getAgentConfig fetches the agent config GET response and decodes it.
func getAgentConfig(t *testing.T, srv *Server, cookies []*http.Cookie, agentID string) agentConfigTargetResponse {
	t.Helper()
	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/"+agentID+"/config", nil, cookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET agent config status = %d, want %d (body: %s)", getResp.Code, http.StatusOK, getResp.Body.String())
	}
	var got agentConfigTargetResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode agent config response: %v", err)
	}
	return got
}

// TestConfigTargetAgentDriftInSync seeds an observed instance matching the
// agent's own desired target → drift.status == "in_sync". Drift is
// desired-vs-observed (P2), so the agent's own config target — not just the
// group target seeded by seedGroupTargetAndAgent — must equal observed here.
func TestConfigTargetAgentDriftInSync(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	const agentID = "agent-drift-insync"
	seedGroupTargetAndAgent(t, srv, cookies, "cfg-drift-insync", "match.example", agentID,
		[]Instance{observedInstance(agentID, "match.example")})
	agentBody := map[string]any{
		"sections": map[string]any{
			"censorship": map[string]any{"tls_domain": "match.example"},
		},
	}
	if resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/"+agentID+"/config", agentBody, cookies); resp.Code != http.StatusOK {
		t.Fatalf("PUT agent config status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}

	got := getAgentConfig(t, srv, cookies, agentID)
	if got.Drift.Status != "in_sync" {
		t.Fatalf("drift.status = %q, want %q", got.Drift.Status, "in_sync")
	}
	if len(got.Drift.Fields) != 0 {
		t.Fatalf("drift.fields = %v, want empty", got.Drift.Fields)
	}
}

// TestConfigTargetAgentDriftDrifted seeds an observed instance whose value
// mismatches the agent's own desired target → drift.status == "drifted" and
// the field is listed. Drift is desired-vs-observed (P2), so the agent's own
// config target is what must differ from observed here — the group target
// alone (seeded by seedGroupTargetAndAgent) no longer drives agent drift.
func TestConfigTargetAgentDriftDrifted(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	const agentID = "agent-drift-drifted"
	seedGroupTargetAndAgent(t, srv, cookies, "cfg-drift-drifted", "target.example", agentID,
		[]Instance{observedInstance(agentID, "observed.example")})
	agentBody := map[string]any{
		"sections": map[string]any{
			"censorship": map[string]any{"tls_domain": "target.example"},
		},
	}
	if resp := performJSONRequest(t, srv, http.MethodPut, "/api/agents/"+agentID+"/config", agentBody, cookies); resp.Code != http.StatusOK {
		t.Fatalf("PUT agent config status = %d, want %d (body: %s)", resp.Code, http.StatusOK, resp.Body.String())
	}

	got := getAgentConfig(t, srv, cookies, agentID)
	if got.Drift.Status != "drifted" {
		t.Fatalf("drift.status = %q, want %q", got.Drift.Status, "drifted")
	}
	found := false
	for _, f := range got.Drift.Fields {
		if f == "censorship.tls_domain" {
			found = true
		}
	}
	if !found {
		t.Fatalf("drift.fields = %v, want to contain %q", got.Drift.Fields, "censorship.tls_domain")
	}
}

// TestConfigTargetAgentDriftUnknown seeds no observed instance → drift.status
// == "unknown".
func TestConfigTargetAgentDriftUnknown(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	const agentID = "agent-drift-unknown"
	seedGroupTargetAndAgent(t, srv, cookies, "cfg-drift-unknown", "target.example", agentID, nil)

	got := getAgentConfig(t, srv, cookies, agentID)
	if got.Drift.Status != "unknown" {
		t.Fatalf("drift.status = %q, want %q", got.Drift.Status, "unknown")
	}
	if got.Drift.Fields == nil {
		t.Fatalf("drift.fields = nil, want non-nil empty slice")
	}
}

// TestConfigTargetAgentDriftEmptyJSONUnknown seeds an instance with an empty
// ManagedConfigJSON → drift.status == "unknown".
func TestConfigTargetAgentDriftEmptyJSONUnknown(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	const agentID = "agent-drift-emptyjson"
	seedGroupTargetAndAgent(t, srv, cookies, "cfg-drift-emptyjson", "target.example", agentID,
		[]Instance{{ID: "telemt-primary", AgentID: agentID, ManagedConfigJSON: ""}})

	got := getAgentConfig(t, srv, cookies, agentID)
	if got.Drift.Status != "unknown" {
		t.Fatalf("drift.status = %q, want %q", got.Drift.Status, "unknown")
	}
}

// TestConfigTargetGroupNodesDrift asserts the group GET response surfaces a
// per-agent drift summary for the group's in-scope agents.
func TestConfigTargetGroupNodesDrift(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	const agentID = "agent-group-nodes"
	groupID := seedGroupTargetAndAgent(t, srv, cookies, "cfg-group-nodes", "target.example", agentID,
		[]Instance{observedInstance(agentID, "observed.example")})

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupID+"/config", nil, cookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET group config status = %d, want %d (body: %s)", getResp.Code, http.StatusOK, getResp.Body.String())
	}
	var got groupConfigTargetResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode group config response: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("group nodes = %v, want 1 entry", got.Nodes)
	}
	if got.Nodes[0].AgentID != agentID || got.Nodes[0].Status != "drifted" {
		t.Fatalf("group node = %+v, want agent_id=%q status=drifted", got.Nodes[0], agentID)
	}
}

// TestConfigTargetGroupNodesDriftIgnoresSchemaVersionMarker is a regression
// test for the bug where groupConfigNodeDrifts merged the agent-scope
// override into the group's effective config WITHOUT stripping the P2
// __schema_version marker (seedDesiredConfig stamps every agent's snapshot
// with it). configDrift walks every leaf of the effective target, so the
// marker — which never appears in the observed config — was reported as a
// drifted field for every seeded node, permanently mislabeling in-sync nodes
// as "drifted" on the fleet-group page while the sibling agent-scope GET
// (which does strip the marker) correctly reported "in_sync" for the same
// node. Asserts the group-page status is "in_sync" once the agent-scope
// snapshot (marker included) matches the observed config.
func TestConfigTargetGroupNodesDriftIgnoresSchemaVersionMarker(t *testing.T) {
	srv, cookies := newConfigTargetTestServer(t)
	const agentID = "agent-group-nodes-marker"
	groupID := seedGroupTargetAndAgent(t, srv, cookies, "cfg-group-nodes-marker", "match.example", agentID,
		[]Instance{observedInstance(agentID, "match.example")})

	// Simulate seedDesiredConfig: the agent-scope snapshot carries the P2
	// schema-version marker alongside the config sections, and those
	// sections match what's observed (so the only possible drift source is
	// the unstripped marker).
	agentSections := map[string]any{
		"censorship":        map[string]any{"tls_domain": "match.example"},
		schemaVersionMarker: "telemt-1.2.3",
	}
	if err := srv.configTargets.Upsert(context.Background(), storage.ConfigScopeAgent, agentID, agentSections); err != nil {
		t.Fatalf("seed agent-scope config target with marker: %v", err)
	}

	getResp := performJSONRequest(t, srv, http.MethodGet, "/api/fleet-groups/"+groupID+"/config", nil, cookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET group config status = %d, want %d (body: %s)", getResp.Code, http.StatusOK, getResp.Body.String())
	}
	var got groupConfigTargetResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode group config response: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("group nodes = %v, want 1 entry", got.Nodes)
	}
	if got.Nodes[0].AgentID != agentID || got.Nodes[0].Status != "in_sync" {
		t.Fatalf("group node = %+v, want agent_id=%q status=in_sync (marker must not leak into drift)", got.Nodes[0], agentID)
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
