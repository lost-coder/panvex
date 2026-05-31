package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

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

// requireClientsAccessWithScope loads the per-operator FleetScopeAccess so
// handlers can narrow list/get/mutate flows to the visible fleet groups.
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

// requireClientsAccess checks that the caller has a valid session with at
// least operator-level role. Returns the session, user, and ok=true on
// success; writes an HTTP error and returns ok=false otherwise.
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

// clientInScope reports whether at least one of the client's fleet
// group ids (via assignments) is inside the operator's scope.
// Operators with global scope always pass.
func (s *Server) clientInScope(scope FleetScopeAccess, assignments []managedClientAssignment) bool {
	if scope.Global {
		return true
	}
	for _, a := range assignments {
		if a.FleetGroupID != "" && scope.IsAllowed(string(a.FleetGroupID)) {
			return true
		}
	}
	return false
}

// bulkUniqueIPCountsForClients fetches per-client unique-IP counts in a
// single round-trip (Q2.U-P-03). Returns an empty map if the store is
// unavailable or the bulk query fails — callers fall back to the
// in-memory usage snapshot.
func (s *Server) bulkUniqueIPCountsForClients(ctx context.Context, clients []managedClient) map[string]int {
	clientIDs := make([]string, 0, len(clients))
	for _, c := range clients {
		clientIDs = append(clientIDs, string(c.ID))
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
// scope (R-S-14). Callers supply the pre-snapshotted assignments,
// deployments, and usage so the loop body holds no lock.
func (s *Server) buildClientListRow(
	client managedClient,
	scope FleetScopeAccess,
	assignments []managedClientAssignment,
	deployments []managedClientDeployment,
	usage aggregatedClientUsage,
	uniqueIPCounts map[string]int,
) (clientListResponse, bool) {
	if !s.clientInScope(scope, assignments) {
		return clientListResponse{}, false
	}
	uniqueIPs := usage.UniqueIPsUsed
	if count, ok := uniqueIPCounts[string(client.ID)]; ok && count > 0 {
		uniqueIPs = count
	}
	return clientListResponse{
		ID:                 string(client.ID),
		Name:               client.Name,
		Enabled:            client.Enabled,
		AssignedNodesCount: len(s.resolveClientTargetAgentIDs(assignments)),
		ExpirationRFC3339:  client.ExpirationRFC3339,
		TrafficUsedBytes:   usage.TrafficUsedBytes,
		UniqueIPsUsed:      uniqueIPs,
		ActiveTCPConns:     usage.ActiveTCPConns,
		DataQuotaBytes:     client.DataQuotaBytes,
		LastDeployStatus:   deriveClientDeployStatus(deployments),
	}, true
}

// handleClientMutationError translates client mutation errors to HTTP
// responses. Returns true when err is nil (no error to write).
func handleClientMutationError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return true
	}

	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errClientNameRequired), errors.Is(err, errClientNameInvalid), errors.Is(err, errClientUserADTag), errors.Is(err, errClientExpiration), errors.Is(err, errClientTargetsRequired), errors.Is(err, errClientLimitNegative):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, jobs.ErrReadOnlyTarget):
		writeError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("client mutation failed", "error", err)
		writeError(w, http.StatusInternalServerError, msgInternalError)
	}

	return false
}

// buildClientDetailResponse assembles the full JSON detail response for
// a client, including resolved usage, unique-IP count, and deployment
// status rows.
func (s *Server) buildClientDetailResponse(ctx context.Context, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment, showSecret bool) clientDetailResponse {
	usageByAgent := s.clientsSvc.MirrorUsageByAgent(string(client.ID))
	usage := s.clientsSvc.AggregateUsage(usageByAgent)
	uniqueIPs := s.resolveUniqueClientIPs(ctx, string(client.ID), usage.UniqueIPsUsed)

	response := clientDetailResponse{
		ID:                string(client.ID),
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
		Deployments:       buildClientDeploymentResponses(deployments, usageByAgent),
		CreatedAt:         client.CreatedAt.UTC().Unix(),
		UpdatedAt:         client.UpdatedAt.UTC().Unix(),
	}
	if client.DeletedAt != nil {
		response.DeletedAt = client.DeletedAt.UTC().Unix()
	}
	return response
}

