package server

import (
	"net/http"

	"github.com/panvex/panvex/internal/controlplane/presence"
	"github.com/panvex/panvex/internal/controlplane/storage"
)

type fleetResponse struct {
	TotalAgents     int `json:"total_agents"`
	OnlineAgents    int `json:"online_agents"`
	DegradedAgents  int `json:"degraded_agents"`
	OfflineAgents   int `json:"offline_agents"`
	TotalInstances  int `json:"total_instances"`
	MetricSnapshots int `json:"metric_snapshots"`
	LiveConnections int `json:"live_connections"`
	AcceptingNewConnectionsAgents int `json:"accepting_new_connections_agents"`
	MiddleProxyAgents int `json:"middle_proxy_agents"`
	DCIssueAgents int `json:"dc_issue_agents"`
}

func (s *Server) handleFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.metricsAuditMu.RLock()
		metricSnapshots := len(s.metrics)
		s.metricsAuditMu.RUnlock()

		s.mu.RLock()
		response := fleetResponse{
			TotalAgents:     len(s.agents),
			TotalInstances:  len(s.instances),
			MetricSnapshots: metricSnapshots,
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
		s.mu.RUnlock()

		writeJSON(w, http.StatusOK, response)
	}
}

func (s *Server) handleAgents() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		now := s.now()
		recoveryGrants := make(map[string]storage.AgentCertificateRecoveryGrantRecord)
		if s.store != nil {
			loadedGrants, err := s.store.ListAgentCertificateRecoveryGrants(r.Context())
			if err != nil {
				s.logger.Error("list agent certificate recovery grants failed", "error", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
			for _, grant := range loadedGrants {
				recoveryGrants[grant.AgentID] = grant
			}
		}

		s.mu.RLock()
		defer s.mu.RUnlock()

		response := make([]Agent, 0, len(s.agents))
		for _, agent := range s.agents {
			agent.PresenceState = string(s.presence.Evaluate(agent.ID, now))
			if grant, ok := recoveryGrants[agent.ID]; ok {
				recovery := agentCertificateRecoveryGrantResponseFromRecord(grant, now)
				agent.CertificateRecovery = &recovery
			}
			agent.Runtime = normalizeAgentRuntime(agent.Runtime)
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

		s.metricsAuditMu.RLock()
		defer s.metricsAuditMu.RUnlock()

		writeJSON(w, http.StatusOK, s.metrics)
	}
}

func (s *Server) handleAudit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		s.metricsAuditMu.RLock()
		defer s.metricsAuditMu.RUnlock()

		writeJSON(w, http.StatusOK, s.auditTrail)
	}
}

func normalizeAgentRuntime(runtime AgentRuntime) AgentRuntime {
	if runtime.DCs == nil {
		runtime.DCs = []RuntimeDC{}
	}
	if runtime.Upstreams == nil {
		runtime.Upstreams = []RuntimeUpstream{}
	}
	if runtime.RecentEvents == nil {
		runtime.RecentEvents = []RuntimeEvent{}
	}

	return runtime
}
