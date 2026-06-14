package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

// TestAuditRequiresOperatorRole verifies that GET /api/audit is restricted to
// operator-role (or above). Viewer must receive 403; operator must receive 200.
// (Task 14 — A5 follow-up: least-privilege on the audit log.)
func TestAuditRequiresOperatorRole(t *testing.T) {
	now := time.Date(2026, time.June, 10, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)

	// Bootstrap admin first (loginAs cannot create users without an existing
	// admin — loginAs uses BootstrapUser which is idempotent and does not
	// require a prior admin for the first call).
	viewerCookies := loginAs(t, server, now, "viewer", "Viewer1password", auth.RoleViewer)

	resp := performJSONRequest(t, server, http.MethodGet, "/api/audit", nil, viewerCookies)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("viewer GET /api/audit = %d, want 403", resp.Code)
	}

	operatorCookies := loginAs(t, server, now, "operator", "Operator1password", auth.RoleOperator)

	resp = performJSONRequest(t, server, http.MethodGet, "/api/audit", nil, operatorCookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("operator GET /api/audit = %d, want 200", resp.Code)
	}
}
