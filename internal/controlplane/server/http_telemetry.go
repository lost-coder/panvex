package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const telemetryDetailBoostTTL = 10 * time.Minute

// telemetryDashboardLoadWindow bounds how far back the dashboard sparklines
// look. 40 minutes at the 2-minute Telemt polling cadence gives ~20 samples,
// which is enough for a legible mini-chart without making the payload heavy.
const telemetryDashboardLoadWindow = 40 * time.Minute

// telemetryDashboardEventLimit is the UI budget for the "Recent Events"
// feed. Stays in lock-step with the value passed to
// controlRoomRecentRuntimeEvents so both fields carry the same cadence.
const telemetryDashboardEventLimit = 8

func (s *Server) handleTelemetryDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		now := s.now()

		s.metricsAuditMu.RLock()
		metricSnapshots := len(s.metrics)
		s.metricsAuditMu.RUnlock()

		s.mu.RLock()
		items := make([]telemetryServerSummary, 0, len(s.agents))
		runtimeDistribution := make(map[string]int)
		// Snapshot just enough agent state to populate the enriched feed
		// outside the lock; touching the store under s.mu would risk a
		// write-starvation on hot paths.
		agentNames := make(map[string]string, len(s.agents))
		agentIDs := make([]string, 0, len(s.agents))
		for _, agent := range s.agents {
			boostExpiresAt := s.detailBoosts[agent.ID]
			summary := telemetrySummaryForAgent(agent, s.presence.Evaluate(agent.ID, now), now, boostExpiresAt)
			items = append(items, summary)
			mode := summary.Agent.Runtime.TransportMode
			if mode == "" {
				mode = "unknown"
			}
			runtimeDistribution[mode]++
			agentNames[agent.ID] = agent.NodeName
			agentIDs = append(agentIDs, agent.ID)
		}
		recentEvents := controlRoomRecentRuntimeEvents(s.agents, telemetryDashboardEventLimit)
		recentEventsEnriched := dashboardRecentEvents(s.agents, telemetryDashboardEventLimit)
		fleet := controlRoomFleetFromState(s.agents, s.instances, metricSnapshots, s.presence, now)
		s.mu.RUnlock()

		loadSeries := s.dashboardAgentLoadSeries(r.Context(), agentIDs, now)

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
			RecentEvents:        recentEventsEnriched,
			AgentLoadSeries:     loadSeries,
		})
	}
}

// dashboardRecentEvents tags each runtime event with the agent that
// emitted it so the web dashboard can render "node-name · message"
// without a second round-trip. Sorted newest-first, capped at `limit`.
func dashboardRecentEvents(agents map[string]Agent, limit int) []telemetryRecentEvent {
	if limit <= 0 {
		return []telemetryRecentEvent{}
	}
	result := make([]telemetryRecentEvent, 0)
	for _, agent := range agents {
		for _, ev := range agent.Runtime.RecentEvents {
			result = append(result, telemetryRecentEvent{
				Sequence:      ev.Sequence,
				TimestampUnix: ev.TimestampUnix,
				EventType:     ev.EventType,
				Context:       ev.Context,
				AgentID:       agent.ID,
				NodeName:      agent.NodeName,
			})
		}
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].TimestampUnix == result[right].TimestampUnix {
			return result[left].Sequence > result[right].Sequence
		}
		return result[left].TimestampUnix > result[right].TimestampUnix
	})
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

// dashboardAgentLoadSeries pulls the last telemetryDashboardLoadWindow
// of raw CPU/MEM samples for each agent so the dashboard can render
// sparklines without a separate round-trip per row. Q2.U-P-01: a single
// bulk SQL replaces the previous per-agent SELECT loop. Missing agents
// (no rows in the window) yield empty slices so a sparse fleet still
// produces a fully-populated payload.
func (s *Server) dashboardAgentLoadSeries(
	ctx context.Context,
	agentIDs []string,
	now time.Time,
) []telemetryAgentLoadSeries {
	out := make([]telemetryAgentLoadSeries, 0, len(agentIDs))
	if s.store == nil {
		return out
	}
	from := now.UTC().Add(-telemetryDashboardLoadWindow)
	to := now.UTC()
	bulk, err := s.store.ListServerLoadPointsForAgents(ctx, agentIDs, from, to)
	if err != nil {
		s.logger.Warn("dashboard load series bulk fetch failed", "error", err)
		// Empty slices for every agent so the FE still renders.
		for _, id := range agentIDs {
			out = append(out, telemetryAgentLoadSeries{AgentID: id, CPUPct: []float64{}, MemPct: []float64{}})
		}
		return out
	}
	for _, id := range agentIDs {
		points := bulk[id]
		cpu := make([]float64, 0, len(points))
		mem := make([]float64, 0, len(points))
		for _, p := range points {
			cpu = append(cpu, p.CPUPctAvg)
			mem = append(mem, p.MemPctAvg)
		}
		out = append(out, telemetryAgentLoadSeries{AgentID: id, CPUPct: cpu, MemPct: mem})
	}
	return out
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
			writeError(w, http.StatusNotFound, msgServerNotFound)
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
			writeError(w, http.StatusNotFound, msgServerNotFound)
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
			writeError(w, http.StatusNotFound, msgServerNotFound)
			return
		}

		job, err := s.jobs.Enqueue(r.Context(), jobs.CreateJobInput{
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
