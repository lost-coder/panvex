package server

import (
	"errors"
	"log"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/jobs"
	"github.com/panvex/panvex/internal/controlplane/storage"
)

type clientMutationRequest struct {
	Name              string   `json:"name"`
	Enabled           *bool    `json:"enabled"`
	UserADTag         string   `json:"user_ad_tag"`
	MaxTCPConns       int      `json:"max_tcp_conns"`
	MaxUniqueIPs      int      `json:"max_unique_ips"`
	DataQuotaBytes    int64    `json:"data_quota_bytes"`
	ExpirationRFC3339 string   `json:"expiration_rfc3339"`
	FleetGroupIDs     []string `json:"fleet_group_ids"`
	AgentIDs          []string `json:"agent_ids"`
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
		if _, _, ok := s.requireClientsAccess(w, r); !ok {
			return
		}

		clients := s.listClientsSnapshot()
		response := make([]clientListResponse, 0, len(clients))
		for _, client := range clients {
			_, assignments, deployments, err := s.clientDetailSnapshot(client.ID)
			if err != nil {
				log.Printf("load client detail failed for client %q: %v", client.ID, err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			usage := s.aggregatedClientUsage(client.ID)

			response = append(response, clientListResponse{
				ID:                 client.ID,
				Name:               client.Name,
				Enabled:            client.Enabled,
				AssignedNodesCount: len(s.resolveClientTargetAgentIDs(assignments)),
				ExpirationRFC3339:  client.ExpirationRFC3339,
				TrafficUsedBytes:   usage.TrafficUsedBytes,
				UniqueIPsUsed:      usage.UniqueIPsUsed,
				ActiveTCPConns:     usage.ActiveTCPConns,
				DataQuotaBytes:     client.DataQuotaBytes,
				LastDeployStatus:   deriveClientDeployStatus(deployments),
			})
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleCreateClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, ok := s.requireClientsAccess(w, r)
		if !ok {
			return
		}

		var request clientMutationRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid client payload")
			return
		}

		client, assignments, deployments, err := s.createClientWithContext(r.Context(), session.UserID, clientMutationInput{
			Name:              request.Name,
			Enabled:           request.Enabled,
			UserADTag:         request.UserADTag,
			MaxTCPConns:       request.MaxTCPConns,
			MaxUniqueIPs:      request.MaxUniqueIPs,
			DataQuotaBytes:    request.DataQuotaBytes,
			ExpirationRFC3339: request.ExpirationRFC3339,
			FleetGroupIDs:     request.FleetGroupIDs,
			AgentIDs:          request.AgentIDs,
		}, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "clients.create", client.ID, map[string]any{
			"name":             client.Name,
			"enabled":          client.Enabled,
			"fleet_group_ids":  assignmentFleetGroupIDs(assignments),
			"agent_ids":        assignmentAgentIDs(assignments),
			"target_agent_ids": deploymentAgentIDsFromResponses(deployments),
		})
		writeJSON(w, http.StatusCreated, s.buildClientDetailResponse(client, assignments, deployments, true))
	}
}

func (s *Server) handleClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, ok := s.requireClientsAccess(w, r); !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, "client id is required")
			return
		}

		client, assignments, deployments, err := s.clientDetailSnapshot(clientID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, err.Error())
				return
			}
			log.Printf("load client failed for client %q: %v", clientID, err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(client, assignments, deployments, false))
	}
}

func (s *Server) handleUpdateClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, ok := s.requireClientsAccess(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, "client id is required")
			return
		}

		var request clientMutationRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid client payload")
			return
		}

		client, assignments, deployments, err := s.updateClientWithContext(r.Context(), clientID, session.UserID, clientMutationInput{
			Name:              request.Name,
			Enabled:           request.Enabled,
			UserADTag:         request.UserADTag,
			MaxTCPConns:       request.MaxTCPConns,
			MaxUniqueIPs:      request.MaxUniqueIPs,
			DataQuotaBytes:    request.DataQuotaBytes,
			ExpirationRFC3339: request.ExpirationRFC3339,
			FleetGroupIDs:     request.FleetGroupIDs,
			AgentIDs:          request.AgentIDs,
		}, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "clients.update", client.ID, map[string]any{
			"name":            client.Name,
			"fleet_group_ids": assignmentFleetGroupIDs(assignments),
			"agent_ids":       assignmentAgentIDs(assignments),
		})
		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(client, assignments, deployments, false))
	}
}

func (s *Server) handleDeleteClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, ok := s.requireClientsAccess(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, "client id is required")
			return
		}

		if err := s.deleteClientWithContext(r.Context(), clientID, session.UserID, s.now()); err != nil {
			handleClientMutationError(w, err)
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "clients.delete", clientID, nil)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) handleRotateClientSecret() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, ok := s.requireClientsAccess(w, r)
		if !ok {
			return
		}

		clientID := chi.URLParam(r, "id")
		if clientID == "" {
			writeError(w, http.StatusBadRequest, "client id is required")
			return
		}

		client, assignments, deployments, err := s.rotateClientSecretWithContext(r.Context(), clientID, session.UserID, s.now())
		if !handleClientMutationError(w, err) {
			return
		}

		s.appendAuditWithContext(r.Context(), session.UserID, "clients.rotate_secret", client.ID, nil)
		writeJSON(w, http.StatusOK, s.buildClientDetailResponse(client, assignments, deployments, true))
	}
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
		log.Printf("client mutation failed: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}

	return false
}

func (s *Server) buildClientDetailResponse(client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment, showSecret bool) clientDetailResponse {
	usage := s.aggregatedClientUsage(client.ID)
	fleetGroupIDs := assignmentFleetGroupIDs(assignments)
	agentIDs := assignmentAgentIDs(assignments)

	var secret string
	if showSecret {
		secret = client.Secret
	}

	response := clientDetailResponse{
		ID:                client.ID,
		Name:              client.Name,
		Secret:            secret,
		UserADTag:         client.UserADTag,
		Enabled:           client.Enabled,
		TrafficUsedBytes:  usage.TrafficUsedBytes,
		UniqueIPsUsed:     usage.UniqueIPsUsed,
		ActiveTCPConns:    usage.ActiveTCPConns,
		MaxTCPConns:       client.MaxTCPConns,
		MaxUniqueIPs:      client.MaxUniqueIPs,
		DataQuotaBytes:    client.DataQuotaBytes,
		ExpirationRFC3339: client.ExpirationRFC3339,
		FleetGroupIDs:     fleetGroupIDs,
		AgentIDs:          agentIDs,
		Deployments:       make([]clientDeploymentResponse, 0, len(deployments)),
		CreatedAt:         client.CreatedAt.UTC().Unix(),
		UpdatedAt:         client.UpdatedAt.UTC().Unix(),
	}
	if client.DeletedAt != nil {
		response.DeletedAt = client.DeletedAt.UTC().Unix()
	}

	for _, deployment := range deployments {
		lastAppliedAt := int64(0)
		if deployment.LastAppliedAt != nil {
			lastAppliedAt = deployment.LastAppliedAt.UTC().Unix()
		}
		response.Deployments = append(response.Deployments, clientDeploymentResponse{
			AgentID:          deployment.AgentID,
			DesiredOperation: deployment.DesiredOperation,
			Status:           deployment.Status,
			LastError:        deployment.LastError,
			ConnectionLink:   deployment.ConnectionLink,
			LastAppliedAt:    lastAppliedAt,
			UpdatedAt:        deployment.UpdatedAt.UTC().Unix(),
		})
	}

	return response
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
