package server

import (
	"container/heap"
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

// telemetryDashboardSnapshot captures the read-side data the
// dashboard needs in a single pass under s.mu. Splitting it out keeps
// handleTelemetryDashboard's complexity below the 15-CC threshold.
type telemetryDashboardSnapshot struct {
	items                []telemetryServerSummary
	runtimeDistribution  map[string]int
	agentIDs             []string
	recentEvents         []RuntimeEvent
	recentEventsEnriched []telemetryRecentEvent
	fleet                fleetResponse
}

// collectTelemetryDashboardSnapshot walks the agents map under s.mu
// and returns the per-agent summaries plus the derived aggregate
// fields used by the dashboard payload. Agents outside the operator's
// scope are filtered out (R-S-14): summaries, recent events, and the
// fleet aggregate all see the scoped view, so a multi-tenant operator
// cannot infer the existence of "neighbour" fleets via metrics or the
// recent-events feed.
func (s *Server) collectTelemetryDashboardSnapshot(scope FleetScopeAccess, now time.Time, metricSnapshots int) telemetryDashboardSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	scopedAgents := make(map[string]Agent, len(s.agents))
	for id, agent := range s.agents {
		if !scope.IsAllowed(agent.FleetGroupID) {
			continue
		}
		scopedAgents[id] = agent
	}

	snapshot := telemetryDashboardSnapshot{
		items:               make([]telemetryServerSummary, 0, len(scopedAgents)),
		runtimeDistribution: make(map[string]int),
		agentIDs:            make([]string, 0, len(scopedAgents)),
	}
	for _, agent := range scopedAgents {
		boostExpiresAt := s.detailBoosts[agent.ID]
		summary := telemetrySummaryForAgent(agent, s.presence.Evaluate(agent.ID, now), now, boostExpiresAt)
		snapshot.items = append(snapshot.items, summary)
		mode := summary.Agent.Runtime.TransportMode
		if mode == "" {
			mode = "unknown"
		}
		snapshot.runtimeDistribution[mode]++
		snapshot.agentIDs = append(snapshot.agentIDs, agent.ID)
	}
	snapshot.recentEvents = controlRoomRecentRuntimeEvents(scopedAgents, telemetryDashboardEventLimit)
	snapshot.recentEventsEnriched = dashboardRecentEvents(scopedAgents, telemetryDashboardEventLimit)
	snapshot.fleet = controlRoomFleetFromState(scopedAgents, s.instances, metricSnapshots, s.presence, now)
	return snapshot
}

// buildTelemetryAttention picks up to 5 non-healthy summaries to surface
// in the dashboard's "needs attention" feed. Items are taken in the
// order produced by sortTelemetrySummaries, which orders worst-first.
func buildTelemetryAttention(items []telemetryServerSummary) []telemetryAttentionItem {
	attention := make([]telemetryAttentionItem, 0, 5)
	for _, item := range items {
		if (item.Severity == "good" || item.Severity == "ok") && item.RuntimeFreshness.State == "fresh" {
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
	return attention
}

func (s *Server) handleTelemetryDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}

		now := s.now()

		s.metricsAuditMu.RLock()
		metricSnapshots := len(s.metrics)
		s.metricsAuditMu.RUnlock()

		snapshot := s.collectTelemetryDashboardSnapshot(scope, now, metricSnapshots)

		loadSeries := s.dashboardAgentLoadSeries(r.Context(), snapshot.agentIDs, now)

		sortTelemetrySummaries(snapshot.items)
		attention := buildTelemetryAttention(snapshot.items)

		writeJSON(w, http.StatusOK, telemetryDashboardResponse{
			Fleet:               snapshot.fleet,
			Attention:           attention,
			ServerCards:         snapshot.items,
			RuntimeDistribution: snapshot.runtimeDistribution,
			RecentRuntimeEvents: snapshot.recentEvents,
			RecentEvents:        snapshot.recentEventsEnriched,
			AgentLoadSeries:     loadSeries,
		})
	}
}

// dashboardRecentEventHeap is a min-heap on (TimestampUnix, Sequence)
// so heap[0] is the OLDEST event we are currently keeping. We push
// every candidate; once size exceeds limit we pop the smallest. This
// keeps overall work at O(N log K) instead of the previous
// O(N·M log N·M) full sort.
type dashboardRecentEventHeap []telemetryRecentEvent

