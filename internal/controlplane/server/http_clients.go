package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type clientMutationRequest struct {
	Name      string `json:"name"`
	Enabled   *bool  `json:"enabled"`
	UserADTag string `json:"user_ad_tag"`
	// UserADTagAuto defaults to nil (legacy auto-generation when tag
	// is empty). Pass `false` explicitly to store an empty ad tag —
	// operators who want a client WITHOUT a tag rely on this flag.
	UserADTagAuto     *bool    `json:"user_ad_tag_auto,omitempty"`
	MaxTCPConns       int      `json:"max_tcp_conns"`
	MaxUniqueIPs      int      `json:"max_unique_ips"`
	DataQuotaBytes    int64    `json:"data_quota_bytes"`
	ExpirationRFC3339 string   `json:"expiration_rfc3339"`
	FleetGroupIDs     []string `json:"fleet_group_ids"`
	AgentIDs          []string `json:"agent_ids"`
}

// toMutationInput converts a client mutation request into the input record
// expected by createClient / updateClient.
func (r clientMutationRequest) toMutationInput() clientMutationInput {
	return clientMutationInput{
		Name:              r.Name,
		Enabled:           r.Enabled,
		UserADTag:         r.UserADTag,
		UserADTagAuto:     r.UserADTagAuto,
		MaxTCPConns:       r.MaxTCPConns,
		MaxUniqueIPs:      r.MaxUniqueIPs,
		DataQuotaBytes:    r.DataQuotaBytes,
		ExpirationRFC3339: r.ExpirationRFC3339,
		FleetGroupIDs:     r.FleetGroupIDs,
		AgentIDs:          r.AgentIDs,
	}
}

type clientListResponse struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Enabled            bool   `json:"enabled"`
	AssignedNodesCount int    `json:"assigned_nodes_count"`
	ExpirationRFC3339  string `json:"expiration_rfc3339"`
	TrafficUsedBytes   uint64 `json:"traffic_used_bytes"`
	UniqueIPsUsed      int    `json:"unique_ips_used"`
	ActiveTCPConns     int    `json:"active_tcp_conns"`
	DataQuotaBytes     int64  `json:"data_quota_bytes"`
	LastDeployStatus   string `json:"last_deploy_status"`
}

type clientDeploymentResponse struct {
	AgentID          string   `json:"agent_id"`
	DesiredOperation string   `json:"desired_operation"`
	Status           string   `json:"status"`
	LastError        string   `json:"last_error"`
	ConnectionLinks  []string `json:"connection_links"`
	// LinkDiagnostic is an operator-facing warning for an otherwise
	// successful apply (IN-M2). Empty means "no issue". Non-empty when
	// the node returned no connection links on a non-delete success, so
	// ConnectionLinks may be stale after a host/secret change.
	LinkDiagnostic string `json:"link_diagnostic"`
	LastAppliedAt  int64  `json:"last_applied_at_unix"`
	UpdatedAt      int64  `json:"updated_at_unix"`
	// QuotaUsedBytes is the bytes-since-last-reset counter Telemt
	// compares against the per-client data_quota_bytes limit. Zero
	// when this agent has never reported traffic for the client or
	// when the agent runs against Telemt < 3.4.12 (the source
	// endpoint /v1/users/quota does not exist there).
	QuotaUsedBytes     uint64 `json:"quota_used_bytes"`
	QuotaLastResetUnix uint64 `json:"quota_last_reset_unix"`
	// PanelLastResetUnix is the unix-seconds value captured the last
	// time a panel-driven client.reset_quota job completed
	// successfully on this (client, agent). Zero means the panel has
	// never reset this pair. Compared with QuotaLastResetUnix on the
	// UI to surface reset history and drift (see QuotaResetDrift).
	PanelLastResetUnix uint64 `json:"panel_last_reset_unix"`
	// QuotaResetDrift is true when the panel has recorded a more
	// recent reset than Telemt currently reports — i.e. our reset
	// job landed but Telemt's persisted state has fallen behind
	// (Telemt restart before sidecar flush, sidecar wipe, etc.).
	// The UI uses this to surface "Quota reset did not stick on
	// {agent}". Both timestamps must be non-zero for the flag to
	// fire so a not-yet-reported agent (QuotaLastResetUnix == 0)
	// does not trigger a false positive on first deploy.
	QuotaResetDrift bool `json:"quota_reset_drift"`
}

