package server

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/bootstrap"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/dbsqlc"
)

// ProvisionOutboundDeps groups the runtime dependencies the provision-
// outbound handler needs beyond Server state. Wired in cmd/control-plane
// via Server.SetProvisionOutboundDeps so test fixtures that construct
// Server directly remain unaffected (nil deps make the handler 503).
//
// The struct duplicates fields with bootstrap.InstallCommandConfig
// deliberately: the install-command handler stores its config inside a
// closed-over *bootstrap.InstallCommandHandler we cannot peek into, so
// the provision-outbound handler needs its own typed copy of the same
// values to build the curl|sudo-bash one-liner.
type ProvisionOutboundDeps struct {
	// Queries is the typed sqlc surface for the agents/transport tables.
	// Required; nil here behaves identically to deps==nil at the route
	// level (handler returns 503).
	Queries *dbsqlc.Queries
	// PanelScriptURL is the URL embedded into the curl when the operator
	// picks `script_source=panel` — typically `<panel>/install-agent.sh`
	// or the PANVEX_INSTALL_SCRIPT_URL override.
	PanelScriptURL string
	// PanelScriptHash is the lowercase hex SHA-256 of the panel-served
	// install script body. Non-empty enables the temp-file + sha256sum
	// curl form; an empty string falls back to the legacy `curl|bash`
	// form (matches BuildInstallCommand semantics).
	PanelScriptHash string
	// GitHubScriptURL is the URL embedded into the curl when the
	// operator picks `script_source=github`. We never carry a hash for
	// the GitHub source — the panel cannot vouch for upstream bytes.
	GitHubScriptURL string
	// PanelCAPin / PanelCN / PanelGRPCURL replay the install-command
	// flags the agent's bootstrap subcommand consumes; mirror values
	// used by InstallCommandHandler.
	PanelCAPin   string
	PanelCN      string
	PanelGRPCURL string
	// Now is an injectable clock so tests can pin token issuance
	// timestamps deterministically. nil → time.Now.
	Now func() time.Time
}

// agentNodeNamePattern restricts the operator-supplied node_name to a
// character class that is safe to interpolate as a shell CLI flag value
// and to embed in URL paths / audit-event targets. Matches the wizard's
// client-side isValidNodeName regex so the rendered command never
// disagrees with what the operator just typed.
var agentNodeNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

const provisionAgentNodeNameMaxLen = 64

type provisionOutboundAgentRequest struct {
	NodeName     string `json:"node_name"`
	FleetGroupID string `json:"fleet_group_id"`
	DialAddress  string `json:"dial_address"`
	// ScriptSource = "panel" or "github". Defaults to "github" when
	// unset because outbound mode implies the panel is firewalled from
	// the agent host (otherwise the operator would use inbound).
	ScriptSource string `json:"script_source,omitempty"`
	Advanced     *struct {
		// Mirrors openapi.InstallCommandAdvancedOptions. Defined inline
		// (rather than imported from openapi.gen.go) so the JSON
		// decoder treats unknown future fields as no-ops without a
		// regen cycle.
		TelemtURL         *string `json:"telemt_url,omitempty"`
		TelemtMetricsURL  *string `json:"telemt_metrics_url,omitempty"`
		TelemtAuth        *string `json:"telemt_auth,omitempty"`
		InsecureTransport *bool   `json:"insecure_transport,omitempty"`
	} `json:"advanced,omitempty"`
}

