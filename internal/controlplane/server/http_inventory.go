package server

import (
	"net/http"

	"github.com/panvex/panvex/internal/controlplane/presence"
)

type fleetResponse struct {
	TotalAgents     int `json:"total_agents"`
	OnlineAgents    int `json:"online_agents"`
	DegradedAgents  int `json:"degraded_agents"`
	OfflineAgents   int `json:"offline_agents"`
	TotalInstances  int `json:"total_instances"`
	MetricSnapshots int `json:"metric_snapshots"`
}

func (s *Server) handleFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.mu.RLock()
		defer s.mu.RUnlock()

		response := fleetResponse{
			TotalAgents:     len(s.agents),
			TotalInstances:  len(s.instances),
			MetricSnapshots: len(s.metrics),
		}

		for agentID := range s.agents {
			switch s.presence.Evaluate(agentID, s.now()) {
			case presence.StateOnline:
				response.OnlineAgents++
			case presence.StateDegraded:
				response.DegradedAgents++
			default:
				response.OfflineAgents++
			}
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleAgents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.mu.RLock()
		defer s.mu.RUnlock()

		response := make([]Agent, 0, len(s.agents))
		for _, agent := range s.agents {
			response = append(response, agent)
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleInstances() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.mu.RLock()
		defer s.mu.RUnlock()

		response := make([]Instance, 0, len(s.instances))
		for _, instance := range s.instances {
			response = append(response, instance)
		}

		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.mu.RLock()
		defer s.mu.RUnlock()

		writeJSON(w, http.StatusOK, s.metrics)
	}
}

func (s *Server) handleAudit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.mu.RLock()
		defer s.mu.RUnlock()

		writeJSON(w, http.StatusOK, s.auditTrail)
	}
}
