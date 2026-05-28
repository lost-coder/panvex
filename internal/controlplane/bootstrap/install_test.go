package bootstrap

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

type fakeInstallQueries struct {
	mu   sync.Mutex
	rows map[string]dbsqlc.GetAgentTransportRow
	last *dbsqlc.SetAgentBootstrapTokenParams
}

func (f *fakeInstallQueries) GetAgentTransport(_ context.Context, id string) (dbsqlc.GetAgentTransportRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		return dbsqlc.GetAgentTransportRow{}, sql.ErrNoRows
	}
	return row, nil
}

func (f *fakeInstallQueries) SetAgentBootstrapToken(_ context.Context, arg dbsqlc.SetAgentBootstrapTokenParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := arg
	f.last = &a
	return nil
}

func newInstallTestRouter(h *InstallCommandHandler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/agents/{id}/install-command", h.ServeHTTP)
	return r
}

func TestInstallCommandHappyPath(t *testing.T) {
	fake := &fakeInstallQueries{rows: map[string]dbsqlc.GetAgentTransportRow{
		"agent-1": {ID: "agent-1", TransportMode: "outbound", DialAddress: sql.NullString{String: "vps:8443", Valid: true}},
	}}
	h := NewInstallCommandHandler(fake, InstallCommandConfig{
		ScriptURL:  "https://example.com/install.sh",
		ScriptHash: strings.Repeat("a", 64),
		PanelCAPin: "sha256:fakepin",
		PanelCN:    "panel.example.com",
		PanelURL:   "panel.example.com:8443",
		Now:        func() time.Time { return time.Unix(1_000_000, 0) },
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/agents/agent-1/install-command", nil)
	rec := httptest.NewRecorder()
	newInstallTestRouter(h).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp InstallCommandResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	wantParts := []string{
		"curl -fsSL https://example.com/install.sh",
		"--mode=reverse",
		"--bootstrap-token=",
		"--agent-id=agent-1",
		"--listen-addr=:8443",
		"--ca-pin=sha256:fakepin",
		"--panel-cn=panel.example.com",
		"--panel-url-grpc=panel.example.com:8443",
		// Pinned-branch markers: prove the rendered command is the
		// hash-verifying form (mktemp + sha256sum + sudo -E with the
		// PANVEX_INSTALL_SCRIPT_SHA256 env var) rather than the legacy
		// `curl ... | sudo bash` shape. Guards against silent regression
		// to the unverified pipeline (HIGH-2 review feedback).
		"mktemp",
		"sha256sum",
		"PANVEX_INSTALL_SCRIPT_SHA256=",
		"sudo -E",
	}
	for _, p := range wantParts {
		if !strings.Contains(resp.Command, p) {
			t.Errorf("install command missing %q\ncmd=%s", p, resp.Command)
		}
	}
	// The legacy `| sudo bash` pipeline must NOT appear when ScriptHash is
	// set: it would imply the unverified body is being piped into a
	// privileged shell, which is exactly the MITM hole S-3 closes.
	if strings.Contains(resp.Command, "| sudo bash") {
		t.Errorf("pinned-hash command unexpectedly contains legacy `| sudo bash` pipeline\ncmd=%s", resp.Command)
	}
	if resp.ExpiresAtUnix != time.Unix(1_000_000, 0).Add(installCommandTTL).Unix() {
		t.Errorf("ExpiresAtUnix mismatch")
	}
	if fake.last == nil {
		t.Fatal("expected SetAgentBootstrapToken to be called")
	}
	if fake.last.ID != "agent-1" {
		t.Errorf("token persisted for wrong agent: %s", fake.last.ID)
	}
	if len(fake.last.BootstrapTokenHash) != 32 {
		t.Errorf("hash length = %d, want 32", len(fake.last.BootstrapTokenHash))
	}
	if !fake.last.BootstrapExpiresAt.Valid {
		t.Errorf("expiry not marked valid")
	}
}

// TestBuildInstallCommand_LegacyWhenHashEmpty drives the empty-ScriptHash
// branch of BuildInstallCommand. With no hash configured (test fixtures or
// transitional deploys that have not yet wired server.InstallScriptSHA256())
// the legacy `curl ... | sudo bash` form must be emitted and none of the
// pinned-branch markers may leak in. Guards the fallback against an over-
// eager refactor that drops the legacy shape (HIGH-2 review feedback).
func TestBuildInstallCommand_LegacyWhenHashEmpty(t *testing.T) {
	t.Parallel()
	cmd := BuildInstallCommand(InstallCommandInput{
		ScriptURL:  "https://example.com/install.sh",
		ScriptHash: "",
		Token:      "tok",
		AgentID:    "agent-1",
		ListenAddr: ":8443",
		PanelCAPin: "sha256:fakepin",
		PanelCN:    "panel.example.com",
		PanelURL:   "panel.example.com:8443",
	})
	wantLegacy := []string{
		"curl -fsSL https://example.com/install.sh",
		"| sudo bash -s --",
		"--mode=reverse",
		"--bootstrap-token=tok",
		"--agent-id=agent-1",
	}
	for _, p := range wantLegacy {
		if !strings.Contains(cmd, p) {
			t.Errorf("legacy command missing %q\ncmd=%s", p, cmd)
		}
	}
	// None of the pinned-branch markers should appear when verification
	// is disabled — they are meaningful only with a non-empty ScriptHash.
	forbidden := []string{
		"mktemp",
		"sha256sum",
		"PANVEX_INSTALL_SCRIPT_SHA256",
		"sudo -E",
	}
	for _, p := range forbidden {
		if strings.Contains(cmd, p) {
			t.Errorf("legacy command unexpectedly contains pinned marker %q\ncmd=%s", p, cmd)
		}
	}
}

// TestInstallCommandHandlerUsesResolverFns pins Plan 4: when ScriptURLFn /
// PanelURLFn are set they are evaluated PER REQUEST and take precedence over
// the static ScriptURL / PanelURL, so the server can derive both from the
// live panel settings without a restart.
func TestInstallCommandHandlerUsesResolverFns(t *testing.T) {
	fake := &fakeInstallQueries{rows: map[string]dbsqlc.GetAgentTransportRow{
		"agent-1": {ID: "agent-1", TransportMode: "outbound", DialAddress: sql.NullString{String: "vps:8443", Valid: true}},
	}}
	h := NewInstallCommandHandler(fake, InstallCommandConfig{
		// Static strings are intentionally distinct from the Fn values so a
		// failure to prefer the resolvers shows up as the static value leaking.
		ScriptURL:   "https://static.example/install-agent.sh",
		PanelURL:    "static.example:8443",
		ScriptURLFn: func(*http.Request) string { return "https://live.example/install-agent.sh" },
		PanelURLFn:  func(*http.Request) string { return "live.example:8443" },
		PanelCAPin:  "pin",
		PanelCN:     "cn",
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/agents/agent-1/install-command", nil)
	rec := httptest.NewRecorder()
	newInstallTestRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp InstallCommandResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(resp.Command, "https://live.example/install-agent.sh") {
		t.Errorf("command missing live script URL: %s", resp.Command)
	}
	if !strings.Contains(resp.Command, "--panel-url-grpc=live.example:8443") {
		t.Errorf("command missing live panel gRPC URL: %s", resp.Command)
	}
	if strings.Contains(resp.Command, "static.example") {
		t.Errorf("command leaked static URL despite resolver Fns: %s", resp.Command)
	}
}

func TestInstallCommandRejectsInboundAgent(t *testing.T) {
	fake := &fakeInstallQueries{rows: map[string]dbsqlc.GetAgentTransportRow{
		"agent-2": {ID: "agent-2", TransportMode: "inbound"},
	}}
	h := NewInstallCommandHandler(fake, InstallCommandConfig{ScriptURL: "x", PanelURL: "panel.example.com:8443"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/agents/agent-2/install-command", nil)
	rec := httptest.NewRecorder()
	newInstallTestRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if fake.last != nil {
		t.Fatal("token should not be persisted for inbound agent")
	}
}

func TestInstallCommandReturns404ForMissingAgent(t *testing.T) {
	fake := &fakeInstallQueries{rows: map[string]dbsqlc.GetAgentTransportRow{}}
	h := NewInstallCommandHandler(fake, InstallCommandConfig{ScriptURL: "x", PanelURL: "panel.example.com:8443"})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/agents/ghost/install-command", nil)
	rec := httptest.NewRecorder()
	newInstallTestRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if fake.last != nil {
		t.Fatal("token should not be persisted for missing agent")
	}
}