type provisionOutboundAgentResponse struct {
	AgentID       string `json:"agent_id"`
	Command       string `json:"command"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
	ScriptURL     string `json:"script_url"`
}

// SetProvisionOutboundDeps wires the deps the provision-outbound
// handler needs. Safe to call concurrently with HTTP requests; nil
// deps cause the route to return 503 until the next call (mirrors
// SetInstallCommandHandler).
func (s *Server) SetProvisionOutboundDeps(d *ProvisionOutboundDeps) {
	s.provisionOutbound.Store(d)
}

func (s *Server) handleProvisionOutboundAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		// requireMinimumRole(auth.RoleAdmin) on the route group guards
		// against operator/viewer access. Belt-and-braces: re-check
		// here so a future routes.go refactor that drops the middleware
		// fails closed instead of silently exposing the endpoint.
		if user.Role != auth.RoleAdmin {
			writeError(w, http.StatusForbidden, "admin role required")
			return
		}

		deps := s.provisionOutbound.Load()
		if deps == nil || deps.Queries == nil {
			http.Error(w, "provision-outbound endpoint not configured", http.StatusServiceUnavailable)
			return
		}

		var req provisionOutboundAgentRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		req.NodeName = strings.TrimSpace(req.NodeName)
		req.DialAddress = strings.TrimSpace(req.DialAddress)
		req.FleetGroupID = strings.TrimSpace(req.FleetGroupID)
		req.ScriptSource = strings.TrimSpace(req.ScriptSource)

		if !isValidAgentNodeName(req.NodeName) {
			writeError(w, http.StatusBadRequest, "node_name must be 1-64 chars matching [A-Za-z0-9._-]")
			return
		}
		host, port, splitErr := net.SplitHostPort(req.DialAddress)
		if splitErr != nil || host == "" || port == "" {
			writeError(w, http.StatusBadRequest, "dial_address must be host:port")
			return
		}
		// Derive the listen-bind from dial_address port — the agent
		// must bind locally (":<port>"); the public host is what the
		// panel dials. Same convention as handleUpdateAgentTransportMode.
		listenBind := ":" + port

		// ScriptSource default: github. Outbound mode means panel ↔ agent
		// path is firewalled; defaulting to panel would yield a curl the
		// agent host cannot resolve.
		scriptSource := req.ScriptSource
		if scriptSource == "" {
			scriptSource = "github"
		}
		var (
			scriptURL  string
			scriptHash string
		)
		switch scriptSource {
		case "panel":
			scriptURL = deps.PanelScriptURL
			scriptHash = deps.PanelScriptHash
		case "github":
			scriptURL = deps.GitHubScriptURL
			scriptHash = "" // panel cannot vouch for upstream bytes
		default:
			writeError(w, http.StatusBadRequest, "script_source must be 'panel' or 'github'")
			return
		}
		if scriptURL == "" {
			http.Error(w, "install-script URL not configured for chosen source", http.StatusServiceUnavailable)
			return
		}

		// Resolve fleet-group + scope. Same helper as the inbound
		// enrollment-token path so operators see consistent error UX.
		fleetGroupID, ok := s.resolveAndAuthorizeEnrollmentScope(w, r, user, req.FleetGroupID)
		if !ok {
			return
		}

		now := deps.Now
		if now == nil {
			now = time.Now
		}
		nowT := now().UTC()

		agentID := uuid.NewString()

		// Persist via the storage interface (PutAgent + UpdateAgentTransportMode)
		// rather than a custom sqlc query: the sqlc-generated SQL targets the
		// postgres column names (`last_seen_at`, etc.), but the sqlite store
		// uses parallel column names (`last_seen_at_unix`, INTEGER). The
		// storage layer hides the difference. LastSeenAt is the "never seen"
		// sentinel (Unix epoch) — presence views sort the row to the bottom
		// and the cleanup sweep keys on it.
		if s.store == nil {
			http.Error(w, "persistent store required", http.StatusServiceUnavailable)
			return
		}
		if err := s.store.PutAgent(r.Context(), storage.AgentRecord{
			ID:           agentID,
			NodeName:     req.NodeName,
			FleetGroupID: fleetGroupID,
			LastSeenAt:   time.Unix(0, 0).UTC(),
		}); err != nil {
			s.logger.Error("insert outbound agent failed", "error", err, "node_name", req.NodeName)
			writeError(w, http.StatusInternalServerError, msgStorageError)
			return
		}
		if err := s.store.UpdateAgentTransportMode(r.Context(), agentID, "outbound", req.DialAddress); err != nil {
			s.logger.Error("set outbound transport mode failed", "error", err, "agent_id", agentID)
			_ = s.deleteProvisionedOutboundAgent(r.Context(), agentID)
			writeError(w, http.StatusInternalServerError, msgStorageError)
			return
		}

		// Issue a 5-minute bootstrap token + persist its hash. Same
		// flow as bootstrap.InstallCommandHandler — we re-use IssueToken
		// directly rather than the handler so we can apply our chosen
		// script URL when calling BuildInstallCommand below.
		issued, err := bootstrap.IssueToken(nowT, 5*time.Minute)
		if err != nil {
			s.logger.Error("issue bootstrap token failed", "agent_id", agentID, "error", err)
			// Best-effort cleanup: drop the row so the operator can retry
			// without colliding on the (eventually-uniqued) node_name.
			_ = s.deleteProvisionedOutboundAgent(r.Context(), agentID)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		if err := deps.Queries.SetAgentBootstrapToken(r.Context(), dbsqlc.SetAgentBootstrapTokenParams{
			ID:                 agentID,
			BootstrapTokenHash: issued.Hash[:],
			BootstrapExpiresAt: sql.NullTime{Time: issued.ExpiresAt, Valid: true},
		}); err != nil {
			s.logger.Error("persist bootstrap token failed", "agent_id", agentID, "error", err)
			_ = s.deleteProvisionedOutboundAgent(r.Context(), agentID)
			writeError(w, http.StatusInternalServerError, msgStorageError)
			return
		}

		// Render the curl|sudo-bash one-liner. With a non-empty hash
		// BuildInstallCommand emits the temp-file + sha256sum form so
		// the operator's shell verifies the body before sudo bash.
		cmd := bootstrap.BuildInstallCommand(bootstrap.InstallCommandInput{
			ScriptURL:  scriptURL,
			ScriptHash: scriptHash,
			Token:      issued.Raw,
			AgentID:    agentID,
			ListenAddr: listenBind,
			PanelCAPin: deps.PanelCAPin,
			PanelCN:    deps.PanelCN,
			PanelURL:   deps.PanelGRPCURL,
		})

		s.appendAuditWithContext(r.Context(), session.UserID, "agents.provision_outbound", agentID, map[string]any{
			"node_name":      req.NodeName,
			"fleet_group_id": fleetGroupID,
			"dial_address":   req.DialAddress,
			"script_source":  scriptSource,
		})

		writeJSON(w, http.StatusCreated, provisionOutboundAgentResponse{
			AgentID:       agentID,
			Command:       cmd,
			ExpiresAtUnix: issued.ExpiresAt.Unix(),
			ScriptURL:     scriptURL,
		})
	}
}

// deleteProvisionedOutboundAgent removes a partially-provisioned agent
// row when a follow-on step (token issuance / persistence) fails. Best-
// effort: an error here only gets logged because the original failure is
// already the user-facing error. If the row sticks around it will be
// caught by the sweep that prunes outbound rows with expired bootstrap
// tokens and no first-connection.
func (s *Server) deleteProvisionedOutboundAgent(ctx context.Context, agentID string) error {
	if s.store == nil {
		return nil
	}
	if err := s.store.DeleteAgent(ctx, agentID); err != nil && !errors.Is(err, storage.ErrNotFound) {
		s.logger.Warn("rollback provisioned outbound agent failed",
			"agent_id", agentID, "error", err)
		return err
	}
	return nil
}

// isValidAgentNodeName mirrors the wizard's client-side validator: 1-64
// chars from the safe class. Defence-in-depth — the regex is also the
// last line of defence against a malicious value reaching the install
// command's argv.
func isValidAgentNodeName(name string) bool {
	if name == "" || len(name) > provisionAgentNodeNameMaxLen {
		return false
	}
	return agentNodeNamePattern.MatchString(name)
}

