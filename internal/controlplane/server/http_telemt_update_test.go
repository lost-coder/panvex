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
	"github.com/lost-coder/panvex/internal/controlplane/updates"
	"github.com/lost-coder/panvex/internal/telemtjob"
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

// --- POST /api/agents/{id}/telemt/update (Task 11) -------------------------

// availableBinaryProbe is a live probe reporting an in-place update path.
func availableBinaryProbe() *TelemtUpdateProbe {
	return &TelemtUpdateProbe{
		Mode:                 "binary",
		SuggestedRestartSpec: "systemd:telemt",
		BinaryPath:           "/usr/local/bin/telemt",
		Available:            true,
	}
}

// dispatchErrorBody decodes the {error, code} envelope a guard 409 writes.
func dispatchErrorBody(t *testing.T, resp interface{ Bytes() []byte }) errorResponse {
	t.Helper()
	var got errorResponse
	if err := json.Unmarshal(resp.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return got
}

func TestDispatchTelemtUpdateUnknownAgentReturns404(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/does-not-exist/telemt/update", map[string]string{}, adminCookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

// TestDispatchTelemtUpdateInvalidVersionReturns400: version flows
// unvalidated into the release URL and the job's download path
// (updates.TelemtReleaseBaseURLForVersion, buildTelemtUpdateJobPayload), so
// anything that is not a bare semver string — in particular a path-traversal
// payload — must be rejected before any of the 409 guards run.
func TestDispatchTelemtUpdateInvalidVersionReturns400(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	seedLiveAgentWithProbe(srv, "agent-1", availableBinaryProbe())

	cases := []string{
		"../../evil/1.0.0",
		"v../../evil/1.0.0",
		"3.4",
		"3.4.25-rc1",
		"3.4.25/../x",
		"not-a-version",
	}
	for _, version := range cases {
		t.Run(version, func(t *testing.T) {
			resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-1/telemt/update",
				map[string]string{"version": version}, adminCookies)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusBadRequest, resp.Body.String())
			}
			if got := dispatchErrorBody(t, resp.Body); got.Code != "invalid_version" {
				t.Fatalf("error code = %q, want %q (body=%s)", got.Code, "invalid_version", resp.Body.String())
			}
		})
	}
}

// TestDispatchTelemtUpdateValidVersionWithVPrefixAccepted: the "v" prefix is
// trimmed before validation, mirroring the resolution logic below.
func TestDispatchTelemtUpdateValidVersionWithVPrefixAccepted(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	seedLiveAgentWithProbe(srv, "agent-1", availableBinaryProbe())
	seedTestAgentRow(t, srv, "agent-1", now)
	if err := srv.telemtStrategySvc.Put(context.Background(), "agent-1", updates.Strategy{
		Mode:        updates.StrategyModeBinary,
		RestartSpec: "systemd:telemt",
		BinaryPath:  "/usr/local/bin/telemt",
	}); err != nil {
		t.Fatalf("Put strategy: %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-1/telemt/update",
		map[string]string{"version": "v3.4.25"}, adminCookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusAccepted, resp.Body.String())
	}
}

func TestDispatchTelemtUpdateRejectsNonAdmin(t *testing.T) {
	srv, _, operatorCookies := newTelemtUpdateStrategyTestServer(t)
	seedLiveAgentWithProbe(srv, "agent-1", availableBinaryProbe())

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-1/telemt/update", map[string]string{}, operatorCookies)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusForbidden, resp.Body.String())
	}
}

func TestDispatchTelemtUpdateStrategyNotConfiguredReturns409(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	seedLiveAgentWithProbe(srv, "agent-1", availableBinaryProbe())

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-1/telemt/update", map[string]string{}, adminCookies)
	if resp.Code != http.StatusConflict {
		t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusConflict, resp.Body.String())
	}
	if got := dispatchErrorBody(t, resp.Body); got.Code != "strategy_not_configured" {
		t.Fatalf("error code = %q, want %q (body=%s)", got.Code, "strategy_not_configured", resp.Body.String())
	}
}

