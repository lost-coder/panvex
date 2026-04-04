package server

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/panvex/panvex/internal/controlplane/presence"
	"github.com/panvex/panvex/internal/controlplane/storage"
	controltelemetry "github.com/panvex/panvex/internal/controlplane/telemetry"
)

const (
	telemetryRuntimeStaleAfter = 30 * time.Second
	telemetryInitializationWatchCooldown = 90 * time.Second
)

type telemetryFreshnessResponse struct {
	State          string `json:"state"`
	ObservedAtUnix int64  `json:"observed_at_unix"`
}

type telemetryDetailBoostResponse struct {
	Active           bool  `json:"active"`
	ExpiresAtUnix    int64 `json:"expires_at_unix"`
	RemainingSeconds int64 `json:"remaining_seconds"`
}

type telemetryAttentionItem struct {
	AgentID        string                    `json:"agent_id"`
	NodeName       string                    `json:"node_name"`
	FleetGroupID   string                    `json:"fleet_group_id"`
	Severity       string                    `json:"severity"`
	Reason         string                    `json:"reason"`
	PresenceState  string                    `json:"presence_state"`
	Runtime        AgentRuntime              `json:"runtime"`
	RuntimeFreshness telemetryFreshnessResponse `json:"runtime_freshness"`
	DetailBoost    telemetryDetailBoostResponse `json:"detail_boost"`
}

type telemetryServerSummary struct {
	Agent           Agent                     `json:"agent"`
	Severity        string                    `json:"severity"`
	Reason          string                    `json:"reason"`
	RuntimeFreshness telemetryFreshnessResponse `json:"runtime_freshness"`
	DetailBoost     telemetryDetailBoostResponse `json:"detail_boost"`
}

type telemetryDashboardResponse struct {
	Fleet              fleetResponse             `json:"fleet"`
	Attention          []telemetryAttentionItem  `json:"attention"`
	ServerCards        []telemetryServerSummary  `json:"server_cards"`
	RuntimeDistribution map[string]int           `json:"runtime_distribution"`
	RecentRuntimeEvents []RuntimeEvent           `json:"recent_runtime_events"`
}

type telemetryServersResponse struct {
	Servers []telemetryServerSummary `json:"servers"`
}

type telemetryServerDetailResponse struct {
	Server            telemetryServerSummary             `json:"server"`
	InitializationWatch telemetryInitializationWatchResponse `json:"initialization_watch"`
	Diagnostics       telemetryDiagnosticsResponse       `json:"diagnostics"`
	SecurityInventory telemetrySecurityInventoryResponse `json:"security_inventory"`
}

type telemetryInitializationWatchResponse struct {
	Visible                    bool    `json:"visible"`
	Mode                       string  `json:"mode"`
	RemainingSeconds           int64   `json:"remaining_seconds"`
	CompletedAtUnix            int64   `json:"completed_at_unix"`
	StartupStatus              string  `json:"startup_status"`
	StartupStage               string  `json:"startup_stage"`
	StartupProgressPct         float64 `json:"startup_progress_pct"`
	InitializationStatus       string  `json:"initialization_status"`
	InitializationStage        string  `json:"initialization_stage"`
	InitializationProgressPct  float64 `json:"initialization_progress_pct"`
}

type telemetryDiagnosticsResponse struct {
	State           string         `json:"state"`
	StateReason     string         `json:"state_reason"`
	SystemInfo      map[string]any `json:"system_info"`
	EffectiveLimits map[string]any `json:"effective_limits"`
	SecurityPosture map[string]any `json:"security_posture"`
	MinimalAll      map[string]any `json:"minimal_all"`
	MEPool          map[string]any `json:"me_pool"`
	DcsDetail       map[string]any `json:"dcs_detail"`
}

type telemetrySecurityInventoryResponse struct {
	State        string   `json:"state"`
	StateReason  string   `json:"state_reason"`
	Enabled      bool     `json:"enabled"`
	EntriesTotal int      `json:"entries_total"`
	Entries      []string `json:"entries"`
}

