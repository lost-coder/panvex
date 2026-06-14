package server

// TestTelemtUnreachableEndToEndSeverityLifecycle exercises the full inbound
// snapshot pipeline — gatewayrpc.RuntimeSnapshot → applyAgentSnapshot →
// server state → /api/telemetry/servers HTTP response — across three phases:
//
//  1. Healthy: TelemtUnreachable=false → severity "ok"
//  2. Unreachable: TelemtUnreachable=true → severity "critical", reason contains "Telemt API unreachable"
//  3. Recovery: TelemtUnreachable=false again → severity "ok"
//
// This is the inbound-path complement of TestServerSeverityCriticalWhenTelemtUnreachable
// (agent_telemetry_test.go), which exercises telemetrySeverityAndReason directly.

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/lost-coder/panvex/internal/security"
)

func TestTelemtUnreachableEndToEndSeverityLifecycle(t *testing.T) {
	var nowPtr atomic.Pointer[time.Time]
	setNow := func(t time.Time) { tt := t; nowPtr.Store(&tt) }
	getNow := func() time.Time { return *nowPtr.Load() }
	setNow(time.Date(2026, time.May, 7, 12, 0, 0, 0, time.UTC))

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              getNow,
		Store:            store,
	})
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})

	// Bootstrap an admin user so we can call the HTTP endpoints.
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, getNow()); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	fleetGroupID := seedTestFleetGroup(t, store, "eu", getNow())

	// Enroll an agent through the production path.
	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: fleetGroupID,
		TTL:          time.Minute,
	}, getNow())
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}
	identity, err := server.enrollAgent(context.Background(), agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-eu-1",
		Version:  "1.0.0",
		CSRPEM:   testCSRPEM(t),
	}, getNow())
	if err != nil {
		t.Fatalf("enrollAgent() error = %v", err)
	}
	agentID := identity.AgentID

	// --- helper: login and obtain session cookies ---
	loginAndGetCookies := func() []*http.Cookie {
		t.Helper()
		resp := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
			"username": "admin",
			"password": "Admin1password",
		}, nil)
		if resp.Code != http.StatusOK {
			t.Fatalf("POST /api/auth/login status = %d, want %d", resp.Code, http.StatusOK)
		}
		return resp.Result().Cookies()
	}

	// --- helper: fetch severity and reason for our agent via /api/telemetry/servers ---
	getSeverityAndReason := func(cookies []*http.Cookie) (severity, reason string) {
		t.Helper()
		resp := performJSONRequest(t, server, http.MethodGet, "/api/telemetry/servers", nil, cookies)
		if resp.Code != http.StatusOK {
			t.Fatalf("GET /api/telemetry/servers status = %d, want %d", resp.Code, http.StatusOK)
		}
		var payload telemetryServersResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
			t.Fatalf("json.Unmarshal(servers) error = %v", err)
		}
		for _, s := range payload.Servers {
			if s.Agent.ID == agentID {
				return s.Severity, s.Reason
			}
		}
		t.Fatalf("agent %q not found in /api/telemetry/servers response (len=%d)", agentID, len(payload.Servers))
		return "", ""
	}

	// --- helper: push a snapshot through the real inbound path ---
	pushSnapshot := func(snap *gatewayrpc.RuntimeSnapshot, observedAt time.Time) {
		t.Helper()
		if err := server.applyAgentSnapshot(context.Background(), agentSnapshot{
			AgentID:      agentID,
			NodeName:     "node-eu-1",
			FleetGroupID: fleetGroupID,
			Version:      "1.0.0",
			Runtime:      snap,
			HasRuntime:   true,
			ObservedAt:   observedAt,
		}); err != nil {
			t.Fatalf("applyAgentSnapshot() error = %v", err)
		}
		// Mark the agent as online so the severity projection sees it as present.
		server.presence.MarkConnected(agentID, observedAt)
	}

	cookies := loginAndGetCookies()

	// ── Phase 1: healthy ME ──────────────────────────────────────────────────
	// TelemtUnreachable=false, UseMiddleProxy=true, MeRuntimeReady=true,
	// AcceptingNewConnections=true.
	// A DC with 100% coverage is required: severityME returns "critical" when
	// AgentReported=true (UpdatedAt non-zero) and DCCoveragePct==0.
	healthyDCs := []*gatewayrpc.RuntimeDCSnapshot{
		{Dc: 2, CoveragePct: 100, AvailablePct: 100, AliveWriters: 6, RequiredWriters: 6, AvailableEndpoints: 4},
	}
	healthySnap := &gatewayrpc.RuntimeSnapshot{
		UseMiddleProxy:             true,
		MeRuntimeReady:             true,
		AcceptingNewConnections:    true,
		TelemtUnreachableSinceUnix: 0,
		StartupStatus:              "ready",
		StartupStage:               "serving",
		StartupProgressPct:         100,
		InitializationStatus:       "ready",
		InitializationStage:        "serving",
		InitializationProgressPct:  100,
		TransportMode:              "middle_proxy",
		Dcs:                        healthyDCs,
	}
	pushSnapshot(healthySnap, getNow().Add(5*time.Second))

	sev, _ := getSeverityAndReason(cookies)
	if sev != "ok" && sev != "good" {
		t.Errorf("Phase 1 (healthy): severity = %q, want %q or %q", sev, "ok", "good")
	}

	// ── Phase 2: Telemt API unreachable ─────────────────────────────────────
	// The agent sees TelemtUnreachable=true and records when it first went down.
	unreachableSince := getNow().Add(10 * time.Second)
	unreachableSnap := &gatewayrpc.RuntimeSnapshot{
		UseMiddleProxy:             true,
		MeRuntimeReady:             true,
		AcceptingNewConnections:    true,
		TelemtUnreachable:          true,
		TelemtUnreachableSinceUnix: unreachableSince.Unix(),
		StartupStatus:              "ready",
		StartupStage:               "serving",
		StartupProgressPct:         100,
		InitializationStatus:       "ready",
		InitializationStage:        "serving",
		InitializationProgressPct:  100,
		TransportMode:              "middle_proxy",
	}
	// Push snapshot ~30 s after unreachable started (past any debounce).
	pushSnapshot(unreachableSnap, unreachableSince.Add(30*time.Second))
	// Advance server clock past the snapshot so runtime is fresh.
	setNow(unreachableSince.Add(35 * time.Second))

	sev, reason := getSeverityAndReason(cookies)
	if sev != "critical" {
		t.Errorf("Phase 2 (unreachable): severity = %q, want %q", sev, "critical")
	}
	if !strings.Contains(reason, "Telemt API unreachable") {
		t.Errorf("Phase 2 (unreachable): reason = %q, want it to contain %q", reason, "Telemt API unreachable")
	}

	// ── Phase 3: recovery ────────────────────────────────────────────────────
	// Telemt becomes reachable again; the fresh healthy snapshot should clear
	// the unreachable state and return severity to "ok"/"good".
	recoveryTime := getNow().Add(5 * time.Second)
	recoverySnap := &gatewayrpc.RuntimeSnapshot{
		UseMiddleProxy:             true,
		MeRuntimeReady:             true,
		AcceptingNewConnections:    true,
		TelemtUnreachableSinceUnix: 0,
		StartupStatus:              "ready",
		StartupStage:               "serving",
		StartupProgressPct:         100,
		InitializationStatus:       "ready",
		InitializationStage:        "serving",
		InitializationProgressPct:  100,
		TransportMode:              "middle_proxy",
		Dcs:                        healthyDCs,
	}
	pushSnapshot(recoverySnap, recoveryTime)
	setNow(recoveryTime.Add(5 * time.Second))

	sev, _ = getSeverityAndReason(cookies)
	if sev != "ok" && sev != "good" {
		t.Errorf("Phase 3 (recovery): severity = %q, want %q or %q", sev, "ok", "good")
	}
}