func TestDispatchTelemtUpdateModeNotBinaryReturns409(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	seedLiveAgentWithProbe(srv, "agent-1", availableBinaryProbe())
	seedTestAgentRow(t, srv, "agent-1", now)
	if err := srv.telemtStrategySvc.Put(context.Background(), "agent-1", updates.Strategy{Mode: updates.StrategyModeDocker}); err != nil {
		t.Fatalf("Put strategy: %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-1/telemt/update", map[string]string{}, adminCookies)
	if resp.Code != http.StatusConflict {
		t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusConflict, resp.Body.String())
	}
	if got := dispatchErrorBody(t, resp.Body); got.Code != "mode_not_binary" {
		t.Fatalf("error code = %q, want %q (body=%s)", got.Code, "mode_not_binary", resp.Body.String())
	}
}

func TestDispatchTelemtUpdateUnavailableReturns409(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	// Live probe reports no in-place path available, even though a binary
	// strategy is configured — the guard must trust the LIVE probe.
	seedLiveAgentWithProbe(srv, "agent-1", &TelemtUpdateProbe{Mode: "none", Available: false, Reason: "no supported supervisor detected"})
	seedTestAgentRow(t, srv, "agent-1", now)
	if err := srv.telemtStrategySvc.Put(context.Background(), "agent-1", updates.Strategy{
		Mode:        updates.StrategyModeBinary,
		RestartSpec: "systemd:telemt",
		BinaryPath:  "/usr/local/bin/telemt",
	}); err != nil {
		t.Fatalf("Put strategy: %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-1/telemt/update", map[string]string{}, adminCookies)
	if resp.Code != http.StatusConflict {
		t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusConflict, resp.Body.String())
	}
	if got := dispatchErrorBody(t, resp.Body); got.Code != "update_unavailable" {
		t.Fatalf("error code = %q, want %q (body=%s)", got.Code, "update_unavailable", resp.Body.String())
	}
}

func TestDispatchTelemtUpdateNoKnownReleaseReturns409(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	seedLiveAgentWithProbe(srv, "agent-1", availableBinaryProbe())
	seedTestAgentRow(t, srv, "agent-1", now)
	if err := srv.telemtStrategySvc.Put(context.Background(), "agent-1", updates.Strategy{
		Mode:        updates.StrategyModeBinary,
		RestartSpec: "systemd:telemt",
		BinaryPath:  "/usr/local/bin/telemt",
	}); err != nil {
		t.Fatalf("Put strategy: %v", err)
	}
	// No version in the request, and the panel has never cached a latest
	// Telemt release (zero-value updateState).

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-1/telemt/update", map[string]string{}, adminCookies)
	if resp.Code != http.StatusConflict {
		t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusConflict, resp.Body.String())
	}
	if got := dispatchErrorBody(t, resp.Body); got.Code != "no_known_release" {
		t.Fatalf("error code = %q, want %q (body=%s)", got.Code, "no_known_release", resp.Body.String())
	}
}

// seedTelemtUpdateHappyPathAgent wires agent-1 with a binary strategy and an
// available probe — the common precondition for every non-guard test below.
func seedTelemtUpdateHappyPathAgent(t *testing.T, srv *Server, now time.Time) {
	t.Helper()
	seedLiveAgentWithProbe(srv, "agent-1", availableBinaryProbe())
	seedTestAgentRow(t, srv, "agent-1", now)
	if err := srv.telemtStrategySvc.Put(context.Background(), "agent-1", updates.Strategy{
		Mode:        updates.StrategyModeBinary,
		RestartSpec: "systemd:telemt",
		BinaryPath:  "/usr/local/bin/telemt",
		AssetFlavor: "v3",
	}); err != nil {
		t.Fatalf("Put strategy: %v", err)
	}
}

