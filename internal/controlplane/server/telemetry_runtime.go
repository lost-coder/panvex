package server

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/presence"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	controltelemetry "github.com/lost-coder/panvex/internal/controlplane/telemetry"
)

const (
	telemetryRuntimeStaleAfter           = 90 * time.Second
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
	AgentID          string                       `json:"agent_id"`
	NodeName         string                       `json:"node_name"`
	FleetGroupID     string                       `json:"fleet_group_id"`
	Severity         string                       `json:"severity"`
	Reason           string                       `json:"reason"`
	PresenceState    string                       `json:"presence_state"`
	Runtime          AgentRuntime                 `json:"runtime"`
	RuntimeFreshness telemetryFreshnessResponse   `json:"runtime_freshness"`
	DetailBoost      telemetryDetailBoostResponse `json:"detail_boost"`
}

type telemetryServerSummary struct {
	Agent            Agent                        `json:"agent"`
	Severity         string                       `json:"severity"`
	Reason           string                       `json:"reason"`
	RuntimeFreshness telemetryFreshnessResponse   `json:"runtime_freshness"`
	DetailBoost      telemetryDetailBoostResponse `json:"detail_boost"`
	// TrafficBytes is the panel-side sum of TrafficUsedBytes across every
	// managed client deployed to this agent. The agent runtime payload
	// itself does not carry a per-node aggregate (Telemt reports per-user
	// usage), so we project the panel's clientUsage map here at response
	// time. Includes adopted/discovered clients tracked on this agent.
	TrafficBytes uint64 `json:"traffic_bytes"`
}

type telemetryDashboardResponse struct {
	Fleet               fleetResponse            `json:"fleet"`
	Attention           []telemetryAttentionItem `json:"attention"`
	ServerCards         []telemetryServerSummary `json:"server_cards"`
	RuntimeDistribution map[string]int           `json:"runtime_distribution"`
	// RecentRuntimeEvents carries the original aggregator payload for
	// backward-compatible consumers (ControlRoom still uses it as-is).
	RecentRuntimeEvents []RuntimeEvent `json:"recent_runtime_events"`
	// RecentEvents is the dashboard-specific enriched feed: same events
	// as RecentRuntimeEvents but tagged with the originating agent so
	// the UI can render "node-name · message" rows.
	RecentEvents []telemetryRecentEvent `json:"recent_events"`
	// AgentLoadSeries carries the last N raw CPU/MEM samples per agent
	// for dashboard mini-charts. Points are ordered oldest-first; an
	// empty/missing entry means the server has no history yet (newly
	// enrolled agent).
	AgentLoadSeries []telemetryAgentLoadSeries `json:"agent_load_series"`
}

// telemetryRecentEvent is the runtime-event envelope exposed to the web
// dashboard. It is intentionally tagged with the originating agent so
// the UI can render "node-name · message" without a second round-trip
// to resolve the agent id.
type telemetryRecentEvent struct {
	Sequence      uint64 `json:"sequence"`
	TimestampUnix int64  `json:"timestamp_unix"`
	EventType     string `json:"event_type"`
	Context       string `json:"context"`
	AgentID       string `json:"agent_id"`
	NodeName      string `json:"node_name"`
}

// telemetryAgentLoadSeries is a short CPU/MEM timeseries for one agent,
// rendered as sparklines on the dashboard. Arrays are the same length
// (paired samples) so the UI does not need to align timestamps.
type telemetryAgentLoadSeries struct {
	AgentID string    `json:"agent_id"`
	CPUPct  []float64 `json:"cpu_pct"`
	MemPct  []float64 `json:"mem_pct"`
}

type telemetryServersResponse struct {
	Servers []telemetryServerSummary `json:"servers"`
}

type telemetryServerDetailResponse struct {
	Server              telemetryServerSummary               `json:"server"`
	InitializationWatch telemetryInitializationWatchResponse `json:"initialization_watch"`
	Diagnostics         telemetryDiagnosticsResponse         `json:"diagnostics"`
	SecurityInventory   telemetrySecurityInventoryResponse   `json:"security_inventory"`
}

