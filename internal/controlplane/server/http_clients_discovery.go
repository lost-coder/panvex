package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type discoveredClientConflict struct {
	Type       string   `json:"type"`
	RelatedIDs []string `json:"related_ids"`
}

type discoveredClientResponse struct {
	ID                 string                     `json:"id"`
	AgentID            string                     `json:"agent_id"`
	NodeName           string                     `json:"node_name"`
	ClientName         string                     `json:"client_name"`
	Status             string                     `json:"status"`
	TotalOctets        uint64                     `json:"total_octets"`
	CurrentConnections int                        `json:"current_connections"`
	ActiveUniqueIPs    int                        `json:"active_unique_ips"`
	ConnectionLink     string                     `json:"connection_link"`
	MaxTCPConns        int                        `json:"max_tcp_conns"`
	MaxUniqueIPs       int                        `json:"max_unique_ips"`
	DataQuotaBytes     int64                      `json:"data_quota_bytes"`
	Expiration         string                     `json:"expiration"`
	DiscoveredAt       int64                      `json:"discovered_at_unix"`
	UpdatedAt          int64                      `json:"updated_at_unix"`
	Conflicts          []discoveredClientConflict `json:"conflicts,omitempty"`
}

func (s *Server) handleDiscoveredClients() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		clients, err := s.listDiscoveredClients(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		clients = s.filterDiscoveredClientsByScope(clients, scope)

		sortDiscoveredClientsByName(clients)
		conflicts := detectDiscoveredClientConflicts(clients)
		agentNodeNames := s.resolveAgentNodeNames(clients)

		response := make([]discoveredClientResponse, 0, len(clients))
		for _, dc := range clients {
			resp := discoveredClientToResponse(dc)
			resp.NodeName = agentNodeNames[dc.AgentID]
			if c, ok := conflicts[dc.ID]; ok {
				resp.Conflicts = c
			}
			response = append(response, resp)
		}

		writeJSON(w, http.StatusOK, response)
	}
}

// filterDiscoveredClientsByScope drops discovered clients whose parent
// agent sits outside the operator's scope. R-S-14: the parent agent's
// fleet_group_id is the scope key; an unknown parent (race with
// deregister) is dropped because we cannot scope-check it safely.
// Global scope short-circuits the lock acquisition.
func (s *Server) filterDiscoveredClientsByScope(clients []discoveredClient, scope FleetScopeAccess) []discoveredClient {
	if scope.Global {
		return clients
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	filtered := clients[:0]
	for _, dc := range clients {
		agent, agentOK := s.agents[dc.AgentID]
		if !agentOK || !scope.IsAllowed(agent.FleetGroupID) {
			continue
		}
		filtered = append(filtered, dc)
	}
	return filtered
}

// discoveredClientInScope reports whether the given discovered-client
// id resolves to an agent inside the operator's scope. Used by
// adopt/ignore. Returns (true, nil) when scope is global.
func (s *Server) discoveredClientInScope(ctx context.Context, scope FleetScopeAccess, dcID string) (bool, error) {
	if scope.Global {
		return true, nil
	}
	if s.store == nil {
		return false, nil
	}
	rec, err := s.store.GetDiscoveredClient(ctx, dcID)
	if err != nil {
		return false, err
	}
	s.mu.RLock()
	agent, agentOK := s.agents[rec.AgentID]
	s.mu.RUnlock()
	if !agentOK {
		return false, nil
	}
	return scope.IsAllowed(agent.FleetGroupID), nil
}

func (s *Server) handleAdoptDiscoveredClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		id := chi.URLParam(r, "id")
		if !s.checkDiscoveredClientScope(w, r, scope, id) {
			return
		}
		client, err := s.adoptDiscoveredClient(r.Context(), id, session.UserID, s.now())
		if err != nil {
			writeAdoptDiscoveredClientError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"client_id": client.ID,
			"name":      client.Name,
		})
	}
}

// checkDiscoveredClientScope writes the appropriate HTTP response when
// the discovered client is missing or out of scope, returning false in
// that case. R-S-14.
func (s *Server) checkDiscoveredClientScope(w http.ResponseWriter, r *http.Request, scope FleetScopeAccess, id string) bool {
	inScope, scopeErr := s.discoveredClientInScope(r.Context(), scope, id)
	if scopeErr != nil {
		if errors.Is(scopeErr, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
			return false
		}
		s.logger.Error("scope-check discovered client failed", "id", id, "error", scopeErr)
		writeError(w, http.StatusInternalServerError, msgInternalError)
		return false
	}
	if !inScope {
		writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
		return false
	}
	return true
}

