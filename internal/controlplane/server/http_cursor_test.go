package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestAuditCursorEndpointPaginatesNewestFirst exercises the S25 T1
// cursor branch on /api/audit. Seeds N audit rows directly into the
// store, walks the endpoint with a small limit, and asserts the page
// contents match newest-first ordering with no overlap or gaps.
//
// We seed via store.AppendAuditEvent (not the in-memory ring) because
// the cursor branch reads from storage; the legacy ring is bounded and
// would not exercise the contract on N=12 events.
func TestAuditCursorEndpointPaginatesNewestFirst(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)

	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}

	const total = 12
	for i := 0; i < total; i++ {
		event := storage.AuditEventRecord{
			ID:        fmt.Sprintf("audit-%02d", i),
			ActorID:   "user-1",
			Action:    "test.cursor",
			TargetID:  "t",
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			Details:   map[string]any{"i": i},
		}
		if err := server.store.AppendAuditEvent(context.Background(), event); err != nil {
			t.Fatalf("AppendAuditEvent: %v", err)
		}
	}

	loginResponse := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginResponse.Code)
	}
	cookies := loginResponse.Result().Cookies()

	// Walk pages: limit=4 walks our 12 seeded rows plus any side-effect
	// audits (e.g. the login.success row added by the auth handler).
	// We filter to action="test.cursor" to compare only our seeded
	// rows against the expected newest-first sequence.
	cursor := ""
	got := make([]string, 0, total)
	for page := 0; page < 8; page++ {
		path := "/api/audit?cursor=" + cursor + "&limit=4"
		resp := performJSONRequest(t, server, http.MethodGet, path, nil, cookies)
		if resp.Code != http.StatusOK {
			t.Fatalf("page %d status = %d, body = %s", page, resp.Code, resp.Body.String())
		}
		var body auditCursorResponse
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("page %d unmarshal: %v body=%s", page, err, resp.Body.String())
		}
		for _, item := range body.Items {
			if item.Action != "test.cursor" {
				continue
			}
			got = append(got, item.ID)
		}
		if body.NextCursor == "" {
			break
		}
		cursor = body.NextCursor
	}
	if len(got) != total {
		t.Fatalf("walked %d test.cursor events, want %d (sequence: %v)", len(got), total, got)
	}
	// Newest-first: index 0 should be audit-11, last should be audit-00.
	if got[0] != "audit-11" || got[total-1] != "audit-00" {
		t.Fatalf("unexpected page-walk order: first=%q last=%q full=%v", got[0], got[total-1], got)
	}
	// No duplicates.
	seen := make(map[string]bool, total)
	for _, id := range got {
		if seen[id] {
			t.Fatalf("duplicate id %q across pages: %v", id, got)
		}
		seen[id] = true
	}
}

// TestAuditCursorEndpointRejectsGarbageCursor asserts a malformed cursor
// returns 400 rather than silently restarting the page walk. This is the
// guardrail that prevents a stale-but-base64-valid cursor from quietly
// producing wrong results.
func TestAuditCursorEndpointRejectsGarbageCursor(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)

	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	loginResponse := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	cookies := loginResponse.Result().Cookies()

	// "***" is not valid base64-url payload.
	resp := performJSONRequest(t, server, http.MethodGet, "/api/audit?cursor=***", nil, cookies)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("garbage cursor status = %d, want %d (body=%s)", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

// TestAuditEndpointLegacyShapeUnchanged asserts that omitting ?cursor
// preserves the legacy response shape (a top-level JSON array, NOT an
// object with items+next_cursor). This is the backwards-compat guarantee
// the spec calls out — callers built against the array shape must not
// break when the cursor variant ships.
func TestAuditEndpointLegacyShapeUnchanged(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)

	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	loginResponse := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	cookies := loginResponse.Result().Cookies()

	resp := performJSONRequest(t, server, http.MethodGet, "/api/audit", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("legacy /api/audit status = %d", resp.Code)
	}
	var arr []AuditEvent
	if err := json.Unmarshal(resp.Body.Bytes(), &arr); err != nil {
		t.Fatalf("legacy /api/audit response is not a JSON array: %v body=%s", err, resp.Body.String())
	}
}
