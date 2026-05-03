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

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-1/install-command", nil)
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
	}
	for _, p := range wantParts {
		if !strings.Contains(resp.Command, p) {
			t.Errorf("install command missing %q\ncmd=%s", p, resp.Command)
		}
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

func TestInstallCommandRejectsInboundAgent(t *testing.T) {
	fake := &fakeInstallQueries{rows: map[string]dbsqlc.GetAgentTransportRow{
		"agent-2": {ID: "agent-2", TransportMode: "inbound"},
	}}
	h := NewInstallCommandHandler(fake, InstallCommandConfig{ScriptURL: "x", PanelURL: "panel.example.com:8443"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/agent-2/install-command", nil)
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
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/ghost/install-command", nil)
	rec := httptest.NewRecorder()
	newInstallTestRouter(h).ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if fake.last != nil {
		t.Fatal("token should not be persisted for missing agent")
	}
}
