package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestHTTPTelemetryEndpointsExposeOperatorSummariesAndDetailBoost(t *testing.T) {
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
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	server.agents["agent-a"] = Agent{
		ID:           "agent-a",
		NodeName:     "fra-a",
		FleetGroupID: "eu",
		Version:      "1.0.0",
		Runtime: AgentRuntime{
			AcceptingNewConnections:   true,
			MERuntimeReady:            true,
			TransportMode:             "direct",
			CurrentConnections:        120,
			CurrentConnectionsME:      70,
			CurrentConnectionsDirect:  50,
			ActiveUsers:               95,
			DCCoveragePct:             100,
			HealthyUpstreams:          2,
			TotalUpstreams:            2,
			UpdatedAt:                 now.Add(-10 * time.Second),
			DCs: []RuntimeDC{{DC: 2, AvailableEndpoints: 4, AvailablePct: 100, RequiredWriters: 6, AliveWriters: 6, CoveragePct: 100, RTTMs: 18, Load: 1}},
			RecentEvents: []RuntimeEvent{{Sequence: 1, TimestampUnix: now.Add(-15 * time.Second).Unix(), EventType: "upstream_recovered", Context: "dc=2 upstream=1"}},
		},
		LastSeenAt: now.Add(-5 * time.Second),
	}
	server.agents["agent-b"] = Agent{
		ID:           "agent-b",
		NodeName:     "ams-b",
		FleetGroupID: "eu",
		Version:      "1.0.0",
		ReadOnly:     true,
		Runtime: AgentRuntime{
			AcceptingNewConnections:   false,
			MERuntimeReady:            true,
			Degraded:                  true,
			TransportMode:             "middle_proxy",
			CurrentConnections:        12,
			CurrentConnectionsME:      10,
			CurrentConnectionsDirect:  2,
			ActiveUsers:               9,
			DCCoveragePct:             73,
			HealthyUpstreams:          1,
			TotalUpstreams:            3,
			UpdatedAt:                 now.Add(-120 * time.Second),
			DCs: []RuntimeDC{{DC: 4, AvailableEndpoints: 4, AvailablePct: 100, RequiredWriters: 6, AliveWriters: 4, CoveragePct: 73, RTTMs: 64, Load: 2}},
			RecentEvents: []RuntimeEvent{{Sequence: 2, TimestampUnix: now.Add(-20 * time.Second).Unix(), EventType: "dc_coverage_dropped", Context: "dc=4 coverage=73"}},
		},
		LastSeenAt: now.Add(-40 * time.Second),
	}
	server.presence.MarkConnected("agent-a", now.Add(-5*time.Second))
	server.presence.MarkConnected("agent-b", now.Add(-40*time.Second))
	if err := store.PutFleetGroup(context.Background(), storage.FleetGroupRecord{
		ID:        "eu",
		Name:      "eu",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	if err := store.PutAgent(context.Background(), agentToRecord(server.agents["agent-a"])); err != nil {
		t.Fatalf("PutAgent(agent-a) error = %v", err)
	}
	if err := store.PutAgent(context.Background(), agentToRecord(server.agents["agent-b"])); err != nil {
		t.Fatalf("PutAgent(agent-b) error = %v", err)
	}
	if err := store.PutTelemetryDiagnosticsCurrent(context.Background(), storage.TelemetryDiagnosticsCurrentRecord{
		AgentID:             "agent-a",
		ObservedAt:          now.Add(-15 * time.Second),
		State:               "fresh",
		SystemInfoJSON:      `{"version":"2026.03","target_arch":"x86_64","config_hash":"cfg-1","config_reload_count":3}`,
		EffectiveLimitsJSON: `{"update_every_secs":5,"me_reinit_every_secs":30,"me_pool_force_close_secs":120,"user_ip_policy":{"mode":"combined","window_secs":600}}`,
		SecurityPostureJSON: `{"api_read_only":false,"api_whitelist_enabled":true,"api_whitelist_entries":2,"telemetry_me_level":"debug"}`,
		MinimalAllJSON:      `{"enabled":true,"data":{"network_path":[{"dc":2,"selected_ip":"149.154.167.40"}]}}`,
		MEPoolJSON:          `{"enabled":true,"data":{"active_generation":7,"warm_generation":8}}`,
	}); err != nil {
		t.Fatalf("PutTelemetryDiagnosticsCurrent() error = %v", err)
	}
	if err := store.PutTelemetrySecurityInventoryCurrent(context.Background(), storage.TelemetrySecurityInventoryCurrentRecord{
		AgentID:      "agent-a",
		ObservedAt:   now.Add(-15 * time.Second),
		State:        "fresh",
		Enabled:      true,
		EntriesTotal: 2,
		EntriesJSON:  `["10.0.0.0/24","192.168.0.0/24"]`,
	}); err != nil {
		t.Fatalf("PutTelemetrySecurityInventoryCurrent() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	dashboardResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/telemetry/dashboard", nil, cookies)
	if dashboardResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/telemetry/dashboard status = %d, want %d", dashboardResponse.Code, http.StatusOK)
	}
	var dashboardPayload telemetryDashboardResponse
	if err := json.Unmarshal(dashboardResponse.Body.Bytes(), &dashboardPayload); err != nil {
		t.Fatalf("json.Unmarshal(dashboard) error = %v", err)
	}
	if len(dashboardPayload.ServerCards) != 2 {
		t.Fatalf("len(server_cards) = %d, want %d", len(dashboardPayload.ServerCards), 2)
	}
	if len(dashboardPayload.Attention) != 1 {
		t.Fatalf("len(attention) = %d, want %d", len(dashboardPayload.Attention), 1)
	}
	if dashboardPayload.Attention[0].AgentID != "agent-b" {
		t.Fatalf("attention[0].agent_id = %q, want %q", dashboardPayload.Attention[0].AgentID, "agent-b")
	}
	if dashboardPayload.Fleet.LiveConnections != 132 {
		t.Fatalf("fleet.live_connections = %d, want %d", dashboardPayload.Fleet.LiveConnections, 132)
	}

	serversResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/telemetry/servers", nil, cookies)
	if serversResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/telemetry/servers status = %d, want %d", serversResponse.Code, http.StatusOK)
	}
	var serversPayload telemetryServersResponse
	if err := json.Unmarshal(serversResponse.Body.Bytes(), &serversPayload); err != nil {
		t.Fatalf("json.Unmarshal(servers) error = %v", err)
	}
	if len(serversPayload.Servers) != 2 {
		t.Fatalf("len(servers) = %d, want %d", len(serversPayload.Servers), 2)
	}
	if serversPayload.Servers[0].Agent.ID != "agent-b" {
		t.Fatalf("servers[0].agent.id = %q, want %q", serversPayload.Servers[0].Agent.ID, "agent-b")
	}
	if serversPayload.Servers[0].RuntimeFreshness.State != "stale" {
		t.Fatalf("servers[0].runtime_freshness.state = %q, want %q", serversPayload.Servers[0].RuntimeFreshness.State, "stale")
	}

	boostResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/telemetry/servers/agent-a/detail-boost", nil, cookies)
	if boostResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/telemetry/servers/{id}/detail-boost status = %d, want %d", boostResponse.Code, http.StatusOK)
	}
	var boostPayload telemetryDetailBoostResponse
	if err := json.Unmarshal(boostResponse.Body.Bytes(), &boostPayload); err != nil {
		t.Fatalf("json.Unmarshal(boost) error = %v", err)
	}
	if !boostPayload.Active {
		t.Fatal("boost.active = false, want true")
	}

	refreshResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/telemetry/servers/agent-a/refresh-diagnostics", nil, cookies)
	if refreshResponse.Code != http.StatusAccepted {
		t.Fatalf("POST /api/telemetry/servers/{id}/refresh-diagnostics status = %d, want %d", refreshResponse.Code, http.StatusAccepted)
	}

	detailResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/telemetry/servers/agent-a", nil, cookies)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/telemetry/servers/{id} status = %d, want %d", detailResponse.Code, http.StatusOK)
	}
	var detailPayload telemetryServerDetailResponse
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("json.Unmarshal(detail) error = %v", err)
	}
	if !detailPayload.Server.DetailBoost.Active {
		t.Fatal("detail.server.detail_boost.active = false, want true")
	}
	if detailPayload.Server.Agent.ID != "agent-a" {
		t.Fatalf("detail.server.agent.id = %q, want %q", detailPayload.Server.Agent.ID, "agent-a")
	}
	if detailPayload.Diagnostics.SystemInfo["config_hash"] != "cfg-1" {
		t.Fatalf("detail.diagnostics.system_info.config_hash = %v, want %q", detailPayload.Diagnostics.SystemInfo["config_hash"], "cfg-1")
	}
	if detailPayload.Diagnostics.EffectiveLimits["update_every_secs"] != float64(5) {
		t.Fatalf("detail.diagnostics.effective_limits.update_every_secs = %v, want %v", detailPayload.Diagnostics.EffectiveLimits["update_every_secs"], 5)
	}
	if detailPayload.SecurityInventory.EntriesTotal != 2 {
		t.Fatalf("detail.security_inventory.entries_total = %d, want %d", detailPayload.SecurityInventory.EntriesTotal, 2)
	}
	if len(detailPayload.SecurityInventory.Entries) != 2 {
		t.Fatalf("len(detail.security_inventory.entries) = %d, want %d", len(detailPayload.SecurityInventory.Entries), 2)
	}

	jobsList := server.jobs.List()
	foundRefreshJob := false
	for _, job := range jobsList {
		if string(job.Action) == "telemetry.refresh_diagnostics" {
			foundRefreshJob = true
			break
		}
	}
	if !foundRefreshJob {
		t.Fatal("jobs.List() did not contain telemetry.refresh_diagnostics job")
	}

	restored := New(Options{
		Now:   func() time.Time { return now.Add(time.Minute) },
		Store: store,
	})
	restored.agents["agent-a"] = server.agents["agent-a"]
	restored.presence.MarkConnected("agent-a", now.Add(-5*time.Second))

	restoredLogin := performJSONRequest(t, restored.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if restoredLogin.Code != http.StatusOK {
		t.Fatalf("restored POST /api/auth/login status = %d, want %d", restoredLogin.Code, http.StatusOK)
	}

	restoredDetail := performJSONRequest(t, restored.Handler(), http.MethodGet, "/api/telemetry/servers/agent-a", nil, restoredLogin.Result().Cookies())
	if restoredDetail.Code != http.StatusOK {
		t.Fatalf("restored GET /api/telemetry/servers/{id} status = %d, want %d", restoredDetail.Code, http.StatusOK)
	}
	var restoredDetailPayload telemetryServerDetailResponse
	if err := json.Unmarshal(restoredDetail.Body.Bytes(), &restoredDetailPayload); err != nil {
		t.Fatalf("json.Unmarshal(restored detail) error = %v", err)
	}
	if !restoredDetailPayload.Server.DetailBoost.Active {
		t.Fatal("restored detail boost = false, want persisted boost")
	}
}