type telemetryInitializationWatchResponse struct {
	Visible                   bool    `json:"visible"`
	Mode                      string  `json:"mode"`
	RemainingSeconds          int64   `json:"remaining_seconds"`
	CompletedAtUnix           int64   `json:"completed_at_unix"`
	StartupStatus             string  `json:"startup_status"`
	StartupStage              string  `json:"startup_stage"`
	StartupProgressPct        float64 `json:"startup_progress_pct"`
	InitializationStatus      string  `json:"initialization_status"`
	InitializationStage       string  `json:"initialization_stage"`
	InitializationProgressPct float64 `json:"initialization_progress_pct"`
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
	blob, err := json.Marshal(agent.Runtime)
	if err != nil {
		// AgentRuntime состоит только из JSON-кодируемых типов; Marshal
		// может упасть лишь на программной ошибке (например, NaN во float
		// из будущего поля). Пустой blob вместо паники снапшот-пути:
		// runtimeFromCurrentRecord вернёт zero-runtime, что эквивалентно
		// "нет персистентного состояния".
		blob = []byte("{}")
	}
	return storage.TelemetryRuntimeCurrentRecord{
		AgentID:     agent.ID,
		ObservedAt:  agent.Runtime.UpdatedAt,
		RuntimeJSON: string(blob),
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
		return "ok"
	}
}

func runtimeFromCurrentRecord(record storage.TelemetryRuntimeCurrentRecord) AgentRuntime {
	var runtime AgentRuntime
	if record.RuntimeJSON != "" {
		if err := json.Unmarshal([]byte(record.RuntimeJSON), &runtime); err != nil {
			// Повреждённый blob = отсутствие персистентного состояния;
			// следующий снапшот агента перезапишет строку целиком.
			runtime = AgentRuntime{}
		}
	}
	// Колонка — источник истины для часов наблюдения (по ней ORDER BY);
	// updated_at из blob'а перезаписывается на неё.
	runtime.UpdatedAt = record.ObservedAt.UTC()
	return runtime
}

func restoreAgentRuntimeFromStorage(agent Agent, runtime storage.TelemetryRuntimeCurrentRecord) Agent {
	agent.Runtime = runtimeFromCurrentRecord(runtime)
	return agent
}

