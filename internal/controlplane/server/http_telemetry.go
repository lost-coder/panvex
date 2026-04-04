package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/panvex/panvex/internal/controlplane/jobs"
	"github.com/panvex/panvex/internal/controlplane/storage"
)

const telemetryDetailBoostTTL = 10 * time.Minute

func (s *Server) handleTelemetryDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		now := s.now()

		s.mu.RLock()
		items := make([]telemetryServerSummary, 0, len(s.agents))
		runtimeDistribution := make(map[string]int)
		for _, agent := range s.agents {
			boostExpiresAt := s.detailBoosts[agent.ID]
			summary := telemetrySummaryForAgent(agent, s.presence.Evaluate(agent.ID, now), now, boostExpiresAt)
			items = append(items, summary)
			mode := summary.Agent.Runtime.TransportMode
			if mode == "" {
				mode = "unknown"
			}
			runtimeDistribution[mode]++
		}
		recentEvents := controlRoomRecentRuntimeEvents(s.agents, 8)
		fleet := controlRoomFleetFromState(s.agents, s.instances, s.metrics, s.presence, now)
		s.mu.RUnlock()

		sortTelemetrySummaries(items)
		attention := make([]telemetryAttentionItem, 0, 5)
		for _, item := range items {
			if item.Severity == "good" && item.RuntimeFreshness.State == "fresh" {
				continue
			}
			attention = append(attention, telemetryAttentionItem{
				AgentID:          item.Agent.ID,
				NodeName:         item.Agent.NodeName,
				FleetGroupID:     item.Agent.FleetGroupID,
				Severity:         item.Severity,
				Reason:           item.Reason,
				PresenceState:    item.Agent.PresenceState,
				Runtime:          item.Agent.Runtime,
				RuntimeFreshness: item.RuntimeFreshness,
				DetailBoost:      item.DetailBoost,
			})
			if len(attention) == 5 {
				break
			}
		}

		writeJSON(w, http.StatusOK, telemetryDashboardResponse{
			Fleet:               fleet,
			Attention:           attention,
			ServerCards:         items,
			RuntimeDistribution: runtimeDistribution,
			RecentRuntimeEvents: recentEvents,
		})
	}
}

func (s *Server) handleTelemetryServers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		now := s.now()

		s.mu.RLock()
		items := make([]telemetryServerSummary, 0, len(s.agents))
		for _, agent := range s.agents {
			items = append(items, telemetrySummaryForAgent(agent, s.presence.Evaluate(agent.ID, now), now, s.detailBoosts[agent.ID]))
		}
		s.mu.RUnlock()

		sortTelemetrySummaries(items)
		writeJSON(w, http.StatusOK, telemetryServersResponse{Servers: items})
	}
}

func (s *Server) handleTelemetryServerDetail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		now := s.now()

		s.mu.RLock()
		agent, ok := s.agents[agentID]
		boostExpiresAt := s.detailBoosts[agentID]
		initializationCooldownExpiresAt := s.initializationWatchCooldowns[agentID]
		s.mu.RUnlock()
		if !ok {
			writeError(w, http.StatusNotFound, "server not found")
			return
		}

		diagnostics := telemetryDiagnosticsResponse{}
		securityInventory := telemetrySecurityInventoryResponse{}
		if s.store != nil {
			if record, err := s.store.GetTelemetryDiagnosticsCurrent(r.Context(), agentID); err == nil {
				diagnostics = telemetryDiagnosticsResponse{
					State:           record.State,
					StateReason:     record.StateReason,
					SystemInfo:      decodeJSONMap(record.SystemInfoJSON),
					EffectiveLimits: decodeJSONMap(record.EffectiveLimitsJSON),
					SecurityPosture: decodeJSONMap(record.SecurityPostureJSON),
					MinimalAll:      decodeJSONMap(record.MinimalAllJSON),
					MEPool:          decodeJSONMap(record.MEPoolJSON),
					DcsDetail:       decodeJSONMap(record.DcsJSON),
				}
			}
			if record, err := s.store.GetTelemetrySecurityInventoryCurrent(r.Context(), agentID); err == nil {
				securityInventory = telemetrySecurityInventoryResponse{
					State:        record.State,
					StateReason:  record.StateReason,
					Enabled:      record.Enabled,
					EntriesTotal: record.EntriesTotal,
					Entries:      decodeJSONStringSlice(record.EntriesJSON),
				}
			}
		}

		writeJSON(w, http.StatusOK, telemetryServerDetailResponse{
			Server:              telemetrySummaryForAgent(agent, s.presence.Evaluate(agentID, now), now, boostExpiresAt),
			InitializationWatch: telemetryInitializationWatchForAgent(agent, now, initializationCooldownExpiresAt),
			Diagnostics:         diagnostics,
			SecurityInventory:   securityInventory,
		})
	}
}

func (s *Server) handleTelemetryServerDetailBoost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		now := s.now()

		s.mu.Lock()
		if _, ok := s.agents[agentID]; !ok {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, "server not found")
			return
		}
		expiresAt := now.UTC().Add(telemetryDetailBoostTTL)
		s.detailBoosts[agentID] = expiresAt
		s.mu.Unlock()
		if s.store != nil {
			if err := s.store.PutTelemetryDetailBoost(r.Context(), storage.TelemetryDetailBoostRecord{
				AgentID:   agentID,
				ExpiresAt: expiresAt,
				UpdatedAt: now.UTC(),
			}); err != nil {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}

		writeJSON(w, http.StatusOK, telemetryDetailBoostResponse{
			Active:           true,
			ExpiresAtUnix:    expiresAt.Unix(),
			RemainingSeconds: int64(telemetryDetailBoostTTL.Seconds()),
		})
	}
}

func (s *Server) handleTelemetryServerRefreshDiagnostics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		agentID := chi.URLParam(r, "id")
		now := s.now()

		s.mu.RLock()
		agent, ok := s.agents[agentID]
		s.mu.RUnlock()
		if !ok {
			writeError(w, http.StatusNotFound, "server not found")
			return
		}

		job, err := s.jobs.Enqueue(jobs.CreateJobInput{
			Action:         jobs.ActionTelemetryRefreshDiagnostics,
			TargetAgentIDs: []string{agentID},
			TTL:            time.Minute,
			IdempotencyKey: "telemetry-refresh:" + agentID + ":" + now.UTC().Format(time.RFC3339Nano),
			ActorID:        user.ID,
			ReadOnlyAgents: map[string]bool{agentID: agent.ReadOnly},
		}, now.UTC())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}

		s.notifyAgentSessions(job.TargetAgentIDs)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"job_id": job.ID,
			"status": "queued",
		})
	}
}

func sortTelemetryAttention(items []telemetryAttentionItem) {
	sort.Slice(items, func(left int, right int) bool {
		leftRank := telemetrySeverityRank(items[left].Severity)
		rightRank := telemetrySeverityRank(items[right].Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		return items[left].NodeName < items[right].NodeName
	})
}

func decodeJSONMap(raw string) map[string]any {
	if raw == "" {
		return map[string]any{}
	}
	result := make(map[string]any)
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return map[string]any{}
	}
	return result
}

func decodeJSONStringSlice(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var result []string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return []string{}
	}
	return result
}