func runtimeCurrentRecordFromAgent(agent Agent) storage.TelemetryRuntimeCurrentRecord {
	return storage.TelemetryRuntimeCurrentRecord{
		AgentID:                   agent.ID,
		ObservedAt:                agent.Runtime.UpdatedAt,
		State:                     "fresh",
		StateReason:               "",
		ReadOnly:                  agent.ReadOnly,
		AcceptingNewConnections:   agent.Runtime.AcceptingNewConnections,
		MERuntimeReady:            agent.Runtime.MERuntimeReady,
		ME2DCFallbackEnabled:      agent.Runtime.ME2DCFallbackEnabled,
		UseMiddleProxy:            agent.Runtime.UseMiddleProxy,
		StartupStatus:             agent.Runtime.StartupStatus,
		StartupStage:              agent.Runtime.StartupStage,
		StartupProgressPct:        agent.Runtime.StartupProgressPct,
		InitializationStatus:      agent.Runtime.InitializationStatus,
		Degraded:                  agent.Runtime.Degraded,
		InitializationStage:       agent.Runtime.InitializationStage,
		InitializationProgressPct: agent.Runtime.InitializationProgressPct,
		TransportMode:             agent.Runtime.TransportMode,
		CurrentConnections:        agent.Runtime.CurrentConnections,
		CurrentConnectionsME:      agent.Runtime.CurrentConnectionsME,
		CurrentConnectionsDirect:  agent.Runtime.CurrentConnectionsDirect,
		ActiveUsers:               agent.Runtime.ActiveUsers,
		UptimeSeconds:             agent.Runtime.UptimeSeconds,
		ConnectionsTotal:          agent.Runtime.ConnectionsTotal,
		ConnectionsBadTotal:       agent.Runtime.ConnectionsBadTotal,
		HandshakeTimeoutsTotal:    agent.Runtime.HandshakeTimeoutsTotal,
		ConfiguredUsers:           agent.Runtime.ConfiguredUsers,
		DCCoveragePct:             agent.Runtime.DCCoveragePct,
		HealthyUpstreams:          agent.Runtime.HealthyUpstreams,
		TotalUpstreams:            agent.Runtime.TotalUpstreams,
	}
}

func runtimeDCRecordsFromAgent(agent Agent) []storage.TelemetryRuntimeDCRecord {
	result := make([]storage.TelemetryRuntimeDCRecord, 0, len(agent.Runtime.DCs))
	for _, dc := range agent.Runtime.DCs {
		result = append(result, storage.TelemetryRuntimeDCRecord{
			AgentID:            agent.ID,
			DC:                 dc.DC,
			ObservedAt:         agent.Runtime.UpdatedAt,
			AvailableEndpoints: dc.AvailableEndpoints,
			AvailablePct:       dc.AvailablePct,
			RequiredWriters:    dc.RequiredWriters,
			AliveWriters:       dc.AliveWriters,
			CoveragePct:        dc.CoveragePct,
			RTTMs:              dc.RTTMs,
			Load:               float64(dc.Load),
		})
	}

	return result
}

func runtimeUpstreamRecordsFromAgent(agent Agent) []storage.TelemetryRuntimeUpstreamRecord {
	result := make([]storage.TelemetryRuntimeUpstreamRecord, 0, len(agent.Runtime.Upstreams))
	for _, upstream := range agent.Runtime.Upstreams {
		result = append(result, storage.TelemetryRuntimeUpstreamRecord{
			AgentID:            agent.ID,
			UpstreamID:         upstream.UpstreamID,
			ObservedAt:         agent.Runtime.UpdatedAt,
			RouteKind:          upstream.RouteKind,
			Address:            upstream.Address,
			Healthy:            upstream.Healthy,
			Fails:              upstream.Fails,
			EffectiveLatencyMs: upstream.EffectiveLatencyMs,
		})
	}

	return result
}

