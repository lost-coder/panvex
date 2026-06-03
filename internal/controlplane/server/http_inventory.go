package server

import (
	"context"
	"net/http"

	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type fleetResponse struct {
	TotalAgents                   int `json:"total_agents"`
	OnlineAgents                  int `json:"online_agents"`
	DegradedAgents                int `json:"degraded_agents"`
	OfflineAgents                 int `json:"offline_agents"`
	TotalInstances                int `json:"total_instances"`
	MetricSnapshots               int `json:"metric_snapshots"`
	LiveConnections               int `json:"live_connections"`
	AcceptingNewConnectionsAgents int `json:"accepting_new_connections_agents"`
	MiddleProxyAgents             int `json:"middle_proxy_agents"`
	DCIssueAgents                 int `json:"dc_issue_agents"`
}

func (s *Server) handleFleet() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		metricSnapshots := s.metricSnapshotCount(r.Context())

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
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		// R-S-14: filter the agent list down to the operator's scope.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}
		recoveryGrants, err := s.loadAgentRecoveryGrants(r.Context())
		if err != nil {
			s.logger.Error("list agent certificate recovery grants failed", "error", err)
			writeError(w, http.StatusInternalServerError, msgInternalError)
			return
		}
		writeJSON(w, http.StatusOK, s.buildAgentsResponse(scope, recoveryGrants))
	}
}

// loadAgentRecoveryGrants returns the persisted recovery grants keyed
// by agent id. A nil store yields an empty map so the caller can stay
// branch-free.
func (s *Server) loadAgentRecoveryGrants(ctx context.Context) (map[string]storage.AgentCertificateRecoveryGrantRecord, error) {
	if s.store == nil {
		return map[string]storage.AgentCertificateRecoveryGrantRecord{}, nil
	}
	loaded, err := s.store.ListAgentCertificateRecoveryGrants(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]storage.AgentCertificateRecoveryGrantRecord, len(loaded))
	for _, grant := range loaded {
		out[grant.AgentID] = grant
	}
	return out, nil
}

// buildAgentsResponse produces the scoped, presence-augmented agent
// list under s.mu RLock. The lock window is intentionally narrow —
// no I/O happens inside it.
func (s *Server) buildAgentsResponse(scope FleetScopeAccess, recoveryGrants map[string]storage.AgentCertificateRecoveryGrantRecord) []Agent {
	now := s.now()
	s.mu.RLock()
	defer s.mu.RUnlock()
	response := make([]Agent, 0, len(s.agents))
	for _, agent := range s.agents {
		if !scope.IsAllowed(agent.FleetGroupID) {
			continue
		}
		agent.PresenceState = string(s.presence.Evaluate(agent.ID, now))
		if grant, ok := recoveryGrants[agent.ID]; ok {
			recovery := agentCertificateRecoveryGrantResponseFromRecord(grant, now)
			agent.CertificateRecovery = &recovery
		}
		agent.Runtime = normalizeAgentRuntime(agent.Runtime)
		response = append(response, agent)
	}
	return response
}

func (s *Server) handleInstances() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, user, err := s.requireSession(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		// R-S-14: filter instances by parent agent's fleet group.
		scope, ok := s.requireFleetScope(w, r, user)
		if !ok {
			return
		}

		s.mu.RLock()
		defer s.mu.RUnlock()

		response := make([]Instance, 0, len(s.instances))
		for _, instance := range s.instances {
			if !scope.Global {
				agent, agentOK := s.agents[instance.AgentID]
				if !agentOK || !scope.IsAllowed(agent.FleetGroupID) {
					continue
				}
			}
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

		snapshots, err := s.listMetricSnapshots(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list metric snapshots failed")
			return
		}

		writeJSON(w, http.StatusOK, snapshots)
	}
}

// listMetricSnapshots reads the last metric snapshots (≤512, oldest→newest)
// directly from the store, converting storage records to the wire type. The
// store query already applies the same order+limit the in-memory ring used to
// enforce (A2: s.metrics ring removed). A nil store (test fixtures with no
// persistence) yields an empty slice, matching the other nil-store handlers.
func (s *Server) listMetricSnapshots(ctx context.Context) ([]MetricSnapshot, error) {
	if s.store == nil {
		return []MetricSnapshot{}, nil
	}
	records, err := s.store.ListMetricSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	snapshots := make([]MetricSnapshot, 0, len(records))
	for _, record := range records {
		snapshots = append(snapshots, metricSnapshotFromRecord(record))
	}
	return snapshots, nil
}