type clientDetailResponse struct {
	ID                string                     `json:"id"`
	Name              string                     `json:"name"`
	Secret            string                     `json:"secret,omitempty"`
	UserADTag         string                     `json:"user_ad_tag"`
	Enabled           bool                       `json:"enabled"`
	TrafficUsedBytes  uint64                     `json:"traffic_used_bytes"`
	UniqueIPsUsed     int                        `json:"unique_ips_used"`
	ActiveTCPConns    int                        `json:"active_tcp_conns"`
	MaxTCPConns       int                        `json:"max_tcp_conns"`
	MaxUniqueIPs      int                        `json:"max_unique_ips"`
	DataQuotaBytes    int64                      `json:"data_quota_bytes"`
	ExpirationRFC3339 string                     `json:"expiration_rfc3339"`
	FleetGroupIDs     []string                   `json:"fleet_group_ids"`
	AgentIDs          []string                   `json:"agent_ids"`
	Deployments       []clientDeploymentResponse `json:"deployments"`
	// SubscriptionURL is the client's public subscription page, "" when the
	// subscription listener has no public base URL configured or the client has
	// no token yet (legacy row — operator must rotate to generate one).
	SubscriptionURL string `json:"subscription_url"`
	CreatedAt       int64  `json:"created_at_unix"`
	UpdatedAt       int64  `json:"updated_at_unix"`
	DeletedAt       int64  `json:"deleted_at_unix"`
}

func (s *Server) handleClients() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		// Q2.U-P-XX (M-1): one bulk snapshot of the clients.Service mirror
		// instead of N×clientDetailSnapshot + N×aggregatedClientUsage. On a
		// 500-client fleet that collapses ~1500 lock-acquire pairs into one.
		listing := s.listClientsListingSnapshot()
		uniqueIPCounts := s.bulkUniqueIPCountsForClients(r.Context(), listing.clients)

		response := make([]clientListResponse, 0, len(listing.clients))
		for _, client := range listing.clients {
			row, included := s.buildClientListRow(
				client,
				scope,
				listing.assignments[string(client.ID)],
				listing.deployments[string(client.ID)],
				listing.usage[string(client.ID)],
				uniqueIPCounts,
			)
			if !included {
				continue
			}
			response = append(response, row)
		}

		writeJSON(w, http.StatusOK, response)
	}
}

// clientListingSnapshot bundles every field handleClients needs into a
// single read so the per-row loop runs lock-free. Each map is keyed by
// client id; missing keys mean "no assignments/deployments/usage on
// record" — callers must not assume the slices are present.
type clientListingSnapshot struct {
	clients     []managedClient
	assignments map[string][]managedClientAssignment
	deployments map[string][]managedClientDeployment
	usage       map[string]aggregatedClientUsage
}

func (s *Server) handleCreateClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		var request clientMutationRequest
		if err := decodeJSON(r, &request); err != nil {
			writeErrorLogged(r.Context(), w, http.StatusBadRequest, "invalid client payload", err)
			return
		}

		if !validateRequestedFleetGroupScope(w, scope, request.FleetGroupIDs) {
			return
		}

		client, assignments, deployments, err := s.createClient(r.Context(), session.UserID, request.toMutationInput(), s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.InfoContext(r.Context(), "client created", "client_id", client.ID, "name", client.Name, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.create", string(client.ID), map[string]any{
			"name":             client.Name,
			"enabled":          client.Enabled,
			"fleet_group_ids":  assignmentFleetGroupIDs(assignments),
			"agent_ids":        assignmentAgentIDs(assignments),
			"target_agent_ids": deploymentAgentIDsFromResponses(deployments),
		})
		writeJSON(w, http.StatusCreated, s.buildClientDetailResponse(r.Context(), client, assignments, deployments, true))
	}
}

func (s *Server) handleClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, msgClientIDRequired)
			return
		}

		client, assignments, deployments, err := s.clientDetailSnapshot(clientID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeErrorLogged(r.Context(), w, http.StatusNotFound, err.Error(), err)
				return
			}
			s.logger.ErrorContext(r.Context(), "load client failed", "client_id", clientID, "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}

		// R-S-14: 404 instead of 403 to avoid leaking existence.
		if !s.clientInScope(scope, assignments) {
			writeError(w, http.StatusNotFound, msgClientNotFound)
			return
		}

		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(r.Context(), client, assignments, deployments, false))
	}
}