func runtimeEventRecordsFromAgent(agent Agent) []storage.TelemetryRuntimeEventRecord {
	result := make([]storage.TelemetryRuntimeEventRecord, 0, len(agent.Runtime.RecentEvents))
	for _, event := range agent.Runtime.RecentEvents {
		result = append(result, storage.TelemetryRuntimeEventRecord{
			AgentID:    agent.ID,
			Sequence:   int64(event.Sequence),
			ObservedAt: agent.Runtime.UpdatedAt,
			Timestamp:  time.Unix(event.TimestampUnix, 0).UTC(),
			EventType:  event.EventType,
			Context:    event.Context,
			Severity:   runtimeEventSeverity(event),
		})
	}

	return result
}

func runtimeEventSeverity(event RuntimeEvent) string {
	text := strings.ToLower(event.EventType + " " + event.Context)
	switch {
	case strings.Contains(text, "offline"), strings.Contains(text, "error"), strings.Contains(text, "dropped"), strings.Contains(text, "failed"):
		return "bad"
	case strings.Contains(text, "warn"), strings.Contains(text, "degrad"), strings.Contains(text, "timeout"):
		return "warn"
	default:
		return "good"
	}
}

func restoreAgentRuntimeFromStorage(agent Agent, runtime storage.TelemetryRuntimeCurrentRecord, dcs []storage.TelemetryRuntimeDCRecord, upstreams []storage.TelemetryRuntimeUpstreamRecord, events []storage.TelemetryRuntimeEventRecord) Agent {
	agent.Runtime = AgentRuntime{
		AcceptingNewConnections:   runtime.AcceptingNewConnections,
		MERuntimeReady:            runtime.MERuntimeReady,
		ME2DCFallbackEnabled:      runtime.ME2DCFallbackEnabled,
		UseMiddleProxy:            runtime.UseMiddleProxy,
		StartupStatus:             runtime.StartupStatus,
		StartupStage:              runtime.StartupStage,
		StartupProgressPct:        runtime.StartupProgressPct,
		InitializationStatus:      runtime.InitializationStatus,
		Degraded:                  runtime.Degraded,
		LifecycleState:            runtimeLifecycleStateFromCurrent(runtime),
		InitializationStage:       runtime.InitializationStage,
		InitializationProgressPct: runtime.InitializationProgressPct,
		TransportMode:             runtime.TransportMode,
		CurrentConnections:        runtime.CurrentConnections,
		CurrentConnectionsME:      runtime.CurrentConnectionsME,
		CurrentConnectionsDirect:  runtime.CurrentConnectionsDirect,
		ActiveUsers:               runtime.ActiveUsers,
		UptimeSeconds:             runtime.UptimeSeconds,
		ConnectionsTotal:          runtime.ConnectionsTotal,
		ConnectionsBadTotal:       runtime.ConnectionsBadTotal,
		HandshakeTimeoutsTotal:    runtime.HandshakeTimeoutsTotal,
		ConfiguredUsers:           runtime.ConfiguredUsers,
		DCCoveragePct:             runtime.DCCoveragePct,
		HealthyUpstreams:          runtime.HealthyUpstreams,
		TotalUpstreams:            runtime.TotalUpstreams,
		UpdatedAt:                 runtime.ObservedAt.UTC(),
	}

	agent.Runtime.DCs = make([]RuntimeDC, 0, len(dcs))
	for _, dc := range dcs {
		agent.Runtime.DCs = append(agent.Runtime.DCs, RuntimeDC{
			DC:                 dc.DC,
			AvailableEndpoints: dc.AvailableEndpoints,
			AvailablePct:       dc.AvailablePct,
			RequiredWriters:    dc.RequiredWriters,
			AliveWriters:       dc.AliveWriters,
			CoveragePct:        dc.CoveragePct,
			RTTMs:              dc.RTTMs,
			Load:               int(dc.Load),
		})
	}

	agent.Runtime.Upstreams = make([]RuntimeUpstream, 0, len(upstreams))
	for _, upstream := range upstreams {
		agent.Runtime.Upstreams = append(agent.Runtime.Upstreams, RuntimeUpstream{
			UpstreamID:         upstream.UpstreamID,
			RouteKind:          upstream.RouteKind,
			Address:            upstream.Address,
			Healthy:            upstream.Healthy,
			Fails:              upstream.Fails,
			EffectiveLatencyMs: upstream.EffectiveLatencyMs,
		})
	}

	agent.Runtime.RecentEvents = make([]RuntimeEvent, 0, len(events))
	for _, event := range events {
		agent.Runtime.RecentEvents = append(agent.Runtime.RecentEvents, RuntimeEvent{
			Sequence:      uint64(event.Sequence),
			TimestampUnix: event.Timestamp.UTC().Unix(),
			EventType:     event.EventType,
			Context:       event.Context,
		})
	}

	return agent
}