// writeAdoptDiscoveredClientError maps an adopt failure to the matching
// HTTP response. Mirrors the original three-branch switch.
func writeAdoptDiscoveredClientError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
	case errors.Is(err, ErrAlreadyAdopted):
		writeError(w, http.StatusConflict, err.Error())
	default:
		handleClientMutationError(w, err)
	}
}

func (s *Server) handleIgnoreDiscoveredClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, scope, ok := s.requireClientsAccessWithScope(w, r)
		if !ok {
			return
		}

		id := chi.URLParam(r, "id")
		inScope, scopeErr := s.discoveredClientInScope(r.Context(), scope, id)
		if scopeErr != nil {
			if errors.Is(scopeErr, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
				return
			}
			s.logger.Error("scope-check discovered client failed", "id", id, "error", scopeErr)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		if !inScope {
			writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
			return
		}
		if err := s.ignoreDiscoveredClient(r.Context(), id, session.UserID, s.now()); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				writeError(w, http.StatusNotFound, msgDiscoveredClientNotFound)
			} else {
				writeError(w, http.StatusInternalServerError, msgInternalError)
			}
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func discoveredClientToResponse(dc discoveredClient) discoveredClientResponse {
	return discoveredClientResponse{
		ID:                 dc.ID,
		AgentID:            dc.AgentID,
		ClientName:         dc.ClientName,
		Status:             dc.Status,
		TotalOctets:        dc.TotalOctets,
		CurrentConnections: dc.CurrentConnections,
		ActiveUniqueIPs:    dc.ActiveUniqueIPs,
		ConnectionLink:     dc.ConnectionLink,
		MaxTCPConns:        dc.MaxTCPConns,
		MaxUniqueIPs:       dc.MaxUniqueIPs,
		DataQuotaBytes:     dc.DataQuotaBytes,
		Expiration:         dc.Expiration,
		DiscoveredAt:       dc.DiscoveredAt.Unix(),
		UpdatedAt:          dc.UpdatedAt.Unix(),
	}
}

// resolveAgentNodeNames maps agent IDs → node names from the agents cache.
func (s *Server) resolveAgentNodeNames(clients []discoveredClient) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	for _, dc := range clients {
		if _, ok := result[dc.AgentID]; ok {
			continue
		}
		if agent, ok := s.agents[dc.AgentID]; ok {
			result[dc.AgentID] = agent.NodeName
		}
	}
	return result
}

// discoveredConflictGroup groups discovered-client IDs that share a
// pivot key (secret or name) and tracks the distinct opposite-axis
// values so the caller can decide whether the group is a real conflict.
type discoveredConflictGroup struct {
	ids        []string
	otherAxis  map[string]struct{}
}

// otherIDs returns every id in the group except `id`.
func (g *discoveredConflictGroup) otherIDs(id string) []string {
	others := make([]string, 0, len(g.ids)-1)
	for _, oid := range g.ids {
		if oid != id {
			others = append(others, oid)
		}
	}
	return others
}

// recordDiscoveredConflicts walks groups and emits conflict entries
// of the given kind for every id in any group with more than one
// distinct value on the opposite axis.
func recordDiscoveredConflicts(groups map[string]*discoveredConflictGroup, kind string, result map[string][]discoveredClientConflict) {
	for _, g := range groups {
		if len(g.otherAxis) <= 1 {
			continue
		}
		for _, id := range g.ids {
			result[id] = append(result[id], discoveredClientConflict{
				Type:       kind,
				RelatedIDs: g.otherIDs(id),
			})
		}
	}
}

// detectDiscoveredClientConflicts identifies conflicts among discovered clients:
// - same_secret_different_names: multiple names share one secret
// - same_name_different_secrets: one name has multiple different secrets
func detectDiscoveredClientConflicts(clients []discoveredClient) map[string][]discoveredClientConflict {
	bySecret := make(map[string]*discoveredConflictGroup)
	byName := make(map[string]*discoveredConflictGroup)

	for _, dc := range clients {
		if dc.Secret != "" {
			g, ok := bySecret[dc.Secret]
			if !ok {
				g = &discoveredConflictGroup{otherAxis: make(map[string]struct{})}
				bySecret[dc.Secret] = g
			}
			g.ids = append(g.ids, dc.ID)
			g.otherAxis[dc.ClientName] = struct{}{}
		}

		g, ok := byName[dc.ClientName]
		if !ok {
			g = &discoveredConflictGroup{otherAxis: make(map[string]struct{})}
			byName[dc.ClientName] = g
		}
		g.ids = append(g.ids, dc.ID)
		if dc.Secret != "" {
			g.otherAxis[dc.Secret] = struct{}{}
		}
	}

	result := make(map[string][]discoveredClientConflict)
	recordDiscoveredConflicts(bySecret, "same_secret_different_names", result)
	recordDiscoveredConflicts(byName, "same_name_different_secrets", result)
	return result
}
