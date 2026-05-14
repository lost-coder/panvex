package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/runtimeevents"
)

// TestHTTPRuntimeEventsReturnsBuffer seeds the per-agent ring buffer
// directly and then asserts the authenticated GET endpoint surfaces
// the events newest-first. The test reuses the same SQLite-backed
// fixture as the other authenticated HTTP tests
// (newEnrollmentRecorderTestServer) so requireAuthenticatedSession +
// CSRF middleware are wired exactly as in production.
func TestHTTPRuntimeEventsReturnsBuffer(t *testing.T) {
	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	srv, _, _ := newEnrollmentRecorderTestServer(t, now)
	cookies := loginAs(t, srv, now, "admin", "Admin1password", auth.RoleAdmin)

	srv.runtimeEvents.AppendBatch("agent-1", []runtimeevents.Event{
		{Ts: time.Unix(1, 0), Level: "info", Message: "first"},
		{Ts: time.Unix(2, 0), Level: "warn", Message: "second"},
	})

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/agents/agent-1/runtime-events", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(payload.Items))
	}
	if msg, _ := payload.Items[0]["message"].(string); msg != "second" {
		t.Fatalf("newest-first violated: items[0].message = %q, want %q (items = %+v)", msg, "second", payload.Items)
	}
	if lvl, _ := payload.Items[0]["level"].(string); lvl != "warn" {
		t.Fatalf("items[0].level = %q, want %q", lvl, "warn")
	}
}

// TestHTTPRuntimeEventsFiltersByLevel asserts that the ?level=
// query-parameter narrows the response to the requested slog levels.
// We expect the warn + error rows to come back, ordered newest-first.
func TestHTTPRuntimeEventsFiltersByLevel(t *testing.T) {
	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	srv, _, _ := newEnrollmentRecorderTestServer(t, now)
	cookies := loginAs(t, srv, now, "admin", "Admin1password", auth.RoleAdmin)

	srv.runtimeEvents.AppendBatch("agent-2", []runtimeevents.Event{
		{Ts: time.Unix(1, 0), Level: "info", Message: "i"},
		{Ts: time.Unix(2, 0), Level: "warn", Message: "w"},
		{Ts: time.Unix(3, 0), Level: "error", Message: "e"},
	})

	resp := performJSONRequest(t, srv, http.MethodGet,
		"/api/agents/agent-2/runtime-events?level=warn,error", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("got %d items, want 2 (warn + error); payload = %+v", len(payload.Items), payload.Items)
	}
	// Newest-first: error (ts=3) then warn (ts=2). Info (ts=1) must be
	// absent.
	if msg, _ := payload.Items[0]["message"].(string); msg != "e" {
		t.Fatalf("items[0].message = %q, want %q", msg, "e")
	}
	if msg, _ := payload.Items[1]["message"].(string); msg != "w" {
		t.Fatalf("items[1].message = %q, want %q", msg, "w")
	}
}

// TestHTTPRuntimeEventsEmptyForUnknownAgent asserts that an
// agent_id with no entries in the ring buffer returns 200 + an empty
// items array, NOT 404. The dashboard polls freshly-enrolled agents
// before their first slog batch arrives and must not see a hard error
// during that race window.
func TestHTTPRuntimeEventsEmptyForUnknownAgent(t *testing.T) {
	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	srv, _, _ := newEnrollmentRecorderTestServer(t, now)
	cookies := loginAs(t, srv, now, "admin", "Admin1password", auth.RoleAdmin)

	resp := performJSONRequest(t, srv, http.MethodGet,
		"/api/agents/never-seen/runtime-events", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 0 {
		t.Fatalf("got %d items, want 0", len(payload.Items))
	}
}

// TestHTTPRuntimeEventsRequiresAuth verifies the panel-session
// middleware is wired in front of the new route — an anonymous GET
// must return 401, not fall through to the handler.
func TestHTTPRuntimeEventsRequiresAuth(t *testing.T) {
	now := time.Date(2026, time.May, 14, 12, 0, 0, 0, time.UTC)
	srv, _, _ := newEnrollmentRecorderTestServer(t, now)

	resp := performJSONRequest(t, srv, http.MethodGet,
		"/api/agents/agent-1/runtime-events", nil, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET status = %d, want 401; body = %s", resp.Code, resp.Body.String())
	}
}
