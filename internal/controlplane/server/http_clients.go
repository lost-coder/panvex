package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/auth"
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
// expected by createClientWithContext / updateClientWithContext.
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

// validateRequestedFleetGroupScope (R-S-14): a non-global operator may only
// assign the client to fleet groups they own. Reject the whole request if any
// requested group is outside scope — silently dropping members would surprise
// the operator on the response.
func validateRequestedFleetGroupScope(w http.ResponseWriter, scope FleetScopeAccess, fleetGroupIDs []string) bool {
	if scope.Global {
		return true
	}
	for _, gid := range fleetGroupIDs {
		if !scope.IsAllowed(gid) {
			writeError(w, http.StatusForbidden, "fleet group outside operator scope")
			return false
		}
	}
	return true
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
	AgentID          string `json:"agent_id"`
	DesiredOperation string `json:"desired_operation"`
	Status           string `json:"status"`
	LastError        string `json:"last_error"`
	ConnectionLink   string `json:"connection_link"`
	LastAppliedAt    int64  `json:"last_applied_at_unix"`
	UpdatedAt        int64  `json:"updated_at_unix"`
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
	CreatedAt         int64                      `json:"created_at_unix"`
	UpdatedAt         int64                      `json:"updated_at_unix"`
	DeletedAt         int64                      `json:"deleted_at_unix"`
}

func (s *Server) handleClients() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clients := s.listClientsSnapshot()
		uniqueIPCounts := s.bulkUniqueIPCountsForClients(r.Context(), clients)

		response := make([]clientListResponse, 0, len(clients))
		for _, client := range clients {
			row, included, err := s.buildClientListRow(client, scope, uniqueIPCounts)
			if err != nil {
				s.logger.Error("load client detail failed", "client_id", client.ID, "error", err)
				writeError(w, http.StatusInternalServerError, msgInternalError)
				return
			}
			if !included {
				continue
			}
			response = append(response, row)
		}

		writeJSON(w, http.StatusOK, response)
	}
}

// bulkUniqueIPCountsForClients fetches per-client unique-IP counts in a
// single round-trip (Q2.U-P-03). Returns an empty map if the store is
// unavailable or the bulk query fails — callers fall back to the
// in-memory usage snapshot.
func (s *Server) bulkUniqueIPCountsForClients(ctx context.Context, clients []managedClient) map[string]int {
	clientIDs := make([]string, 0, len(clients))
	for _, c := range clients {
		clientIDs = append(clientIDs, c.ID)
	}
	uniqueIPCounts := map[string]int{}
	if s.store == nil || len(clientIDs) == 0 {
		return uniqueIPCounts
	}
	counts, err := s.store.CountUniqueClientIPsForClients(ctx, clientIDs)
	if err != nil {
		s.logger.Warn("bulk unique-ip count failed", "error", err)
		return uniqueIPCounts
	}
	return counts
}

// buildClientListRow assembles the listing row for a single client.
// Returns included=false when the client falls outside the operator
// scope (R-S-14).
func (s *Server) buildClientListRow(client managedClient, scope FleetScopeAccess, uniqueIPCounts map[string]int) (clientListResponse, bool, error) {
	_, assignments, deployments, err := s.clientDetailSnapshot(client.ID)
	if err != nil {
		return clientListResponse{}, false, err
	}
	if !s.clientInScope(scope, assignments) {
		return clientListResponse{}, false, nil
	}
	usage := s.aggregatedClientUsage(client.ID)
	uniqueIPs := usage.UniqueIPsUsed
	if count, ok := uniqueIPCounts[client.ID]; ok && count > 0 {
		uniqueIPs = count
	}
	return clientListResponse{
		ID:                 client.ID,
		Name:               client.Name,
		Enabled:            client.Enabled,
		AssignedNodesCount: len(s.resolveClientTargetAgentIDs(assignments)),
		ExpirationRFC3339:  client.ExpirationRFC3339,
		TrafficUsedBytes:   usage.TrafficUsedBytes,
		UniqueIPsUsed:      uniqueIPs,
		ActiveTCPConns:     usage.ActiveTCPConns,
		DataQuotaBytes:     client.DataQuotaBytes,
		LastDeployStatus:   deriveClientDeployStatus(deployments),
	}, true, nil
}