func runtimeLifecycleStateFromCurrent(runtime storage.TelemetryRuntimeCurrentRecord) string {
	switch {
	case runtime.Degraded:
		return "degraded"
	case runtime.InitializationStatus != "" && runtime.InitializationStatus != "ready":
		return runtime.InitializationStatus
	case runtime.StartupStatus != "" && runtime.StartupStatus != "ready":
		return runtime.StartupStatus
	case !runtime.AcceptingNewConnections || !runtime.MERuntimeReady:
		return "starting"
	default:
		return "ready"
	}
}

func (s *Server) restoreStoredTelemetry() error {
	if s.store == nil {
		return nil
	}

	boosts, err := s.store.ListTelemetryDetailBoosts(context.Background())
	if err != nil {
		return err
	}
	now := s.now().UTC()
	for _, boost := range boosts {
		if !boost.ExpiresAt.After(now) {
			_ = s.store.DeleteTelemetryDetailBoost(context.Background(), boost.AgentID)
			continue
		}
		s.detailBoosts[boost.AgentID] = boost.ExpiresAt.UTC()
	}

	for agentID, agent := range s.agents {
		runtime, err := s.store.GetTelemetryRuntimeCurrent(context.Background(), agentID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				continue
			}
			return err
		}
		dcs, err := s.store.ListTelemetryRuntimeDCs(context.Background(), agentID)
		if err != nil {
			return err
		}
		upstreams, err := s.store.ListTelemetryRuntimeUpstreams(context.Background(), agentID)
		if err != nil {
			return err
		}
		events, err := s.store.ListTelemetryRuntimeEvents(context.Background(), agentID, 10)
		if err != nil {
			return err
		}
		s.agents[agentID] = restoreAgentRuntimeFromStorage(agent, runtime, dcs, upstreams, events)
	}

	return nil
}

func telemetryFreshnessForRuntime(runtime AgentRuntime, now time.Time) telemetryFreshnessResponse {
	freshness := controltelemetry.FreshnessForObservedAt(runtime.UpdatedAt, now, telemetryRuntimeStaleAfter)
	return telemetryFreshnessResponse{
		State:          freshness.State,
		ObservedAtUnix: freshness.ObservedAtUnix,
	}
}

func telemetryBoostStateForAgent(expiresAt time.Time, now time.Time) telemetryDetailBoostResponse {
	boost := controltelemetry.DetailBoostState(expiresAt, now)
	return telemetryDetailBoostResponse{
		Active:           boost.Active,
		ExpiresAtUnix:    boost.ExpiresAtUnix,
		RemainingSeconds: boost.RemainingSeconds,
	}
}

func runtimeNeedsInitializationWatch(runtime AgentRuntime) bool {
	switch {
	case runtime.StartupStatus != "" && runtime.StartupStatus != "ready":
		return true
	case runtime.InitializationStatus != "" && runtime.InitializationStatus != "ready":
		return true
	case !runtime.AcceptingNewConnections || !runtime.MERuntimeReady:
		return true
	default:
		return false
	}
}