// metricSnapshotCount returns the number of recent metric snapshots surfaced
// by the dashboards (fleet, control-room, telemetry). It mirrors the old
// len(s.metrics) semantics: the store query caps the result at 512, exactly
// the bound the in-memory ring used to enforce (A2). On a store error the
// count degrades to 0 rather than failing the dashboard — this is a cosmetic
// counter, not load-bearing data, so the surrounding handlers stay resilient.
func (s *Server) metricSnapshotCount(ctx context.Context) int {
	snapshots, err := s.listMetricSnapshots(ctx)
	if err != nil {
		s.logger.ErrorContext(ctx, "count metric snapshots failed", "error", err)
		return 0
	}
	return len(snapshots)
}

func (s *Server) handleAudit() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		// S25 T1: opt-in keyset pagination. Presence of the ?cursor=
		// query param routes through the store; legacy callers continue
		// to read the bounded in-memory ring buffer. The two paths
		// intentionally differ in event order: the legacy ring is
		// chronological-ascending (timeline replay), the cursor path
		// returns newest-first (operator browsing) so the UI can show
		// "page 1 = most recent" with a consistent ORDER BY across pages.
		if r.URL.Query().Has("cursor") {
			s.handleAuditCursor(w, r)
			return
		}

		trail, err := s.auditFirstPage(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list audit events failed")
			return
		}

		writeJSON(w, http.StatusOK, trail)
	}
}

// auditFirstPage reads the most recent audit events (≤auditFirstPageLimit,
// oldest→newest) directly from the store, converting storage records to the
// wire type. The store query already applies the same order+limit the in-memory
// ring used to enforce (A2: the audit ring was removed): ListAuditEvents with a
// limit of auditFirstPageLimit returns the last 1024 events in ascending
// order — byte-equivalent to the old snapshotAuditTrailLocked() output. A nil
// store (test fixtures with no persistence) yields an empty slice, matching the
// cursor branch's nil-store guard.
func (s *Server) auditFirstPage(ctx context.Context) ([]AuditEvent, error) {
	if s.store == nil {
		return []AuditEvent{}, nil
	}
	records, err := s.store.ListAuditEvents(ctx, auditFirstPageLimit)
	if err != nil {
		return nil, err
	}
	events := make([]AuditEvent, 0, len(records))
	for _, record := range records {
		events = append(events, auditEventFromRecord(record))
	}
	return events, nil
}

// auditCursorResponse is the wire shape of /api/audit?cursor=. Items match
// the AuditEvent struct used by the legacy ring response; next_cursor is the
// opaque base64-url string a client passes back as ?cursor= for the next
// page. Empty string means "no more pages".
type auditCursorResponse struct {
	Items      []AuditEvent `json:"items"`
	NextCursor string       `json:"next_cursor"`
}

// handleAuditCursor serves the cursor-paginated branch of /api/audit. Same
// scope semantics as the legacy branch (no per-row scope check — audit is
// admin-visible across the fleet).
func (s *Server) handleAuditCursor(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSON(w, http.StatusOK, auditCursorResponse{Items: []AuditEvent{}})
		return
	}
	createdAt, afterID, err := storage.DecodeKeysetCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid cursor")
		return
	}
	limit := parseCursorLimit(r)
	records, next, err := s.store.ListAuditEventsCursor(r.Context(), storage.ListAuditEventsCursorParams{
		Limit:          limit,
		AfterCreatedAt: createdAt,
		AfterID:        afterID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list audit events failed")
		return
	}
	items := make([]AuditEvent, 0, len(records))
	for _, rec := range records {
		items = append(items, auditEventFromRecord(rec))
	}
	writeJSON(w, http.StatusOK, auditCursorResponse{
		Items:      items,
		NextCursor: storage.EncodeKeysetCursor(next.AfterCreatedAt, next.AfterID),
	})
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

	// Direct-mode projection: the `Degraded` flag is sourced from
	// Telemt's /v1/runtime/initialization endpoint, which only describes
	// the ME pool initialization state. Direct nodes have no ME pool,
	// so the flag is either permanently set on some Telemt builds or
	// semantically meaningless. Clear it (and the matching lifecycle
	// label) so the dashboard does not paint healthy Direct nodes as
	// degraded. The mode-aware severity classifier in projections.go is
	// the second line of defense.
	if !runtime.UseMiddleProxy {
		runtime.Degraded = false
		if runtime.LifecycleState == "degraded" {
			runtime.LifecycleState = "ready"
		}
	}

	return runtime
}
