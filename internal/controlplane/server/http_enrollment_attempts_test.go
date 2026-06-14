package server

import (
	"encoding/json"
	"fmt"
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
		map[string]any{"node_name": "node-list", "version": "0.0.0-test", "csr_pem": testCSRPEM(t)},
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

// TestListEnrollmentAttemptsFiltersByMode drives a happy-path inbound
// bootstrap and then asserts that mode=outbound returns zero items —
// the only persisted attempt is inbound. This exercises the new
// ?mode= query string wired up by Task 17.
func TestListEnrollmentAttemptsFiltersByMode(t *testing.T) {
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
		map[string]any{"node_name": "node-mode", "version": "0.0.0-test", "csr_pem": testCSRPEM(t)},
		nil,
		map[string]string{"Authorization": "Bearer " + token.Value},
	)
	if bootstrap.Code != http.StatusOK {
		t.Fatalf("bootstrap status = %d, body = %s", bootstrap.Code, bootstrap.Body.String())
	}

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/enrollment-attempts?mode=outbound&limit=20", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var payload struct {
		Items      []map[string]any `json:"items"`
		NextCursor any              `json:"next_cursor"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Items) != 0 {
		t.Fatalf("expected 0 outbound attempts, got %d: %s", len(payload.Items), resp.Body.String())
	}
	if payload.NextCursor != nil {
		t.Fatalf("next_cursor = %v, want nil", payload.NextCursor)
	}

	// Inbound filter must still surface the just-recorded attempt.
	respIn := performJSONRequest(t, srv, http.MethodGet, "/api/enrollment-attempts?mode=inbound&limit=20", nil, cookies)
	if respIn.Code != http.StatusOK {
		t.Fatalf("inbound status = %d, body = %s", respIn.Code, respIn.Body.String())
	}
	var inboundPayload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(respIn.Body.Bytes(), &inboundPayload); err != nil {
		t.Fatalf("decode inbound: %v", err)
	}
	if len(inboundPayload.Items) == 0 {
		t.Fatalf("expected >=1 inbound attempt, got 0")
	}
}

// TestListEnrollmentAttemptsCursorRoundTrip drives three successful
// bootstraps then walks the list endpoint with limit=2 and the cursor
// returned by the first page. Page 2 must contain the remaining items
// and a nil next_cursor.
func TestListEnrollmentAttemptsCursorRoundTrip(t *testing.T) {
	now := time.Date(2026, time.May, 4, 12, 0, 0, 0, time.UTC)
	srv, _, _ := newEnrollmentRecorderTestServer(t, now)
	cookies := loginAs(t, srv, now, "admin", "Admin1password", auth.RoleAdmin)

	for i := 0; i < 3; i++ {
		token, err := srv.issueEnrollmentToken(security.EnrollmentScope{
			FleetGroupID: "default",
			TTL:          time.Minute,
		}, now)
		if err != nil {
			t.Fatalf("issueEnrollmentToken(%d) error = %v", i, err)
		}
		bootstrap := performJSONRequestWithHeaders(
			t,
			srv,
			http.MethodPost,
			"/api/agent/bootstrap",
			map[string]any{"node_name": fmt.Sprintf("node-cur-%d", i), "version": "0.0.0-test", "csr_pem": testCSRPEM(t)},
			nil,
			map[string]string{"Authorization": "Bearer " + token.Value},
		)
		if bootstrap.Code != http.StatusOK {
			t.Fatalf("bootstrap %d status = %d, body = %s", i, bootstrap.Code, bootstrap.Body.String())
		}
	}

	resp1 := performJSONRequest(t, srv, http.MethodGet, "/api/enrollment-attempts?limit=2", nil, cookies)
	if resp1.Code != http.StatusOK {
		t.Fatalf("page1 status = %d, body = %s", resp1.Code, resp1.Body.String())
	}
	var page1 struct {
		Items      []map[string]any `json:"items"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(resp1.Body.Bytes(), &page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("page1 items = %d, want 2; body = %s", len(page1.Items), resp1.Body.String())
	}
	if page1.NextCursor == "" {
		t.Fatalf("page1 next_cursor empty; body = %s", resp1.Body.String())
	}

	resp2 := performJSONRequest(t, srv, http.MethodGet,
		"/api/enrollment-attempts?limit=2&cursor="+page1.NextCursor, nil, cookies)
	if resp2.Code != http.StatusOK {
		t.Fatalf("page2 status = %d, body = %s", resp2.Code, resp2.Body.String())
	}
	var page2 struct {
		Items      []map[string]any `json:"items"`
		NextCursor any              `json:"next_cursor"`
	}
	if err := json.Unmarshal(resp2.Body.Bytes(), &page2); err != nil {
		t.Fatalf("decode page2: %v", err)
	}
	if len(page2.Items) < 1 {
		t.Fatalf("page2 empty: %s", resp2.Body.String())
	}
	if page2.NextCursor != nil {
		t.Fatalf("page2 next_cursor = %v, want nil (end of results)", page2.NextCursor)
	}

	// Sanity: the two pages must not overlap by id.
	seen := map[string]bool{}
	for _, it := range page1.Items {
		id, _ := it["id"].(string)
		seen[id] = true
	}
	for _, it := range page2.Items {
		id, _ := it["id"].(string)
		if seen[id] {
			t.Fatalf("page2 id %q already present on page1", id)
		}
	}
}