// applyBulkClientEnable runs the enable/disable variant. It loads each
// client, flips Enabled if it differs from the requested value, and
// dispatches the existing updateClient flow. Clients whose
// state already matches are recorded as "skipped" so the UI can show
// accurate counts.
func (s *Server) applyBulkClientEnable(ctx context.Context, actorID string, scope FleetScopeAccess, request bulkClientRequest, response *bulkClientResponse) {
	want := request.Action == bulkClientEnable
	for _, id := range request.IDs {
		if id == "" {
			response.Failed = append(response.Failed, bulkClientFailure{ID: id, Error: "missing id"})
			continue
		}
		current, assignments, _, lookupErr := s.clientDetailSnapshot(id)
		if lookupErr != nil {
			response.Failed = append(response.Failed, bulkClientFailure{ID: id, Error: lookupErr.Error()})
			continue
		}
		if !s.clientInScope(scope, assignments) {
			// Mirror the regular handler's not-found semantics so
			// out-of-scope ids cannot be probed by trial.
			response.Failed = append(response.Failed, bulkClientFailure{ID: id, Error: msgClientNotFound})
			continue
		}
		if current.Enabled == want {
			response.Skipped = append(response.Skipped, id)
			continue
		}
		input := clientMutationInput{
			Name:              current.Name,
			Enabled:           &want,
			UserADTag:         current.UserADTag,
			MaxTCPConns:       current.MaxTCPConns,
			MaxUniqueIPs:      current.MaxUniqueIPs,
			DataQuotaBytes:    current.DataQuotaBytes,
			ExpirationRFC3339: current.ExpirationRFC3339,
			FleetGroupIDs:     assignmentFleetGroupIDs(assignments),
			AgentIDs:          assignmentAgentIDs(assignments),
		}
		if _, _, _, err := s.updateClient(ctx, id, actorID, input, s.now()); err != nil {
			response.Failed = append(response.Failed, bulkClientFailure{ID: id, Error: err.Error()})
			continue
		}
		response.Succeeded = append(response.Succeeded, id)
	}
}

// applyBulkClientDelete runs the delete variant. Scope-check reuses
// ensureClientMutationScope semantics so an out-of-scope or unknown id
// returns the same not-found shape callers see from the single-id
// endpoint.
func (s *Server) applyBulkClientDelete(ctx context.Context, actorID string, scope FleetScopeAccess, request bulkClientRequest, response *bulkClientResponse) {
	for _, id := range request.IDs {
		if id == "" {
			response.Failed = append(response.Failed, bulkClientFailure{ID: id, Error: "missing id"})
			continue
		}
		_, assignments, _, lookupErr := s.clientDetailSnapshot(id)
		if lookupErr != nil {
			response.Failed = append(response.Failed, bulkClientFailure{ID: id, Error: lookupErr.Error()})
			continue
		}
		if !s.clientInScope(scope, assignments) {
			response.Failed = append(response.Failed, bulkClientFailure{ID: id, Error: msgClientNotFound})
			continue
		}
		if err := s.deleteClient(ctx, id, actorID, s.now()); err != nil {
			response.Failed = append(response.Failed, bulkClientFailure{ID: id, Error: err.Error()})
			continue
		}
		response.Succeeded = append(response.Succeeded, id)
	}
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
func buildClientDeploymentResponses(deployments []managedClientDeployment, usageByAgent map[string]clients.UsageSnapshot) []clientDeploymentResponse {
	out := make([]clientDeploymentResponse, 0, len(deployments))
	for _, deployment := range deployments {
		lastAppliedAt := int64(0)
		if deployment.LastAppliedAt != nil {
			lastAppliedAt = deployment.LastAppliedAt.UTC().Unix()
		}
		links := deployment.ConnectionLinks
		if links == nil {
			links = []string{}
		}
		// usageByAgent may be nil when callers don't have a snapshot
		// handy (e.g. mutation flows that build a response before the
		// next usage tick lands). Missing keys mean "no traffic on
		// record for this agent yet" — zero is the correct default.
		usage := usageByAgent[deployment.AgentID]
		// Phase 3 drift signal: panel recorded a reset newer than Telemt
		// currently reports. Both sides must be non-zero — otherwise a
		// fresh deployment that has not yet reported quota usage would
		// trip the flag while still mid-deploy.
		drift := deployment.LastResetEpochSecs > 0 &&
			usage.QuotaLastResetUnix > 0 &&
			deployment.LastResetEpochSecs > usage.QuotaLastResetUnix
		out = append(out, clientDeploymentResponse{
			AgentID:            deployment.AgentID,
			DesiredOperation:   deployment.DesiredOperation,
			Status:             deployment.Status,
			LastError:          deployment.LastError,
			ConnectionLinks:    links,
			LinkDiagnostic:     deployment.LinkDiagnostic,
			LastAppliedAt:      lastAppliedAt,
			UpdatedAt:          deployment.UpdatedAt.UTC().Unix(),
			QuotaUsedBytes:     usage.QuotaUsedBytes,
			QuotaLastResetUnix: usage.QuotaLastResetUnix,
			PanelLastResetUnix: deployment.LastResetEpochSecs,
			QuotaResetDrift:    drift,
		})
	}
	return out
}

func assignmentFleetGroupIDs(assignments []managedClientAssignment) []string {
	values := make([]string, 0)
	for _, assignment := range assignments {
		if assignment.TargetType == clientAssignmentTargetFleetGroup {
			values = append(values, string(assignment.FleetGroupID))
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
