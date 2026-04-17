package server

import (
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
		if _, _, ok := s.requireClientsAccess(w, r); !ok {
			return
		}

		clients, err := s.listDiscoveredClients(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

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

func (s *Server) handleAdoptDiscoveredClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, ok := s.requireClientsAccess(w, r)
		if !ok {
			return
		}

		id := chi.URLParam(r, "id")
		client, err := s.adoptDiscoveredClient(r.Context(), id, session.UserID, s.now())
		if err != nil {
			switch {
			case errors.Is(err, storage.ErrNotFound):
				writeError(w, http.StatusNotFound, "discovered client not found")
			case errors.Is(err, ErrAlreadyAdopted):
				writeError(w, http.StatusConflict, err.Error())
			default:
				if !handleClientMutationError(w, err) {
					return
				}
			}
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"client_id": client.ID,
			"name":      client.Name,
		})
	}
}

func (s *Server) handleIgnoreDiscoveredClient() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, _, ok := s.requireClientsAccess(w, r)
		if !ok {
			return
		}

		id := chi.URLParam(r, "id")
		if err := s.ignoreDiscoveredClient(r.Context(), id, session.UserID, s.now()); err != nil {
			if err == storage.ErrNotFound {
				writeError(w, http.StatusNotFound, "discovered client not found")
			} else {
				writeError(w, http.StatusInternalServerError, "internal error")
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

// detectDiscoveredClientConflicts identifies conflicts among discovered clients:
// - same_secret_different_names: multiple names share one secret
// - same_name_different_secrets: one name has multiple different secrets
func detectDiscoveredClientConflicts(clients []discoveredClient) map[string][]discoveredClientConflict {
	// Group by secret → list of IDs and names.
	type secretGroup struct {
		ids   []string
		names map[string]struct{}
	}
	bySecret := make(map[string]*secretGroup)

	// Group by name → list of IDs and secrets.
	type nameGroup struct {
		ids     []string
		secrets map[string]struct{}
	}
	byName := make(map[string]*nameGroup)

	for _, dc := range clients {
		if dc.Secret != "" {
			g, ok := bySecret[dc.Secret]
			if !ok {
				g = &secretGroup{names: make(map[string]struct{})}
				bySecret[dc.Secret] = g
			}
			g.ids = append(g.ids, dc.ID)
			g.names[dc.ClientName] = struct{}{}
		}

		g, ok := byName[dc.ClientName]
		if !ok {
			g = &nameGroup{secrets: make(map[string]struct{})}
			byName[dc.ClientName] = g
		}
		g.ids = append(g.ids, dc.ID)
		if dc.Secret != "" {
			g.secrets[dc.Secret] = struct{}{}
		}
	}

	result := make(map[string][]discoveredClientConflict)

	// Same secret, different names.
	for _, g := range bySecret {
		if len(g.names) <= 1 {
			continue
		}
		for _, id := range g.ids {
			others := make([]string, 0, len(g.ids)-1)
			for _, oid := range g.ids {
				if oid != id {
					others = append(others, oid)
				}
			}
			result[id] = append(result[id], discoveredClientConflict{
				Type:       "same_secret_different_names",
				RelatedIDs: others,
			})
		}
	}

	// Same name, different secrets.
	for _, g := range byName {
		if len(g.secrets) <= 1 {
			continue
		}
		for _, id := range g.ids {
			others := make([]string, 0, len(g.ids)-1)
			for _, oid := range g.ids {
				if oid != id {
					others = append(others, oid)
				}
			}
			result[id] = append(result[id], discoveredClientConflict{
				Type:       "same_name_different_secrets",
				RelatedIDs: others,
			})
		}
	}

	return result
}
