package bootstrap

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// installCommandTTL bounds how long an issued bootstrap token is valid.
// 5 minutes is the S-02 upper-bound: an operator copies the curl one-liner
// and runs it immediately; a leaked token is only exploitable for a very short
// window before it expires.  Changing this constant above 5 minutes would
// violate the S-02 security requirement — see the regression test
// TestBootstrapToken_DefaultTTLIsAtMost5Minutes.
const installCommandTTL = 5 * time.Minute

// defaultListenAddr is what the agent binds to when reverse-mode is selected
// without an explicit override. :8443 mirrors the panel's gRPC listen port
// for symmetry; operators can override via the install command if needed.
const defaultListenAddr = ":8443"

// Queries is the subset of dbsqlc.Queries that InstallCommandHandler needs.
// Lives here so tests can supply a fake without depending on dbsqlc internals.
type Queries interface {
	GetAgentTransport(ctx context.Context, id string) (dbsqlc.GetAgentTransportRow, error)
	SetAgentBootstrapToken(ctx context.Context, arg dbsqlc.SetAgentBootstrapTokenParams) error
}

// InstallCommandResponse is the JSON body returned to the operator. Command
// is a one-line curl ... | sudo bash -s -- ... pre-baked with the freshly
// issued bootstrap token. ExpiresAtUnix mirrors the persisted expiry so UIs
// can show a countdown.
type InstallCommandResponse struct {
	Command       string `json:"command"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

// InstallCommandHandler issues an install command for an outbound (reverse-
// mode) agent. Each call replaces any previously-issued token.
type InstallCommandHandler struct {
	queries    Queries
	scriptURL  string
	panelCAPin string
	panelCN    string
	panelURL   string
	listenAddr string
	now        func() time.Time
}

// InstallCommandConfig groups the non-DB inputs to the handler so callers
// don't accidentally swap two strings of the same Go type.
type InstallCommandConfig struct {
	ScriptURL  string // public URL of the install-agent.sh script
	PanelCAPin string // SHA-256 fingerprint of the panel's CA cert
	PanelCN    string // CN agents use to verify the panel's TLS cert
	PanelURL   string // gRPC endpoint (host:port) agents dial when switching back to inbound mode
	ListenAddr string // agent-side listen addr; "" → defaultListenAddr
	Now        func() time.Time // injectable clock; nil → time.Now
}

// NewInstallCommandHandler constructs a handler using the provided queries and
// config. q may be nil — in that case every request returns 503 until a
// non-nil Queries is provided. cfg.ListenAddr defaults to defaultListenAddr;
// cfg.Now defaults to time.Now.
//
// cfg.PanelURL must be non-empty; it is embedded into the generated install
// command as --panel-url-grpc so reverse-bootstrapped agents can switch back
// to dial mode without re-enrolling. If PanelURL is empty the handler returns
// 503 on every request.
func NewInstallCommandHandler(q Queries, cfg InstallCommandConfig) *InstallCommandHandler {
	listen := cfg.ListenAddr
	if listen == "" {
		listen = defaultListenAddr
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &InstallCommandHandler{
		queries:    q,
		scriptURL:  cfg.ScriptURL,
		panelCAPin: cfg.PanelCAPin,
		panelCN:    cfg.PanelCN,
		panelURL:   cfg.PanelURL,
		listenAddr: listen,
		now:        now,
	}
}

func (h *InstallCommandHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.queries == nil {
		http.Error(w, "install-command endpoint not configured", http.StatusServiceUnavailable)
		return
	}
	if h.panelURL == "" {
		http.Error(w, "install-command endpoint not configured: panel_url not set", http.StatusServiceUnavailable)
		return
	}

	agentID := chi.URLParam(r, "id")
	if agentID == "" {
		http.Error(w, "agent id required", http.StatusBadRequest)
		return
	}

	row, err := h.queries.GetAgentTransport(r.Context(), agentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "agent not found", http.StatusNotFound)
			return
		}
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if row.TransportMode != "outbound" {
		http.Error(w, "install-command is only available for outbound agents", http.StatusBadRequest)
		return
	}

	issued, err := IssueToken(h.now(), installCommandTTL)
	if err != nil {
		http.Error(w, "token issue failed", http.StatusInternalServerError)
		return
	}
	if err := h.queries.SetAgentBootstrapToken(r.Context(), dbsqlc.SetAgentBootstrapTokenParams{
		ID:                 agentID,
		BootstrapTokenHash: issued.Hash[:],
		BootstrapExpiresAt: sql.NullTime{Time: issued.ExpiresAt, Valid: true},
	}); err != nil {
		http.Error(w, "token persist failed", http.StatusInternalServerError)
		return
	}

	cmd := fmt.Sprintf(
		"curl -fsSL %s | sudo bash -s -- --mode=reverse --bootstrap-token=%s --agent-id=%s --listen-addr=%s --ca-pin=%s --panel-cn=%s --panel-url-grpc=%s",
		h.scriptURL, issued.Raw, agentID, h.listenAddr, h.panelCAPin, h.panelCN, h.panelURL,
	)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(InstallCommandResponse{
		Command:       cmd,
		ExpiresAtUnix: issued.ExpiresAt.Unix(),
	}); err != nil {
		// Body partially written; can't change status.
		return
	}
}
