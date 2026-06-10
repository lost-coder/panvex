package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

func TestRescanDiscoveredClientsNudgesSessions(t *testing.T) {
	now := time.Date(2026, time.June, 4, 12, 0, 0, 0, time.UTC)
	s := testServerWithSQLite(t, now)

	// Bootstrap an admin user so we can call the operator-gated endpoint.
	if _, _, err := s.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Log in and obtain session cookies.
	loginResp := performJSONRequest(t, s, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResp.Code, http.StatusOK)
	}
	cookies := loginResp.Result().Cookies()

	// Register two live sessions — both should receive the rediscovery flag.
	a, ua := s.sessions.Register("agent-a", nil)
	t.Cleanup(ua)
	b, ub := s.sessions.Register("agent-b", nil)
	t.Cleanup(ub)

	resp := performJSONRequest(t, s, http.MethodPost, "/api/discovered-clients/rescan", nil, cookies)
	if resp.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusAccepted)
	}
	var body struct {
		AgentsNotified int `json:"agents_notified"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.AgentsNotified != 2 {
		t.Fatalf("agents_notified = %d, want 2", body.AgentsNotified)
	}
	if !a.TakeRediscovery() || !b.TakeRediscovery() {
		t.Fatal("both sessions should have the rediscovery flag set")
	}
}
