package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// Wire-audit I1: createJobRequest has no payload_json field, so the generic
// POST /api/jobs endpoint could enqueue payload-REQUIRING actions (client.*,
// config.apply, switch_transport_mode) that were accepted, dispatched, and
// then failed on EVERY agent with a JSON decode error — nothing warned at
// enqueue time. The endpoint now accepts only actions it can dispatch
// correctly: the payload-free telemetry.refresh_diagnostics, plus
// agent.self-update whose payload the panel builds server-side (the
// dashboard's bulk self-update button posts here).

func newCreateJobTestServer(t *testing.T) (*Server, []*http.Cookie) {
	t.Helper()
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	t.Cleanup(server.Close)
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	server.seedLiveAgentKeyed("agent-1", Agent{
		ID:           "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams-1",
	})
	return server, loginAdminForClients(t, server)
}

func postJob(t *testing.T, server *Server, cookies []*http.Cookie, action, key string) *httptest.ResponseRecorder {
	t.Helper()
	return performJSONRequest(t, server, http.MethodPost, "/api/jobs", map[string]any{
		"action":           action,
		"target_agent_ids": []string{"agent-1"},
		"idempotency_key":  key,
		"ttl_seconds":      60,
	}, cookies)
}

func TestCreateJobRejectsPayloadCarryingActions(t *testing.T) {
	server, cookies := newCreateJobTestServer(t)

	payloadCarrying := []string{
		"client.create",
		"client.update",
		"client.delete",
		"client.rotate_secret",
		"client.reset_quota",
		"switch_transport_mode",
		"config.apply",
	}
	for _, action := range payloadCarrying {
		resp := postJob(t, server, cookies, action, "key-"+action)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("POST /api/jobs action=%s status = %d, want %d (payload-carrying action must be rejected at enqueue)", action, resp.Code, http.StatusBadRequest)
		}
		if body := resp.Body.String(); !strings.Contains(body, "dedicated") {
			t.Fatalf("POST /api/jobs action=%s body = %q, want an explanation pointing at the dedicated endpoint", action, body)
		}
	}
	if got := server.jobs.ListWithContext(context.Background()); len(got) != 0 {
		t.Fatalf("jobs enqueued = %d, want 0 (rejected actions must not enqueue)", len(got))
	}
}

func TestCreateJobAcceptsPayloadFreeActions(t *testing.T) {
	server, cookies := newCreateJobTestServer(t)

	for _, action := range []string{"telemetry.refresh_diagnostics"} {
		resp := postJob(t, server, cookies, action, "key-"+action)
		if resp.Code != http.StatusAccepted {
			t.Fatalf("POST /api/jobs action=%s status = %d, want %d (body %q)", action, resp.Code, http.StatusAccepted, resp.Body.String())
		}
	}
}

// TestCreateJobBuildsSelfUpdatePayloadServerSide: the bulk self-update button
// posts agent.self-update here with no payload; the agent REQUIRES
// {version, release_base_url}, so the panel must attach it (resolved exactly
// like the dedicated per-agent update endpoint) instead of enqueuing a job
// that fails on every agent.
func TestCreateJobBuildsSelfUpdatePayloadServerSide(t *testing.T) {
	server, cookies := newCreateJobTestServer(t)

	// No known agent version yet → honest 400 instead of a doomed job.
	resp := postJob(t, server, cookies, "agent.self-update", "key-selfupdate-noversion")
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("self-update without a known version status = %d, want %d (body %q)", resp.Code, http.StatusBadRequest, resp.Body.String())
	}

	server.settingsMu.Lock()
	server.updateState.LatestAgentVersion = "1.2.3"
	server.updateSettings.GitHubRepo = "lost-coder/panvex"
	server.settingsMu.Unlock()

	resp = postJob(t, server, cookies, "agent.self-update", "key-selfupdate")
	if resp.Code != http.StatusAccepted {
		t.Fatalf("self-update status = %d, want %d (body %q)", resp.Code, http.StatusAccepted, resp.Body.String())
	}
	var created jobs.Job
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil {
		t.Fatalf("json.Unmarshal(job): %v", err)
	}
	stored, ok := server.jobs.Get(created.ID)
	if !ok {
		t.Fatalf("job %q not found after enqueue", created.ID)
	}
	var payload struct {
		Version        string `json:"version"`
		ReleaseBaseURL string `json:"release_base_url"`
	}
	if err := json.Unmarshal([]byte(stored.PayloadJSON), &payload); err != nil {
		t.Fatalf("job PayloadJSON = %q, want the agent self-update payload: %v", stored.PayloadJSON, err)
	}
	if payload.Version != "1.2.3" {
		t.Fatalf("payload version = %q, want %q", payload.Version, "1.2.3")
	}
	if !strings.Contains(payload.ReleaseBaseURL, "lost-coder/panvex") || !strings.Contains(payload.ReleaseBaseURL, "v1.2.3") {
		t.Fatalf("payload release_base_url = %q, want repo + v1.2.3", payload.ReleaseBaseURL)
	}
}