func TestHTTPTelemetryDetailExposesInitializationWatchActiveAndCooldown(t *testing.T) {
	now := time.Date(2026, time.March, 29, 16, 0, 0, 0, time.UTC)
	server := New(Options{Now: func() time.Time { return now }})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	server.agents["agent-a"] = Agent{
		ID:           "agent-a",
		NodeName:     "fra-a",
		FleetGroupID: "eu",
		Version:      "1.0.0",
		Runtime: AgentRuntime{
			AcceptingNewConnections:   false,
			MERuntimeReady:            false,
			StartupStatus:             "starting",
			StartupStage:              "me_pool_bootstrap",
			StartupProgressPct:        42,
			InitializationStatus:      "starting",
			InitializationStage:       "warming_me_pool",
			InitializationProgressPct: 38,
			UpdatedAt:                 now.Add(-5 * time.Second),
		},
		LastSeenAt: now.Add(-3 * time.Second),
	}
	server.presence.MarkConnected("agent-a", now.Add(-3*time.Second))

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	activeDetailResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/telemetry/servers/agent-a", nil, cookies)
	if activeDetailResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/telemetry/servers/{id} active status = %d, want %d", activeDetailResponse.Code, http.StatusOK)
	}
	var activeDetail telemetryServerDetailResponse
	if err := json.Unmarshal(activeDetailResponse.Body.Bytes(), &activeDetail); err != nil {
		t.Fatalf("json.Unmarshal(active detail) error = %v", err)
	}
	if !activeDetail.InitializationWatch.Visible {
		t.Fatal("initialization_watch.visible = false, want true while runtime is starting")
	}
	if activeDetail.InitializationWatch.Mode != "active" {
		t.Fatalf("initialization_watch.mode = %q, want %q", activeDetail.InitializationWatch.Mode, "active")
	}

	server.mu.Lock()
	agent := server.agents["agent-a"]
	agent.Runtime.AcceptingNewConnections = true
	agent.Runtime.MERuntimeReady = true
	agent.Runtime.StartupStatus = "ready"
	agent.Runtime.StartupStage = "steady_state"
	agent.Runtime.StartupProgressPct = 100
	agent.Runtime.InitializationStatus = "ready"
	agent.Runtime.InitializationStage = "steady_state"
	agent.Runtime.InitializationProgressPct = 100
	agent.Runtime.LifecycleState = "ready"
	agent.Runtime.UpdatedAt = now.Add(15 * time.Second)
	server.agents["agent-a"] = agent
	server.initializationWatchCooldowns["agent-a"] = now.Add(15 * time.Second).Add(telemetryInitializationWatchCooldown)
	server.mu.Unlock()

	now = now.Add(30 * time.Second)

	cooldownDetailResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/telemetry/servers/agent-a", nil, cookies)
	if cooldownDetailResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/telemetry/servers/{id} cooldown status = %d, want %d", cooldownDetailResponse.Code, http.StatusOK)
	}
	var cooldownDetail telemetryServerDetailResponse
	if err := json.Unmarshal(cooldownDetailResponse.Body.Bytes(), &cooldownDetail); err != nil {
		t.Fatalf("json.Unmarshal(cooldown detail) error = %v", err)
	}
	if !cooldownDetail.InitializationWatch.Visible {
		t.Fatal("initialization_watch.visible = false, want true during ready cooldown")
	}
	if cooldownDetail.InitializationWatch.Mode != "cooldown" {
		t.Fatalf("initialization_watch.mode = %q, want %q", cooldownDetail.InitializationWatch.Mode, "cooldown")
	}

	now = server.initializationWatchCooldowns["agent-a"].Add(time.Second)

	hiddenDetailResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/telemetry/servers/agent-a", nil, cookies)
	if hiddenDetailResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/telemetry/servers/{id} hidden status = %d, want %d", hiddenDetailResponse.Code, http.StatusOK)
	}
	var hiddenDetail telemetryServerDetailResponse
	if err := json.Unmarshal(hiddenDetailResponse.Body.Bytes(), &hiddenDetail); err != nil {
		t.Fatalf("json.Unmarshal(hidden detail) error = %v", err)
	}
	if hiddenDetail.InitializationWatch.Visible {
		t.Fatal("initialization_watch.visible = true, want false after cooldown expires")
	}
}