// TestDispatchTelemtUpdateHappyPathExplicitVersion is the contract test: the
// job payload must unmarshal into telemtjob.Payload (Task 5's exact-keys
// contract, imported here as the agent package the panel targets), the job
// must land in jobs.Service under jobs.ActionTelemtUpdate, and the pending
// desired-state map must record the resolved version.
func TestDispatchTelemtUpdateHappyPathExplicitVersion(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	seedTelemtUpdateHappyPathAgent(t, srv, now)

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-1/telemt/update",
		map[string]any{"version": "v3.4.25", "allow_downgrade": true}, adminCookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	var got dispatchTelemtUpdateResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.JobID == "" {
		t.Fatal("job_id is empty")
	}

	job, ok := srv.jobs.Get(got.JobID)
	if !ok {
		t.Fatalf("job %q not found in jobs.Service", got.JobID)
	}
	if job.Action != jobs.ActionTelemtUpdate {
		t.Fatalf("job.Action = %q, want %q", job.Action, jobs.ActionTelemtUpdate)
	}
	if len(job.TargetAgentIDs) != 1 || job.TargetAgentIDs[0] != "agent-1" {
		t.Fatalf("job.TargetAgentIDs = %v, want [agent-1]", job.TargetAgentIDs)
	}

	// Contract guarantee: unmarshal straight into the agent's own Payload
	// type. A "v" prefix in the request must be stripped (Telemt tags are
	// bare semver).
	var payload telemtjob.Payload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal job payload into telemtjob.Payload: %v", err)
	}
	want := telemtjob.Payload{
		Version:        "3.4.25",
		ReleaseBaseURL: "https://github.com/telemt/telemt/releases/download/3.4.25",
		AllowDowngrade: true,
		RestartSpec:    "systemd:telemt",
		BinaryPath:     "/usr/local/bin/telemt",
		AssetFlavor:    "v3",
	}
	if payload != want {
		t.Fatalf("job payload = %+v, want %+v", payload, want)
	}

	// Exact-keys contract: re-marshal the decoded struct and diff against the
	// wire JSON as a set of keys, so an accidental extra field (e.g. a leaked
	// agent_id) is caught even though it would silently survive the
	// unmarshal above.
	var wireKeys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(job.PayloadJSON), &wireKeys); err != nil {
		t.Fatalf("unmarshal job payload as map: %v", err)
	}
	wantKeys := []string{"version", "release_base_url", "allow_downgrade", "restart_spec", "binary_path", "asset_flavor"}
	if len(wireKeys) != len(wantKeys) {
		t.Fatalf("job payload has %d keys (%v), want exactly %v", len(wireKeys), wireKeys, wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := wireKeys[k]; !ok {
			t.Fatalf("job payload missing key %q: %s", k, job.PayloadJSON)
		}
	}

	version, ok, err := srv.updatesSvc.PendingTelemtUpdate(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("PendingTelemtUpdate() error = %v", err)
	}
	if !ok || version != "3.4.25" {
		t.Fatalf("pending target after dispatch = %q/%v, want 3.4.25/true", version, ok)
	}
}

// TestDispatchTelemtUpdateDefaultsToLatestKnownVersion covers the
// version-resolution fallback: an empty request body must fall back to the
// panel's cached TelemtLatestVersion/TelemtReleaseBaseURL.
func TestDispatchTelemtUpdateDefaultsToLatestKnownVersion(t *testing.T) {
	srv, adminCookies, _ := newTelemtUpdateStrategyTestServer(t)
	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	seedTelemtUpdateHappyPathAgent(t, srv, now)

	srv.settingsMu.Lock()
	srv.updateState = UpdateState{
		TelemtLatestVersion:  "3.4.24",
		TelemtReleaseBaseURL: "https://github.com/telemt/telemt/releases/download/3.4.24",
	}
	srv.settingsMu.Unlock()

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/agents/agent-1/telemt/update", map[string]any{}, adminCookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d, want %d (body=%s)", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	var got dispatchTelemtUpdateResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	job, ok := srv.jobs.Get(got.JobID)
	if !ok {
		t.Fatalf("job %q not found", got.JobID)
	}
	var payload telemtjob.Payload
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal job payload: %v", err)
	}
	if payload.Version != "3.4.24" {
		t.Fatalf("payload.Version = %q, want 3.4.24 (the cached latest)", payload.Version)
	}
	if payload.ReleaseBaseURL != "https://github.com/telemt/telemt/releases/download/3.4.24" {
		t.Fatalf("payload.ReleaseBaseURL = %q, want the release download base for 3.4.24", payload.ReleaseBaseURL)
	}
	if payload.AllowDowngrade {
		t.Fatal("AllowDowngrade must default to false when omitted")
	}
}