func telemetryInitializationWatchForAgent(agent Agent, now time.Time, cooldownExpiresAt time.Time) telemetryInitializationWatchResponse {
	runtime := normalizeAgentRuntime(agent.Runtime)
	if runtimeNeedsInitializationWatch(runtime) {
		return telemetryInitializationWatchResponse{
			Visible:                   true,
			Mode:                      "active",
			StartupStatus:             runtime.StartupStatus,
			StartupStage:              runtime.StartupStage,
			StartupProgressPct:        runtime.StartupProgressPct,
			InitializationStatus:      runtime.InitializationStatus,
			InitializationStage:       runtime.InitializationStage,
			InitializationProgressPct: runtime.InitializationProgressPct,
		}
	}

	if cooldownExpiresAt.After(now.UTC()) {
		completedAt := cooldownExpiresAt.UTC().Add(-telemetryInitializationWatchCooldown)
		return telemetryInitializationWatchResponse{
			Visible:                   true,
			Mode:                      "cooldown",
			RemainingSeconds:          int64(cooldownExpiresAt.UTC().Sub(now.UTC()).Seconds()),
			CompletedAtUnix:           completedAt.Unix(),
			StartupStatus:             runtime.StartupStatus,
			StartupStage:              runtime.StartupStage,
			StartupProgressPct:        runtime.StartupProgressPct,
			InitializationStatus:      runtime.InitializationStatus,
			InitializationStage:       runtime.InitializationStage,
			InitializationProgressPct: runtime.InitializationProgressPct,
		}
	}

	return telemetryInitializationWatchResponse{
		Visible:                   false,
		Mode:                      "hidden",
		StartupStatus:             runtime.StartupStatus,
		StartupStage:              runtime.StartupStage,
		StartupProgressPct:        runtime.StartupProgressPct,
		InitializationStatus:      runtime.InitializationStatus,
		InitializationStage:       runtime.InitializationStage,
		InitializationProgressPct: runtime.InitializationProgressPct,
	}
}

func telemetrySeverityAndReason(agent Agent, presenceState presence.State, freshness telemetryFreshnessResponse) (string, string) {
	return controltelemetry.SeverityAndReason(controltelemetry.SeverityInput{
		PresenceState:           presenceState,
		ReadOnly:                agent.ReadOnly,
		AcceptingNewConnections: agent.Runtime.AcceptingNewConnections,
		Degraded:                agent.Runtime.Degraded,
		StartupStatus:           agent.Runtime.StartupStatus,
		DCCoveragePct:           agent.Runtime.DCCoveragePct,
		HealthyUpstreams:        agent.Runtime.HealthyUpstreams,
		TotalUpstreams:          agent.Runtime.TotalUpstreams,
	}, controltelemetry.Freshness{
		State:          freshness.State,
		ObservedAtUnix: freshness.ObservedAtUnix,
	})
}

func telemetrySummaryForAgent(agent Agent, presenceState presence.State, now time.Time, boostExpiresAt time.Time) telemetryServerSummary {
	agent.PresenceState = string(presenceState)
	agent.Runtime = normalizeAgentRuntime(agent.Runtime)
	freshness := telemetryFreshnessForRuntime(agent.Runtime, now)
	severity, reason := telemetrySeverityAndReason(agent, presenceState, freshness)
	return telemetryServerSummary{
		Agent:            agent,
		Severity:         severity,
		Reason:           reason,
		RuntimeFreshness: freshness,
		DetailBoost:      telemetryBoostStateForAgent(boostExpiresAt, now),
	}
}

func sortTelemetrySummaries(items []telemetryServerSummary) {
	sort.Slice(items, func(left int, right int) bool {
		leftRank := telemetrySeverityRank(items[left].Severity)
		rightRank := telemetrySeverityRank(items[right].Severity)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		if items[left].RuntimeFreshness.State != items[right].RuntimeFreshness.State {
			return items[left].RuntimeFreshness.State > items[right].RuntimeFreshness.State
		}
		return items[left].Agent.NodeName < items[right].Agent.NodeName
	})
}

func telemetrySeverityRank(value string) int {
	return controltelemetry.SeverityRank(value)
}
