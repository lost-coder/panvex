package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/presence"
)

type controlRoomResponse struct {
	Onboarding          controlRoomOnboarding `json:"onboarding"`
	Fleet               fleetResponse         `json:"fleet"`
	Jobs                controlRoomJobs       `json:"jobs"`
	RecentActivity      []AuditEvent          `json:"recent_activity"`
	RecentRuntimeEvents []RuntimeEvent        `json:"recent_runtime_events"`
}

type controlRoomOnboarding struct {
	NeedsFirstServer      bool   `json:"needs_first_server"`
	SetupComplete         bool   `json:"setup_complete"`
	SuggestedFleetGroupID string `json:"suggested_fleet_group_id"`
}

type controlRoomJobs struct {
	Total   int `json:"total"`
	Queued  int `json:"queued"`
	Running int `json:"running"`
	Failed  int `json:"failed"`
}

func (s *Server) handleControlRoom() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, _, err := s.requireSession(r); err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		now := s.now()
		jobList := s.jobs.List()

		s.metricsAuditMu.RLock()
		recentActivity := controlRoomRecentActivity(s.snapshotAuditTrailLocked(), 5)
		metricSnapshots := len(s.metrics)
		s.metricsAuditMu.RUnlock()

		s.mu.RLock()
		fleet := controlRoomFleetFromState(s.agents, s.instances, metricSnapshots, s.presence, now)
		response := controlRoomResponse{
			Onboarding:          controlRoomOnboardingFromState(s.agents, s.instances),
			Fleet:               fleet,
			Jobs:                controlRoomJobsFromList(jobList),
			RecentActivity:      recentActivity,
			RecentRuntimeEvents: controlRoomRecentRuntimeEvents(s.agents, 5),
		}
		s.mu.RUnlock()

		writeJSON(w, http.StatusOK, response)
	}
}

func controlRoomOnboardingFromState(agents map[string]Agent, instances map[string]Instance) controlRoomOnboarding {
	const defaultScope = "default"

	setupComplete := len(agents) > 0 || len(instances) > 0
	response := controlRoomOnboarding{
		NeedsFirstServer:      !setupComplete,
		SetupComplete:         setupComplete,
		SuggestedFleetGroupID: defaultScope,
	}

	var candidate Agent
	var hasCandidate bool
	for _, agent := range agents {
		if !hasCandidate || agent.LastSeenAt.After(candidate.LastSeenAt) || (agent.LastSeenAt.Equal(candidate.LastSeenAt) && agent.ID < candidate.ID) {
			candidate = agent
			hasCandidate = true
		}
	}
	if !hasCandidate {
		return response
	}
	if candidate.FleetGroupID != "" {
		response.SuggestedFleetGroupID = candidate.FleetGroupID
	}

	return response
}

func controlRoomFleetFromState(agents map[string]Agent, instances map[string]Instance, metricSnapshots int, tracker *presence.Tracker, now time.Time) fleetResponse {
	response := fleetResponse{
		TotalAgents:     len(agents),
		TotalInstances:  len(instances),
		MetricSnapshots: metricSnapshots,
	}

	for agentID := range agents {
		agent := agents[agentID]
		response.LiveConnections += agent.Runtime.CurrentConnections
		if agent.Runtime.AcceptingNewConnections {
			response.AcceptingNewConnectionsAgents++
		}
		if agent.Runtime.UseMiddleProxy {
			response.MiddleProxyAgents++
		}
		if agent.Runtime.DCCoveragePct > 0 && agent.Runtime.DCCoveragePct < 100 {
			response.DCIssueAgents++
		}

		switch tracker.Evaluate(agentID, now) {
		case presence.StateOnline:
			response.OnlineAgents++
		case presence.StateDegraded:
			response.DegradedAgents++
		default:
			response.OfflineAgents++
		}
	}

	return response
}

func controlRoomRecentRuntimeEvents(agents map[string]Agent, limit int) []RuntimeEvent {
	if limit <= 0 {
		return []RuntimeEvent{}
	}

	result := make([]RuntimeEvent, 0)
	for _, agent := range agents {
		result = append(result, agent.Runtime.RecentEvents...)
	}
	if len(result) == 0 {
		return []RuntimeEvent{}
	}

	sort.Slice(result, func(left int, right int) bool {
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

func controlRoomJobsFromList(jobList []jobs.Job) controlRoomJobs {
	response := controlRoomJobs{
		Total: len(jobList),
	}

	for _, job := range jobList {
		switch job.Status {
		case jobs.StatusQueued:
			response.Queued++
		case jobs.StatusRunning:
			response.Running++
		case jobs.StatusFailed:
			response.Failed++
		}
	}

	return response
}

func controlRoomRecentActivity(auditTrail []AuditEvent, limit int) []AuditEvent {
	if limit <= 0 || len(auditTrail) == 0 {
		return []AuditEvent{}
	}

	result := make([]AuditEvent, 0, limit)
	for index := len(auditTrail) - 1; index >= 0 && len(result) < limit; index-- {
		if !includeControlRoomActivity(auditTrail[index].Action) {
			continue
		}
		result = append(result, auditTrail[index])
	}

	return result
}

func includeControlRoomActivity(action string) bool {
	switch action {
	case "auth.login", "auth.logout", "auth.me":
		return false
	default:
		return true
	}
}