func (s *Server) handleCreateClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		var request clientMutationRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid client payload")
			return
		}

		if !validateRequestedFleetGroupScope(w, scope, request.FleetGroupIDs) {
			return
		}

		client, assignments, deployments, err := s.createClientWithContext(r.Context(), session.UserID, request.toMutationInput(), s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.Info("client created", "client_id", client.ID, "name", client.Name, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.create", client.ID, map[string]any{
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
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			s.logger.Error("load client failed", "client_id", clientID, "error", err)
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
		if !s.ensureClientMutationScope(w, clientID, scope) {
			return
		}

		var request clientMutationRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid client payload")
			return
		}

		if !validateRequestedFleetGroupScope(w, scope, request.FleetGroupIDs) {
			return
		}

		client, assignments, deployments, err := s.updateClientWithContext(r.Context(), clientID, session.UserID, request.toMutationInput(), s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.Info("client updated", "client_id", client.ID, "name", client.Name, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.update", client.ID, map[string]any{
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
		if !s.ensureClientMutationScope(w, clientID, scope) {
			return
		}

		if err := s.deleteClientWithContext(r.Context(), clientID, session.UserID, s.now()); err != nil {
			handleClientMutationError(w, err)
			return
		}

		s.logger.Info("client deleted", "client_id", clientID, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.delete", clientID, nil)
		w.WriteHeader(http.StatusNoContent)
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

		if !s.ensureClientMutationScope(w, clientID, scope) {
			return
		}

		client, assignments, deployments, err := s.rotateClientSecretWithContext(r.Context(), clientID, session.UserID, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.Info("client secret rotated", "client_id", client.ID, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.rotate_secret", client.ID, nil)
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

		if !s.ensureClientMutationScope(w, clientID, scope) {
			return
		}

		client, assignments, deployments, err := s.redeployClientWithContext(r.Context(), clientID, session.UserID, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.logger.Info("client redeployed", "client_id", client.ID, "user_id", session.UserID)
		s.appendAuditWithContext(r.Context(), session.UserID, "clients.redeploy", client.ID, map[string]any{
			"target_agent_ids": deploymentAgentIDsFromResponses(deployments),
		})
		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(r.Context(), client, assignments, deployments, false))
	}
}

// ensureClientMutationScope verifies the scoped operator may mutate
// the given client. Returns false (and writes the HTTP error) when the
// client is missing, the lookup fails, or the client sits outside the
// operator's scope. Global scope short-circuits the lookup.
func (s *Server) ensureClientMutationScope(w http.ResponseWriter, clientID string, scope FleetScopeAccess) bool {
	if scope.Global {
		return true
	}
	_, existing, _, lookupErr := s.clientDetailSnapshot(clientID)
	if lookupErr != nil {
		if errors.Is(lookupErr, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, msgClientNotFound)
			return false
		}
		s.logger.Error(msgClientScopeCheckFailed, "client_id", clientID, "error", lookupErr)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return false
	}
	if !s.clientInScope(scope, existing) {
		writeError(w, http.StatusNotFound, msgClientNotFound)
		return false
	}
	return true
}

// requireClientsAccessWithScope is the scope-aware variant of
// requireClientsAccess used by the R-S-14 rollout. It loads the
// per-operator FleetScopeAccess so handlers can narrow list/get/mutate
// flows to the visible fleet groups.
func (s *Server) requireClientsAccessWithScope(w http.ResponseWriter, r *http.Request) (auth.Session, auth.User, FleetScopeAccess, bool) {
	session, user, ok := s.requireClientsAccess(w, r)
	if !ok {
		return session, user, FleetScopeAccess{}, false
	}
	scope, ok := s.requireFleetScope(w, r, user)
	if !ok {
		return session, user, FleetScopeAccess{}, false
	}
	return session, user, scope, true
}

// clientInScope reports whether at least one of the client's fleet
// group ids (via assignments) is inside the operator's scope.
// Operators with global scope always pass.
func (s *Server) clientInScope(scope FleetScopeAccess, assignments []managedClientAssignment) bool {
	if scope.Global {
		return true
	}
	for _, a := range assignments {
		if a.FleetGroupID != "" && scope.IsAllowed(a.FleetGroupID) {
			return true
		}
	}
	return false
}

func (s *Server) requireClientsAccess(w http.ResponseWriter, r *http.Request) (auth.Session, auth.User, bool) {
	session, user, err := s.requireSession(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return auth.Session{}, auth.User{}, false
	}
	if user.Role == auth.RoleViewer {
		writeError(w, http.StatusForbidden, "viewer role cannot access clients")
		return auth.Session{}, auth.User{}, false
	}

	return session, user, true
}

func handleClientMutationError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return true
	}

	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errClientNameRequired), errors.Is(err, errClientUserADTag), errors.Is(err, errClientExpiration), errors.Is(err, errClientTargetsRequired):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, jobs.ErrReadOnlyTarget):
		writeError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("client mutation failed", "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
	}

	return false
}