func (s *Server) restoreStoredTelemetry(ctx context.Context) error {
	if s.store == nil {
		return nil
	}

	// Detail boosts (F4) are ephemeral in-memory-only state and are not
	// restored on boot — a panel restart simply clears any active boost.

	// P3-3.1: the full AgentRuntime (including DCs/Upstreams/RecentEvents)
	// lives in each row's runtime_json blob, so a single bulk read of
	// telemt_runtime_current rehydrates every agent — no per-projection
	// bulk queries anymore.
	currents, err := s.store.ListTelemetryRuntimeCurrent(ctx)
	if err != nil {
		return err
	}

	currentByAgent := make(map[string]storage.TelemetryRuntimeCurrentRecord, len(currents))
	for _, current := range currents {
		currentByAgent[current.AgentID] = current
	}

	for _, agent := range s.live.List() {
		current, ok := currentByAgent[agent.ID]
		if !ok {
			// No persisted runtime row — same as the ErrNotFound branch
			// in the historic per-agent path: skip this agent.
			continue
		}
		// Merge the restored runtime into the agent value and re-commit
		// through the live store, preserving the agent's instances
		// (restored separately in restoreInstances).
		merged := restoreAgentRuntimeFromStorage(agent, current)
		s.live.ApplySnapshot(agent.ID, merged, s.live.InstancesForAgent(agent.ID))
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

func telemetryBoostStateForAgent(expiresAt, now time.Time) telemetryDetailBoostResponse {
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

func telemetryInitializationWatchForAgent(agent Agent, now, cooldownExpiresAt time.Time) telemetryInitializationWatchResponse {
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

// telemetrySeverityAndReason classifies an agent for the dashboard. The
// fallback entered-at timestamp must be provided by the caller (zero =
// not in fallback) so this function takes no locks of its own — every
// caller already holds s.mu.RLock for the duration of the snapshot pass
// and sync.RWMutex is not reentrant (a queued writer would deadlock the
// second RLock).
func (s *Server) telemetrySeverityAndReason(agent Agent, presenceState presence.State, freshness telemetryFreshnessResponse, fallbackEnteredAt time.Time, now time.Time) (string, string) {
	in := controltelemetry.SeverityInput{
		PresenceState:           presenceState,
		ReadOnly:                agent.ReadOnly,
		AcceptingNewConnections: agent.Runtime.AcceptingNewConnections,
		Degraded:                agent.Runtime.Degraded,
		StartupStatus:           agent.Runtime.StartupStatus,
		DCCoveragePct:           agent.Runtime.DCCoveragePct,
		HealthyUpstreams:        agent.Runtime.HealthyUpstreams,
		TotalUpstreams:          agent.Runtime.TotalUpstreams,
		AgentReported:           !agent.Runtime.UpdatedAt.IsZero(),

		UseMiddleProxy:             agent.Runtime.UseMiddleProxy,
		MERuntimeReady:             agent.Runtime.MERuntimeReady,
		ME2DCFallbackEnabled:       agent.Runtime.ME2DCFallbackEnabled,
		UptimeSeconds:              agent.Runtime.UptimeSeconds,
		TelemtUnreachable:          agent.Runtime.TelemtUnreachable,
		TelemtUnreachableSinceUnix: agent.Runtime.TelemtUnreachableSinceUnix,
	}
	// Pull the (rate, known) pair through the helper so a future caller
	// who forgets to set one of the parallel fields gets nil-is-unknown
	// semantics for free instead of a half-initialised classification.
	if rate := agent.Runtime.FailRatePct5mPtr(); rate != nil {
		in.UpstreamFailRatePct5m = *rate
		in.UpstreamFailRateKnown = true
	}
	if !fallbackEnteredAt.IsZero() {
		in.FallbackActiveDuration = now.Sub(fallbackEnteredAt)
	}
	return controltelemetry.SeverityAndReason(in, controltelemetry.Freshness{
		State:          freshness.State,
		ObservedAtUnix: freshness.ObservedAtUnix,
	})
}

// telemetrySummaryForAgent builds the per-agent dashboard summary. The
// caller holds s.mu.RLock for the detailBoosts lookup; the fallback
// timestamp comes from the fallback tracker, which owns its own lock
// (s.mu -> tracker ordering), so this read is independently safe.
func (s *Server) telemetrySummaryForAgent(agent Agent, presenceState presence.State, now, boostExpiresAt time.Time) telemetryServerSummary {
	agent.PresenceState = string(presenceState)
	agent.Runtime = normalizeAgentRuntime(agent.Runtime)
	fallbackEnteredAt, fallbackActive := s.fallback.Get(agent.ID)
	if fallbackActive {
		// `agent` is passed by value (a deep copy from s.live): this
		// assignment mutates the local copy that gets embedded in the
		// returned telemetryServerSummary, not the live mirror. The *int64
		// pointer is local to this scope (no aliasing back into shared
		// state), so the write is safe and intentional — don't "fix" it by
		// taking &agent or reaching into the live store. See follow-up #6.
		entered := fallbackEnteredAt.Unix()
		agent.Runtime.FallbackEnteredAtUnix = &entered
	}
	freshness := telemetryFreshnessForRuntime(agent.Runtime, now)
	var fallbackForSeverity time.Time
	if fallbackActive {
		fallbackForSeverity = fallbackEnteredAt
	}
	severity, reason := s.telemetrySeverityAndReason(agent, presenceState, freshness, fallbackForSeverity, now)
	return telemetryServerSummary{
		Agent:            agent,
		Severity:         severity,
		Reason:           reason,
		RuntimeFreshness: freshness,
		DetailBoost:      telemetryBoostStateForAgent(boostExpiresAt, now),
	}
}

func sortTelemetrySummaries(items []telemetryServerSummary) {
	sort.Slice(items, func(left, right int) bool {
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