func (s *Server) handleUpdateClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, msgClientIDRequired)
			return
		}

		// R-S-14: scope-check both the existing client and any new
		// fleet groups the update wants to introduce.
		if !s.ensureClientMutationScope(r.Context(), w, clientID, scope) {
			return
		}

		var request clientMutationRequest
		if err := decodeJSON(r, &request); err != nil {
			writeErrorLogged(r.Context(), w, http.StatusBadRequest, "invalid client payload", err)
			return
		}

		if !validateRequestedFleetGroupScope(w, scope, request.FleetGroupIDs) {
			return
		}

		client, assignments, deployments, err := s.updateClient(r.Context(), clientID, session.UserID, request.toMutationInput(), s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.InfoContext(r.Context(), "client updated", "client_id", client.ID, "name", client.Name, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.update", string(client.ID), map[string]any{
			"name":            client.Name,
			"fleet_group_ids": assignmentFleetGroupIDs(assignments),
			"agent_ids":       assignmentAgentIDs(assignments),
		})
		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(r.Context(), client, assignments, deployments, false))
	}
}

func (s *Server) handleDeleteClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, msgClientIDRequired)
			return
		}

		// R-S-14: scope-check before delete.
		if !s.ensureClientMutationScope(r.Context(), w, clientID, scope) {
			return
		}

		if err := s.deleteClient(r.Context(), clientID, session.UserID, s.now()); err != nil {
			handleClientMutationError(w, err)
			return
		}

		s.logger.InfoContext(r.Context(), "client deleted", "client_id", clientID, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.delete", clientID, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

// bulkClientAction is the action portion of a bulk client request. The
// frontend uses these verbs verbatim — keep them stable across releases.
type bulkClientAction string

const (
	bulkClientEnable  bulkClientAction = "enable"
	bulkClientDisable bulkClientAction = "disable"
	bulkClientDelete  bulkClientAction = "delete"
)

// bulkClientRequest carries a list of client IDs and the action to apply
// to each. Limited to bulkClientMaxIDs entries per request so a wedged
// agent or runaway script does not pin the clients lock for an
// unbounded interval.
type bulkClientRequest struct {
	Action bulkClientAction `json:"action"`
	IDs    []string         `json:"ids"`
}

type bulkClientFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
	// Retryable distinguishes an operational failure (a real DB/storage
	// error, a job-dispatch error, etc.) from a legitimate per-item
	// "doesn't exist" or "outside scope" result (3.13). Omitted (absent /
	// false) for the common not-found case so existing frontend consumers
	// that only read id/error are unaffected; true tells the operator the
	// item might succeed on a retry rather than being permanently gone.
	Retryable bool `json:"retryable,omitempty"`
}

type bulkClientResponse struct {
	Action    bulkClientAction    `json:"action"`
	Succeeded []string            `json:"succeeded"`
	Skipped   []string            `json:"skipped"`
	Failed    []bulkClientFailure `json:"failed"`
}

// bulkClientMaxIDs caps a single bulk request so a misbehaving caller
// cannot pin the clients lock with an arbitrarily long list. 500 is
// well above any realistic operator-driven bulk action and well below
// the "thousands" range where contention starts to dominate.
const bulkClientMaxIDs = 500

// handleBulkClientAction collapses the previous N-HTTP-requests fan-out
// from the dashboard (one PUT/DELETE per client) into a single
// authoritative call. Each id is processed sequentially so the
// per-client mutex pattern is preserved; the win is purely round-trip
// elimination.
func (s *Server) handleBulkClientAction() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		var request bulkClientRequest
		if err := decodeJSON(r, &request); err != nil {
			writeErrorLogged(r.Context(), w, http.StatusBadRequest, "invalid bulk payload", err)
			return
		}
		if len(request.IDs) == 0 {
			writeError(w, http.StatusBadRequest, "ids required")
			return
		}
		if len(request.IDs) > bulkClientMaxIDs {
			writeError(w, http.StatusBadRequest, "too many ids in single bulk request")
			return
		}

		response := bulkClientResponse{
			Action:    request.Action,
			Succeeded: make([]string, 0, len(request.IDs)),
			Skipped:   make([]string, 0),
			Failed:    make([]bulkClientFailure, 0),
		}

		switch request.Action {
		case bulkClientEnable, bulkClientDisable:
			s.applyBulkClientEnable(r.Context(), session.UserID, scope, request, &response)
		case bulkClientDelete:
			s.applyBulkClientDelete(r.Context(), session.UserID, scope, request, &response)
		default:
			writeError(w, http.StatusBadRequest, "unsupported bulk action")
			return
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleRotateClientSecret() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, msgClientIDRequired)
			return
		}

		if !s.ensureClientMutationScope(r.Context(), w, clientID, scope) {
			return
		}

		client, assignments, deployments, err := s.rotateClientSecret(r.Context(), clientID, session.UserID, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.InfoContext(r.Context(), "client secret rotated", "client_id", client.ID, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.rotate_secret", string(client.ID), nil)
		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(r.Context(), client, assignments, deployments, true))
	}
}