func (s *Server) buildClientDetailResponse(ctx context.Context, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment, showSecret bool) clientDetailResponse {
	usage := s.aggregatedClientUsage(client.ID)
	uniqueIPs := s.resolveUniqueClientIPs(ctx, client.ID, usage.UniqueIPsUsed)

	response := clientDetailResponse{
		ID:                client.ID,
		Name:              client.Name,
		Secret:            secretIfRevealed(client.Secret, showSecret),
		UserADTag:         client.UserADTag,
		Enabled:           client.Enabled,
		TrafficUsedBytes:  usage.TrafficUsedBytes,
		UniqueIPsUsed:     uniqueIPs,
		ActiveTCPConns:    usage.ActiveTCPConns,
		MaxTCPConns:       client.MaxTCPConns,
		MaxUniqueIPs:      client.MaxUniqueIPs,
		DataQuotaBytes:    client.DataQuotaBytes,
		ExpirationRFC3339: client.ExpirationRFC3339,
		FleetGroupIDs:     assignmentFleetGroupIDs(assignments),
		AgentIDs:          assignmentAgentIDs(assignments),
		Deployments:       buildClientDeploymentResponses(deployments),
		CreatedAt:         client.CreatedAt.UTC().Unix(),
		UpdatedAt:         client.UpdatedAt.UTC().Unix(),
	}
	if client.DeletedAt != nil {
		response.DeletedAt = client.DeletedAt.UTC().Unix()
	}
	return response
}

// resolveUniqueClientIPs prefers the durable per-client unique-IP count
// from storage and falls back to the in-memory snapshot when the store is
// unavailable or returns zero (no rows).
func (s *Server) resolveUniqueClientIPs(ctx context.Context, clientID string, fallback int) int {
	if s.store == nil {
		return fallback
	}
	count, err := s.store.CountUniqueClientIPs(ctx, clientID)
	if err != nil || count <= 0 {
		return fallback
	}
	return count
}

// secretIfRevealed returns the raw secret when the caller has opted in to
// disclosure, else "".
func secretIfRevealed(secret string, reveal bool) string {
	if reveal {
		return secret
	}
	return ""
}

// buildClientDeploymentResponses converts the deployment slice into the
// JSON response shape, normalising the optional LastAppliedAt timestamp.
func buildClientDeploymentResponses(deployments []managedClientDeployment) []clientDeploymentResponse {
	out := make([]clientDeploymentResponse, 0, len(deployments))
	for _, deployment := range deployments {
		lastAppliedAt := int64(0)
		if deployment.LastAppliedAt != nil {
			lastAppliedAt = deployment.LastAppliedAt.UTC().Unix()
		}
		out = append(out, clientDeploymentResponse{
			AgentID:          deployment.AgentID,
			DesiredOperation: deployment.DesiredOperation,
			Status:           deployment.Status,
			LastError:        deployment.LastError,
			ConnectionLink:   deployment.ConnectionLink,
			LastAppliedAt:    lastAppliedAt,
			UpdatedAt:        deployment.UpdatedAt.UTC().Unix(),
		})
	}
	return out
}

func assignmentFleetGroupIDs(assignments []managedClientAssignment) []string {
	values := make([]string, 0)
	for _, assignment := range assignments {
		if assignment.TargetType == clientAssignmentTargetFleetGroup {
			values = append(values, assignment.FleetGroupID)
		}
	}
	return normalizedIDs(values)
}

func assignmentAgentIDs(assignments []managedClientAssignment) []string {
	values := make([]string, 0)
	for _, assignment := range assignments {
		if assignment.TargetType == clientAssignmentTargetAgent {
			values = append(values, assignment.AgentID)
		}
	}
	return normalizedIDs(values)
}

func deploymentAgentIDsFromResponses(deployments []managedClientDeployment) []string {
	agentIDs := make([]string, 0, len(deployments))
	for _, deployment := range deployments {
		agentIDs = append(agentIDs, deployment.AgentID)
	}
	sort.Strings(agentIDs)
	return agentIDs
}

func deriveClientDeployStatus(deployments []managedClientDeployment) string {
	if len(deployments) == 0 {
		return "idle"
	}

	status := clientDeploymentStatusSucceeded
	for _, deployment := range deployments {
		switch deployment.Status {
		case clientDeploymentStatusFailed:
			return clientDeploymentStatusFailed
		case clientDeploymentStatusQueued:
			status = "pending"
		}
	}

	return status
}