func (h dashboardRecentEventHeap) Len() int { return len(h) }
func (h dashboardRecentEventHeap) Less(left, right int) bool {
	if h[left].TimestampUnix == h[right].TimestampUnix {
		return h[left].Sequence < h[right].Sequence
	}
	return h[left].TimestampUnix < h[right].TimestampUnix
}
func (h dashboardRecentEventHeap) Swap(left, right int) { h[left], h[right] = h[right], h[left] }
func (h *dashboardRecentEventHeap) Push(x any)          { *h = append(*h, x.(telemetryRecentEvent)) }
func (h *dashboardRecentEventHeap) Pop() any {
	old := *h
	n := len(old)
	last := old[n-1]
	*h = old[:n-1]
	return last
}

// dashboardRecentEvents tags each runtime event with the agent that
// emitted it so the web dashboard can render "node-name · message"
// without a second round-trip. Sorted newest-first, capped at `limit`.
// Uses a bounded min-heap so the cost stays O(N log K) regardless of
// how many runtime events the fleet emitted in the window.
func dashboardRecentEvents(agents map[string]Agent, limit int) []telemetryRecentEvent {
	if limit <= 0 {
		return []telemetryRecentEvent{}
	}
	h := make(dashboardRecentEventHeap, 0, limit+1)
	for _, agent := range agents {
		for _, ev := range agent.Runtime.RecentEvents {
			candidate := telemetryRecentEvent{
				Sequence:      ev.Sequence,
				TimestampUnix: ev.TimestampUnix,
				EventType:     ev.EventType,
				Context:       ev.Context,
				AgentID:       agent.ID,
				NodeName:      agent.NodeName,
			}
			if h.Len() < limit {
				heap.Push(&h, candidate)
				continue
			}
			// Heap is full: skip events strictly older than the
			// current minimum, otherwise replace the min.
			oldest := h[0]
			if candidate.TimestampUnix < oldest.TimestampUnix ||
				(candidate.TimestampUnix == oldest.TimestampUnix && candidate.Sequence <= oldest.Sequence) {
				continue
			}
			h[0] = candidate
			heap.Fix(&h, 0)
		}
	}
	result := make([]telemetryRecentEvent, h.Len())
	// Drain: the heap pops oldest-first, so place each one at the
	// tail of the output and the final slice is newest-first.
	for i := len(result) - 1; i >= 0; i-- {
		result[i] = heap.Pop(&h).(telemetryRecentEvent)
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
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}

		now := s.now()

		s.mu.RLock()
		items := make([]telemetryServerSummary, 0, len(s.agents))
		for _, agent := range s.agents {
			if !scope.IsAllowed(agent.FleetGroupID) {
				continue
			}
			items = append(items, telemetrySummaryForAgent(agent, s.presence.Evaluate(agent.ID, now), now, s.detailBoosts[agent.ID]))
		}
		s.mu.RUnlock()

		sortTelemetrySummaries(items)
		writeJSON(w, http.StatusOK, telemetryServersResponse{Servers: items})
	}
}

func (s *Server) handleTelemetryServerDetail() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		scope, scopeOK := s.requireFleetScope(w, r, user)
		if !scopeOK {
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
		if !scope.IsAllowed(agent.FleetGroupID) {
			// Mirror the not-found path so cross-scope probes cannot
			// distinguish "exists but not yours" from "doesn't exist".
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
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		scope, scopeOK := s.requireFleetScope(w, r, user)
		if !scopeOK {
			return
		}

		agentID := chi.URLParam(r, "id")
		now := s.now()

		s.mu.Lock()
		agent, ok := s.agents[agentID]
		if !ok {
			s.mu.Unlock()
			writeError(w, http.StatusNotFound, msgServerNotFound)
			return
		}
		if !scope.IsAllowed(agent.FleetGroupID) {
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
		scope, scopeOK := s.requireFleetScope(w, r, user)
		if !scopeOK {
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
		if !scope.IsAllowed(agent.FleetGroupID) {
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
	sort.Slice(items, func(left, right int) bool {
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
