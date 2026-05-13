package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/security"
)

// TestListEnrollmentAttemptsReturnsRecent drives a full happy-path
// bootstrap through the inbound flow (Task 14 territory) and then asserts
// that the read-side endpoint surfaces the attempt to an authenticated
// admin session. The test runs through real cookie-backed auth — the
// existing loginAs helper already gives us a session that satisfies
// requireAuthenticatedSession + CSRF (GET is unguarded for CSRF anyway).
func TestListEnrollmentAttemptsReturnsRecent(t *testing.T) {
	now := time.Date(2026, time.May, 4, 12, 0, 0, 0, time.UTC)
	srv, _, _ := newEnrollmentRecorderTestServer(t, now)

	cookies := loginAs(t, srv, now, "admin", "Admin1password", auth.RoleAdmin)

	token, err := srv.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "default",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}
	bootstrap := performJSONRequestWithHeaders(
		t,
		srv,
		http.MethodPost,
		"/api/agent/bootstrap",
		map[string]any{"node_name": "node-list", "version": "0.0.0-test"},
		nil,
		map[string]string{"Authorization": "Bearer " + token.Value},
	)
	if bootstrap.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d, body = %s", bootstrap.Code, bootstrap.Body.String())
	}
	attemptID := attemptIDFromBootstrapBody(bootstrap.Body.Bytes())
	if attemptID == "" {
		t.Fatalf("attempt_id missing from bootstrap response: %s", bootstrap.Body.String())
	}

	listResp := performJSONRequest(t, srv, http.MethodGet, "/api/enrollment-attempts?limit=10", nil, cookies)
	if listResp.Code != http.StatusOK {
		t.Fatalf("GET /api/enrollment-attempts status = %d, body = %s", listResp.Code, listResp.Body.String())
	}
	var listPayload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listPayload.Items) == 0 {
		t.Fatalf("expected at least one attempt, got 0")
	}
	if id, _ := listPayload.Items[0]["id"].(string); id != attemptID {
		t.Errorf("first item id = %q, want %q", id, attemptID)
	}
	if status, _ := listPayload.Items[0]["status"].(string); status != "success" {
		t.Errorf("first item status = %q, want %q", status, "success")
	}

	detailResp := performJSONRequest(t, srv, http.MethodGet, "/api/enrollment-attempts/"+attemptID, nil, cookies)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("GET /api/enrollment-attempts/{id} status = %d, body = %s", detailResp.Code, detailResp.Body.String())
	}
	var detail struct {
		Attempt map[string]any   `json:"attempt"`
		Events  []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(detailResp.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if id, _ := detail.Attempt["id"].(string); id != attemptID {
		t.Errorf("detail attempt.id = %q, want %q", id, attemptID)
	}
	if len(detail.Events) == 0 {
		t.Fatalf("expected at least one event in detail, got 0")
	}
}

// TestListEnrollmentAttemptsRequiresAuth verifies the panel session
// middleware is wired correctly — an anonymous GET must return 401, not
// fall through to the handler.
func TestListEnrollmentAttemptsRequiresAuth(t *testing.T) {
	now := time.Date(2026, time.May, 4, 12, 0, 0, 0, time.UTC)
	srv, _, _ := newEnrollmentRecorderTestServer(t, now)

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/enrollment-attempts", nil, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET status = %d, want 401; body = %s", resp.Code, resp.Body.String())
	}
}

// TestGetEnrollmentAttemptNotFound asserts that an unknown id returns
// 404 rather than 500. The route parses the UUID inside the SQLStore;
// a syntactically-valid-but-unknown UUID is the cleanest case to assert
// on without depending on chi's path matcher.
func TestGetEnrollmentAttemptNotFound(t *testing.T) {
	now := time.Date(2026, time.May, 4, 12, 0, 0, 0, time.UTC)
	srv, _, _ := newEnrollmentRecorderTestServer(t, now)
	cookies := loginAs(t, srv, now, "admin", "Admin1password", auth.RoleAdmin)

	resp := performJSONRequest(t, srv, http.MethodGet,
		"/api/enrollment-attempts/00000000-0000-0000-0000-000000000000", nil, cookies)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("missing-attempt status = %d, want 404; body = %s", resp.Code, resp.Body.String())
	}
}