// handleRedeployClient re-queues the client.create rollout job for
// every currently-assigned target agent. Operators hit this when an
// earlier deployment failed (bad Telemt response, network glitch,
// unreachable node) and left the panel with a stuck "failed"
// deployment that couldn't be recovered without editing fields.
func (s *Server) handleRedeployClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, msgClientIDRequired)
			return
		}

		if !s.ensureClientMutationScope(r.Context(), w, clientID, scope) {
			return
		}

		client, assignments, deployments, err := s.redeployClientWithContext(r.Context(), clientID, session.UserID, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.InfoContext(r.Context(), "client redeployed", "client_id", client.ID, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.redeploy", string(client.ID), map[string]any{
			"target_agent_ids": deploymentAgentIDsFromResponses(deployments),
		})
		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(r.Context(), client, assignments, deployments, false))
	}
}

// resetClientQuotaResponse is the wire shape returned by the two
// reset-quota HTTP routes. `Job` is the freshly-enqueued job — the
// frontend polls /api/jobs to watch its Targets[i].Status flip and
// parse Targets[i].ResultJSON (clientResetQuotaJobResult) for typed
// per-agent reasons (unsupported_telemt / read_only_telemt). `Client`
// reflects the current detail snapshot so the caller can render the
// page from the same response.
type resetClientQuotaResponse struct {
	Client clientDetailResponse `json:"client"`
	Job    jobs.Job             `json:"job"`
}

// handleResetClientQuota fans the client.reset_quota job out to every
// agent currently hosting the client. Operators hit this when a
// client's quota counter needs to be cleared everywhere at once (start
// of a new accounting period, manual quota top-up, etc.).
func (s *Server) handleResetClientQuota() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, msgClientIDRequired)
			return
		}

		if !s.ensureClientMutationScope(r.Context(), w, clientID, scope) {
			return
		}

		client, assignments, deployments, job, err := s.resetClientQuota(r.Context(), clientID, "", session.UserID, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.InfoContext(r.Context(), "client quota reset", "client_id", client.ID, "user_id", session.UserID, "target_agents", job.TargetAgentIDs)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.reset_quota", string(client.ID), map[string]any{
			"target_agent_ids": job.TargetAgentIDs,
			"job_id":           job.ID,
		})
		// L-2: redact the job payload before returning, consistent with the
		// /api/jobs list endpoints (the reset-quota payload carries no secret
		// today, but this keeps the contract uniform).
		job.PayloadJSON = ""
		writeJSON(w, http.StatusOK, resetClientQuotaResponse{
			Client: s.buildClientDetailResponse(r.Context(), client, assignments, deployments, false),
			Job:    job,
		})
	}
}

// handleResetClientQuotaOnAgent targets the client.reset_quota job at
// exactly one agent. Used by the per-deployment Reset affordance on
// the client detail page — operators can investigate a single
// misbehaving node without affecting the rest of the fleet.
func (s *Server) handleResetClientQuotaOnAgent() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		agentID := chi.URLParam(r, "agent_id")
		if clientID == "" || agentID == "" {
			writeError(w, http.StatusBadRequest, "client id and agent id are required")
			return
		}

		if !s.ensureClientMutationScope(r.Context(), w, clientID, scope) {
			return
		}

		client, assignments, deployments, job, err := s.resetClientQuota(r.Context(), clientID, agentID, session.UserID, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.InfoContext(r.Context(), "client quota reset", "client_id", client.ID, "user_id", session.UserID, "agent_id", agentID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.reset_quota", string(client.ID), map[string]any{
			"agent_id": agentID,
			"job_id":   job.ID,
		})
		// L-2: redact the job payload before returning, consistent with the
		// /api/jobs list endpoints (the reset-quota payload carries no secret
		// today, but this keeps the contract uniform).
		job.PayloadJSON = ""
		writeJSON(w, http.StatusOK, resetClientQuotaResponse{
			Client: s.buildClientDetailResponse(r.Context(), client, assignments, deployments, false),
			Job:    job,
		})
	}
}
