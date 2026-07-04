package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/agent/telemtrestart"
	"github.com/lost-coder/panvex/internal/agent/updater"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// jobActionRotateSecret is the canonical job-action token used to flag
// a rotate-secret rollout; centralised so the dispatch and audit paths
// stay in sync (Sonar S1192).
const jobActionRotateSecret = "client.rotate_secret"

// jobActionResetQuota is the canonical job-action token for the
// client.reset_quota rollout. The action is structurally distinct from
// the other client.* actions because it doesn't carry a ManagedClient
// payload (only the username) and its Telemt endpoint may be absent on
// older Telemt builds — see handleClientResetQuotaJob.
const jobActionResetQuota = "client.reset_quota"

// configRestarter restarts the local Telemt process. Implemented by
// *telemtrestart.Restarter; nil when no (valid) strategy is configured, in which
// case restart-required config changes are refused.
type configRestarter interface {
	Restart(ctx context.Context) error
}

type telemtClient interface {
	FetchRuntimeState(context.Context) (telemt.RuntimeState, error)
	FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error)
	FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error)
	FetchSystemInfo(context.Context) (telemt.SystemInfo, error)
	FetchDiscoveredUsers(ctx context.Context, configPath string) ([]telemt.DiscoveredUser, error)
	ExecuteRuntimeReload(context.Context) error
	CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error)
	UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error)
	DeleteClient(context.Context, string) error
	ResetUserQuota(context.Context, string) (telemt.ResetUserQuotaResult, error)
	InvalidateSlowDataCache()
	PatchConfig(ctx context.Context, patch map[string]any, expectedRevision string) (telemt.PatchConfigResult, error)
	GetManagedConfig(ctx context.Context) (map[string]any, string, error)
	HealthReady(ctx context.Context) (bool, string, error)
}

// Config describes the control-plane identity reported by the agent.
type Config struct {
	AgentID          string
	NodeName         string
	FleetGroupID     string
	Version          string
	TelemtConfigPath string
	// TelemtRestart is the restart strategy for the local Telemt process,
	// e.g. "systemd:telemt.service", "docker:telemt", or "command:<argv>".
	// Empty means restart-required config changes are refused on this node.
	TelemtRestart string
	// InitialUsageSeq is the last snapshot sequence number persisted from a
	// previous agent incarnation. On fresh agents it is zero. See P2-LOG-06.
	InitialUsageSeq uint64
	// PersistUsageSeq is called (if non-nil) after every BuildUsageSnapshot
	// so the next run can resume from the last emitted sequence. Errors are
	// logged but do not fail snapshot emission.
	PersistUsageSeq func(seq uint64) error
	// UpdateTransport, if non-nil, is called when a switch_transport_mode job
	// is processed. It is responsible for persisting the new transport state and
	// signalling the outer reconnect loop to pick up the change on next iteration.
	// Making it optional ensures existing tests constructing Config{} continue to
	// compile without change.
	UpdateTransport func(mode, listenAddr, panelURL string) error
	// ScheduleSelfRestart, if non-nil, is invoked after a successful
	// agent.self-update binary swap, AFTER the handler has produced the
	// JobResult. The implementation must delay the actual process restart
	// long enough for the worker to flush the result onto the gRPC stream
	// (see cmd/agent/runtime.go) — restarting synchronously re-creates the
	// A3 infinite update loop. nil (tests) means no restart is scheduled.
	ScheduleSelfRestart func()
}

type runtimeLifecycleState struct {
	startupStatus        string
	startupStage         string
	startupProgressPct   float64
	initializationStatus string
	initializationStage  string
	initializationPct    float64
}

const defaultCompletedJobRetention = 2 * time.Hour
const runtimeInitializationCooldown = 90 * time.Second

type completedJobRecord struct {
	CompletedAt time.Time
	Success     bool
	Message     string
	ResultJSON  string
}

// Agent builds snapshots and executes control-plane commands against local Telemt.
type Agent struct {
	config    Config
	telemt    telemtClient
	restarter configRestarter
	mu        sync.RWMutex

	observedConfig  observedConfigReporter
	diagnosticsGate contentHashGate
	securityGate    contentHashGate

	clientNames                        map[string]string
	lastOctets                         map[string]uint64
	lastConnections                    map[string]int
	lastMetricsUptime                  float64
	lastLifecycle                      runtimeLifecycleState
	runtimeInitializationActive        bool
	runtimeInitializationCooldownUntil time.Time
	completedJobs                      map[string]completedJobRecord
	completedJobRetention              time.Duration
	ipCollector                        *telemt.IPCollector
	// usageSeq is the last monotonic client-usage sequence number emitted
	// by this agent process. Loaded from persisted state on boot (InitialUsageSeq)
	// and incremented for every BuildUsageSnapshot. The control-plane dedups
	// duplicate seqs and resets its baseline when seq rewinds (agent restart
	// on a fresh state file). See P2-LOG-06 / L-07.
	usageSeq        uint64
	persistUsageSeq func(seq uint64) error
	// usageBaselinePrimed guards against double-counting all historical
	// traffic on an agent-process restart. lastOctets (the per-client
	// delta baseline) lives only in memory, so after a restart it is
	// empty while telemt's counters carry the full cumulative total. Since
	// usageSeq is persisted and resumes (it does NOT rewind to 1 on a
	// restart with an intact state file), the control-plane would accept
	// the first post-restart "delta" — really the entire cumulative total —
	// and add it on top of what it already had. On the first
	// BuildUsageSnapshot of the process we therefore treat current totals
	// as the baseline and emit delta=0, then count normally from there.
	usageBaselinePrimed bool
}

// New constructs a runtime agent bound to one local Telemt client.
func New(config Config, client telemtClient) *Agent {
	a := &Agent{
		config:                config,
		telemt:                client,
		clientNames:           make(map[string]string),
		lastOctets:            make(map[string]uint64),
		lastConnections:       make(map[string]int),
		completedJobs:         make(map[string]completedJobRecord),
		completedJobRetention: defaultCompletedJobRetention,
		ipCollector:           telemt.NewIPCollector(),
		usageSeq:              config.InitialUsageSeq,
		persistUsageSeq:       config.PersistUsageSeq,
	}
	a.restarter = buildRestarter(config.TelemtRestart)
	return a
}

// buildRestarter parses the restart strategy. Returns nil (an untyped nil
// interface, so `restarter == nil` holds) when unset or invalid; an invalid
// strategy is logged so the operator can fix it.
func buildRestarter(spec string) configRestarter {
	if strings.TrimSpace(spec) == "" {
		return nil
	}
	r, err := telemtrestart.Parse(spec, telemtrestart.ExecRunner{})
	if err != nil {
		slog.Warn("invalid telemt restart strategy; restart-required config changes will be refused",
			"strategy", spec, "error", err)
		return nil
	}
	return r
}

// AgentID returns the persistent control-plane identity of the agent.
func (a *Agent) AgentID() string {
	return a.config.AgentID
}

// NodeName returns the node name reported by the agent.
func (a *Agent) NodeName() string {
	return a.config.NodeName
}

// Version returns the current agent version string.
func (a *Agent) Version() string {
	return a.config.Version
}

// FleetGroupID returns the fleet group reported by the agent.
func (a *Agent) FleetGroupID() string {
	return a.config.FleetGroupID
}

func lifecycleStateForRuntime(state telemt.RuntimeState) runtimeLifecycleState {
	return runtimeLifecycleState{
		startupStatus:        state.Gates.StartupStatus,
		startupStage:         state.Gates.StartupStage,
		startupProgressPct:   state.Gates.StartupProgressPct,
		initializationStatus: state.Initialization.Status,
		initializationStage:  state.Initialization.CurrentStage,
		initializationPct:    state.Initialization.ProgressPct,
	}
}

func startupLifecycleRegressed(previous, current runtimeLifecycleState) bool {
	if previous.startupProgressPct > 0 && current.startupProgressPct < previous.startupProgressPct {
		return true
	}
	if previous.initializationPct > 0 && current.initializationPct < previous.initializationPct {
		return true
	}
	if previous.startupStage != "" && current.startupStage != "" && previous.startupStage != current.startupStage && current.startupStatus != "ready" {
		return true
	}
	if previous.initializationStage != "" && current.initializationStage != "" && previous.initializationStage != current.initializationStage && current.initializationStatus != "ready" {
		return true
	}
	return false
}

func runtimeNeedsInitializationWatch(state telemt.RuntimeState) bool {
	switch {
	case state.Gates.StartupStatus != "" && state.Gates.StartupStatus != "ready":
		return true
	case state.Initialization.Status != "" && state.Initialization.Status != "ready":
		return true
	case !state.Gates.AcceptingNewConnections || !state.Gates.MERuntimeReady:
		return true
	default:
		return false
	}
}

// BuildRuntimeSnapshot converts the current Telemt runtime state into a gateway snapshot.
func connectionTopEntries(entries []telemt.RuntimeConnectionTopEntry) []*gatewayrpc.ConnectionTopEntry {
	if len(entries) == 0 {
		return nil
	}
	result := make([]*gatewayrpc.ConnectionTopEntry, 0, len(entries))
	for _, e := range entries {
		result = append(result, &gatewayrpc.ConnectionTopEntry{
			Username:        e.Username,
			Connections:     int32(e.Connections),
			ThroughputBytes: e.ThroughputBytes,
		})
	}
	return result
}

// connectionClassCounts marshals telemt class-count rows into the proto
// wire shape. Empty class slices come back as nil so the proto stays at
// its zero-value default — the panel treats absence and empty slice the
// same.
func connectionClassCounts(rows []telemt.ConnectionClassStat) []*gatewayrpc.ConnectionsClassCount {
	if len(rows) == 0 {
		return nil
	}
	result := make([]*gatewayrpc.ConnectionsClassCount, 0, len(rows))
	for _, r := range rows {
		result = append(result, &gatewayrpc.ConnectionsClassCount{
			Class: r.Class,
			Total: r.Total,
		})
	}
	return result
}

// runtimeSnapshotTelemtDeadline caps how long one snapshot cycle may
// spend in telemt calls (B6). telemt.Client.FetchRuntimeState has its
// own 30s ceiling, but from the snapshot loop's perspective 30s is too
// long — operator dashboards stop updating for a full half-minute when
// a single endpoint is slow. 10s is still comfortably above a normal
// health/posture/summary round-trip and leaves Partial=true semantics
// intact for the slow-endpoint case.
const runtimeSnapshotTelemtDeadline = 10 * time.Second

func (a *Agent) BuildRuntimeSnapshot(ctx context.Context, observedAt time.Time) (*gatewayrpc.Snapshot, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, runtimeSnapshotTelemtDeadline)
	defer cancel()

	state, err := a.telemt.FetchRuntimeState(fetchCtx)
	if err != nil {
		return nil, err
	}
	// P2-REL-07: telemt.Client.FetchRuntimeState returns Partial=true when at
	// least one sub-endpoint failed or the per-cycle deadline fired. Still send
	// the snapshot to the control-plane so operator dashboards do not go dark,
	// but log a warning so the degradation is visible in the agent journal.
	if state.Partial {
		slog.Default().Warn("telemt runtime snapshot is partial",
			"agent_id", a.config.AgentID,
			"node_name", a.config.NodeName,
			"ctx_err", ctx.Err(),
		)
	}

	dcs := convertRuntimeDCs(state.DCs)
	upstreamRows := convertUpstreamRows(state.Upstreams.Rows)
	recentEvents := convertRecentEvents(state.RecentEvents)

	wasRestarting := a.updateLifecycleState(state, observedAt)

	snapshot := a.baseSnapshot(observedAt)
	// IN-H6: tell the panel this snapshot is partial so it preserves
	// last-known version/connections/read_only/uptime instead of
	// overwriting them with the (possibly zeroed) partial values.
	snapshot.Partial = state.Partial
	snapshot.ReadOnly = state.ReadOnly
	// Report the agent's observed managed Telemt config, delta-gated: the
	// canonical hash every snapshot, the full canonical JSON only when the
	// hash changed. On any error (incl. telemt.ErrConfigEditUnsupported on
	// older Telemt builds) both stay empty.
	var managedConfigHash, managedConfigJSON string
	if sections, _, err := a.telemt.GetManagedConfig(fetchCtx); err == nil {
		managedConfigHash, managedConfigJSON = a.observedConfig.next(sections)
	}
	snapshot.Instances = []*gatewayrpc.InstanceSnapshot{
		{
			Id:                "telemt-primary",
			Name:              "telemt-primary",
			Version:           state.Version,
			ConfigFingerprint: "runtime",
			Connections:       int32(state.Connections),
			ReadOnly:          state.ReadOnly,
			ManagedConfigHash: managedConfigHash,
			ManagedConfigJson: managedConfigJSON,
		},
	}
	snapshot.Metrics = map[string]uint64{
		"connections": uint64(state.Connections),
	}
	snapshot.Runtime = buildRuntimeSnapshotProto(state, dcs, upstreamRows, recentEvents, wasRestarting)
	diagHash, sendDiagnosticsBody := a.diagnosticsGate.next(
		state.Diagnostics.State,
		state.Diagnostics.StateReason,
		state.Diagnostics.SystemInfoJSON,
		state.Diagnostics.EffectiveLimitsJSON,
		state.Diagnostics.SecurityPostureJSON,
		state.Diagnostics.MinimalAllJSON,
		state.Diagnostics.MEPoolJSON,
		state.Diagnostics.DcsJSON,
	)
	snapshot.RuntimeDiagnostics = &gatewayrpc.RuntimeDiagnosticsSnapshot{ContentHash: diagHash}
	if sendDiagnosticsBody {
		snapshot.RuntimeDiagnostics = &gatewayrpc.RuntimeDiagnosticsSnapshot{
			ContentHash:         diagHash,
			State:               state.Diagnostics.State,
			StateReason:         state.Diagnostics.StateReason,
			SystemInfoJson:      state.Diagnostics.SystemInfoJSON,
			EffectiveLimitsJson: state.Diagnostics.EffectiveLimitsJSON,
			SecurityPostureJson: state.Diagnostics.SecurityPostureJSON,
			MinimalAllJson:      state.Diagnostics.MinimalAllJSON,
			MePoolJson:          state.Diagnostics.MEPoolJSON,
			DcsJson:             state.Diagnostics.DcsJSON,
		}
	}
	securityHash, sendSecurityBody := a.securityGate.next(
		state.SecurityInventory.State,
		state.SecurityInventory.StateReason,
		strconv.FormatBool(state.SecurityInventory.Enabled),
		strconv.Itoa(state.SecurityInventory.EntriesTotal),
		state.SecurityInventory.EntriesJSON,
	)
	snapshot.RuntimeSecurityInventory = &gatewayrpc.RuntimeSecurityInventorySnapshot{ContentHash: securityHash}
	if sendSecurityBody {
		snapshot.RuntimeSecurityInventory = &gatewayrpc.RuntimeSecurityInventorySnapshot{
			ContentHash:  securityHash,
			State:        state.SecurityInventory.State,
			StateReason:  state.SecurityInventory.StateReason,
			Enabled:      state.SecurityInventory.Enabled,
			EntriesTotal: int32(state.SecurityInventory.EntriesTotal),
			EntriesJson:  state.SecurityInventory.EntriesJSON,
		}
	}
	snapshot.TotalActiveConnections = int32(state.ConnectionTotals.CurrentConnections)
	snapshot.TotalActiveUsers = int32(state.ConnectionTotals.ActiveUsers)

	return snapshot, nil
}

// BuildRuntimeUnreachableSnapshot emits a degraded snapshot used while the
// local Telemt API has been confirmed unreachable for at least
// telemtUnreachableThreshold (see cmd/agent/polling.go). The Runtime payload
// carries the reachability flag plus a "since" timestamp; all other runtime
// fields are intentionally zero — the panel zeroes/stubs the corresponding
// UI views to avoid showing stale "last known" data while we have nothing
// fresh to report.
func (a *Agent) BuildRuntimeUnreachableSnapshot(observedAt, since time.Time) *gatewayrpc.Snapshot {
	snapshot := a.baseSnapshot(observedAt)
	snapshot.Runtime = &gatewayrpc.RuntimeSnapshot{
		TelemtUnreachable:          true,
		TelemtUnreachableSinceUnix: since.Unix(),
	}
	snapshot.RuntimeDiagnostics = &gatewayrpc.RuntimeDiagnosticsSnapshot{}
	snapshot.RuntimeSecurityInventory = &gatewayrpc.RuntimeSecurityInventorySnapshot{}
	// The panel deliberately blanks its stored diagnostics row on an
	// unreachable snapshot (empty hash = historical overwrite semantics).
	// Reset the gates so the first post-recovery snapshot re-sends full
	// bodies — otherwise an unchanged hash would leave the panel blank
	// forever (D5).
	a.diagnosticsGate.reset()
	a.securityGate.reset()
	return snapshot
}

// convertRuntimeDCs converts internal DC entries to gateway protobuf snapshots.
func convertRuntimeDCs(dcsIn []telemt.RuntimeDC) []*gatewayrpc.RuntimeDCSnapshot {
	dcs := make([]*gatewayrpc.RuntimeDCSnapshot, 0, len(dcsIn))
	for _, dc := range dcsIn {
		dcs = append(dcs, &gatewayrpc.RuntimeDCSnapshot{
			Dc:                 int32(dc.DC),
			AvailableEndpoints: int32(dc.AvailableEndpoints),
			AvailablePct:       dc.AvailablePct,
			RequiredWriters:    int32(dc.RequiredWriters),
			AliveWriters:       int32(dc.AliveWriters),
			CoveragePct:        dc.CoveragePct,
			RttMs:              dc.RTTMs,
			Load:               int32(dc.Load),
			FreshAliveWriters:  int32(dc.FreshAliveWriters),
			FreshCoveragePct:   dc.FreshCoveragePct,
		})
	}
	return dcs
}

// convertUpstreamRows converts upstream rows into gateway protobuf snapshots.
func convertUpstreamRows(rows []telemt.RuntimeUpstream) []*gatewayrpc.RuntimeUpstreamRowSnapshot {
	upstreamRows := make([]*gatewayrpc.RuntimeUpstreamRowSnapshot, 0, len(rows))
	for _, upstream := range rows {
		upstreamRows = append(upstreamRows, &gatewayrpc.RuntimeUpstreamRowSnapshot{
			UpstreamId:         int32(upstream.UpstreamID),
			RouteKind:          upstream.RouteKind,
			Address:            upstream.Address,
			Healthy:            upstream.Healthy,
			Fails:              int32(upstream.Fails),
			EffectiveLatencyMs: upstream.EffectiveLatencyMs,
			Weight:             int32(upstream.Weight),
			LastCheckAgeSecs:   int32(upstream.LastCheckAgeSecs),
			Scopes:             upstream.Scopes,
		})
	}
	return upstreamRows
}

// convertRecentEvents converts runtime events to gateway protobuf snapshots.
func convertRecentEvents(events []telemt.RuntimeEvent) []*gatewayrpc.RuntimeEventSnapshot {
	recentEvents := make([]*gatewayrpc.RuntimeEventSnapshot, 0, len(events))
	for _, event := range events {
		recentEvents = append(recentEvents, &gatewayrpc.RuntimeEventSnapshot{
			Sequence:      event.Sequence,
			TimestampUnix: event.TimestampUnix,
			EventType:     event.EventType,
			Context:       event.Context,
		})
	}
	return recentEvents
}

// updateLifecycleState stores lifecycle/initialization changes under the
// agent lock and returns whether the runtime appears to have restarted.
func (a *Agent) updateLifecycleState(state telemt.RuntimeState, observedAt time.Time) bool {
	lifecycle := lifecycleStateForRuntime(state)
	wasRestarting := false

	a.mu.Lock()
	defer a.mu.Unlock()

	if startupLifecycleRegressed(a.lastLifecycle, lifecycle) {
		wasRestarting = true
	}
	previousInitializationActive := a.runtimeInitializationActive
	currentInitializationActive := runtimeNeedsInitializationWatch(state)
	if currentInitializationActive {
		a.runtimeInitializationActive = true
		a.runtimeInitializationCooldownUntil = time.Time{}
	} else {
		a.runtimeInitializationActive = false
		if previousInitializationActive {
			a.runtimeInitializationCooldownUntil = observedAt.UTC().Add(runtimeInitializationCooldown)
		}
	}
	a.lastLifecycle = lifecycle
	return wasRestarting
}

// buildRuntimeSnapshotProto assembles the runtime portion of a gateway snapshot.
func buildRuntimeSnapshotProto(
	state telemt.RuntimeState,
	dcs []*gatewayrpc.RuntimeDCSnapshot,
	upstreamRows []*gatewayrpc.RuntimeUpstreamRowSnapshot,
	recentEvents []*gatewayrpc.RuntimeEventSnapshot,
	wasRestarting bool,
) *gatewayrpc.RuntimeSnapshot {
	return &gatewayrpc.RuntimeSnapshot{
		// TelemtUnreachable is left at its proto3 default (false) on every
		// snapshot the agent successfully builds from a real telemt.RuntimeState
		// — by definition we just talked to Telemt to obtain this state. The
		// unreachable signal (true + TelemtUnreachableSinceUnix) is emitted by
		// the separate BuildRuntimeUnreachableSnapshot path in cmd/agent/polling.go.
		AcceptingNewConnections:   state.Gates.AcceptingNewConnections,
		MeRuntimeReady:            state.Gates.MERuntimeReady,
		Me2DcFallbackEnabled:      state.Gates.ME2DCFallbackEnabled,
		Me2DcFastEnabled:          state.Gates.ME2DCFastEnabled,
		UseMiddleProxy:            state.Gates.UseMiddleProxy,
		RerouteActive:             state.Gates.RerouteActive,
		RouteMode:                 state.Gates.RouteMode,
		StartupStatus:             state.Gates.StartupStatus,
		StartupStage:              state.Gates.StartupStage,
		StartupProgressPct:        state.Gates.StartupProgressPct,
		InitializationStatus:      state.Initialization.Status,
		Degraded:                  state.Initialization.Degraded || wasRestarting,
		InitializationStage:       state.Initialization.CurrentStage,
		InitializationProgressPct: state.Initialization.ProgressPct,
		TransportMode:             state.Initialization.TransportMode,
		CurrentConnections:        int32(state.ConnectionTotals.CurrentConnections),
		CurrentConnectionsMe:      int32(state.ConnectionTotals.CurrentConnectionsME),
		CurrentConnectionsDirect:  int32(state.ConnectionTotals.CurrentConnectionsDirect),
		ActiveUsers:               int32(state.ConnectionTotals.ActiveUsers),
		StaleCacheUsed:            state.ConnectionTotals.StaleCacheUsed,
		TopByConnections:          connectionTopEntries(state.ConnectionTotals.TopByConnections),
		TopByThroughput:           connectionTopEntries(state.ConnectionTotals.TopByThroughput),
		UptimeSeconds:             state.UptimeSeconds,
		ConnectionsTotal:          state.Summary.ConnectionsTotal,
		ConnectionsBadTotal:       state.Summary.ConnectionsBadTotal,
		HandshakeTimeoutsTotal:    state.Summary.HandshakeTimeoutsTotal,
		ConfiguredUsers:           int32(state.Summary.ConfiguredUsers),
		ConnectionsBadByClass:     connectionClassCounts(state.Summary.ConnectionsBadByClass),
		HandshakeFailuresByClass:  connectionClassCounts(state.Summary.HandshakeFailuresByClass),
		Dcs:                       dcs,
		Upstreams: &gatewayrpc.RuntimeUpstreamSnapshot{
			ConfiguredTotal:      int32(state.Upstreams.ConfiguredTotal),
			HealthyTotal:         int32(state.Upstreams.HealthyTotal),
			UnhealthyTotal:       int32(state.Upstreams.UnhealthyTotal),
			DirectTotal:          int32(state.Upstreams.DirectTotal),
			Socks4Total:          int32(state.Upstreams.SOCKS4Total),
			Socks5Total:          int32(state.Upstreams.SOCKS5Total),
			ShadowsocksTotal:     int32(state.Upstreams.ShadowsocksTotal),
			Rows:                 upstreamRows,
			FailRatePct_5M:       state.Upstreams.FailRatePct5m,
			FailRateKnown:        state.Upstreams.FailRateKnown,
			ConnectAttemptTotal:  state.Upstreams.ConnectAttemptTotal,
			ConnectSuccessTotal:  state.Upstreams.ConnectSuccessTotal,
			ConnectFailTotal:     state.Upstreams.ConnectFailTotal,
			ConnectFailfastTotal: state.Upstreams.ConnectFailfastTotal,
		},
		RecentEvents: recentEvents,
		SystemLoad: &gatewayrpc.RuntimeSystemLoadSnapshot{
			CpuUsagePct:      state.SystemLoad.CPUUsagePct,
			MemoryUsedBytes:  state.SystemLoad.MemoryUsedBytes,
			MemoryTotalBytes: state.SystemLoad.MemoryTotalBytes,
			MemoryUsagePct:   state.SystemLoad.MemoryUsagePct,
			DiskUsedBytes:    state.SystemLoad.DiskUsedBytes,
			DiskTotalBytes:   state.SystemLoad.DiskTotalBytes,
			DiskUsagePct:     state.SystemLoad.DiskUsagePct,
			Load_1M:          state.SystemLoad.Load1M,
			Load_5M:          state.SystemLoad.Load5M,
			Load_15M:         state.SystemLoad.Load15M,
			NetBytesSent:     state.SystemLoad.NetBytesSent,
			NetBytesRecv:     state.SystemLoad.NetBytesRecv,
		},
		MeWritersSummary: &gatewayrpc.RuntimeMeWritersSummary{
			ConfiguredEndpoints: int32(state.MeWritersSummary.ConfiguredEndpoints),
			AvailableEndpoints:  int32(state.MeWritersSummary.AvailableEndpoints),
			CoveragePct:         state.MeWritersSummary.CoveragePct,
			FreshAliveWriters:   int32(state.MeWritersSummary.FreshAliveWriters),
			FreshCoveragePct:    state.MeWritersSummary.FreshCoveragePct,
			RequiredWriters:     int32(state.MeWritersSummary.RequiredWriters),
			AliveWriters:        int32(state.MeWritersSummary.AliveWriters),
		},
	}
}

// RuntimeSnapshotInterval reports the desired runtime polling cadence for the current lifecycle state.
func (a *Agent) RuntimeSnapshotInterval(baseInterval time.Duration, fastInterval time.Duration, now time.Time) time.Duration {
	if baseInterval <= 0 {
		return baseInterval
	}
	if fastInterval <= 0 {
		return baseInterval
	}

	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.runtimeInitializationActive {
		return fastInterval
	}
	if a.runtimeInitializationCooldownUntil.After(now.UTC()) {
		return fastInterval
	}

	return baseInterval
}

// BuildUsageSnapshot fetches per-client counters from Telemt metrics and emits only changed deltas.
func (a *Agent) BuildUsageSnapshot(ctx context.Context, observedAt time.Time) (*gatewayrpc.Snapshot, error) {
	metricsSnapshot, err := a.telemt.FetchClientUsageFromMetrics(ctx)
	if err != nil {
		return nil, err
	}
	usageRows := metricsSnapshot.Users

	a.mu.Lock()

	restarted := metricsSnapshot.UptimeSeconds > 0 && metricsSnapshot.UptimeSeconds < a.lastMetricsUptime
	// First tick after process start: lastOctets is empty but telemt may
	// already hold large cumulative counters. Prime baselines and emit
	// delta=0 so we never replay the full cumulative total as a single
	// "delta" (which the control-plane would accumulate → double-count).
	baselineTick := !a.usageBaselinePrimed
	a.usageBaselinePrimed = true
	clients := make([]*gatewayrpc.ClientUsageSnapshot, 0, len(usageRows))
	seen := make(map[string]struct{}, len(usageRows))
	for _, client := range usageRows {
		if snap, ok := a.processUsageRowLocked(client, restarted, baselineTick, seen); ok {
			clients = append(clients, snap)
		}
	}

	a.cleanupStaleUsageStateLocked(seen)
	if metricsSnapshot.UptimeSeconds > 0 {
		a.lastMetricsUptime = metricsSnapshot.UptimeSeconds
	}

	// Advance the monotonic per-agent snapshot sequence. The sequence is
	// stamped on every ClientUsageSnapshot (even on empty deltas) so the
	// control-plane can dedup retries/replays and detect agent restarts
	// (seq rewinds to 1). P2-LOG-06 / L-07.
	a.usageSeq++
	nextSeq := a.usageSeq
	for _, client := range clients {
		client.Seq = nextSeq
	}
	persist := a.persistUsageSeq
	a.mu.Unlock()

	snapshot := a.baseSnapshot(observedAt)
	snapshot.Clients = clients
	snapshot.HasClientUsage = true

	// Persist OUTSIDE a.mu (audit #7): SaveUsageSeq fsync+renames the
	// whole state bundle; doing that under the runtime lock stalled every
	// concurrent snapshot/job for the duration of the disk write.
	if persist != nil {
		if err := persist(nextSeq); err != nil {
			slog.Warn("persist usage seq failed", "seq", nextSeq, "error", err)
		}
	}
	return snapshot, nil
}

// processUsageRowLocked computes the per-client delta, updates last-known
// trackers, and returns the gateway snapshot when the row contributes a
// non-empty change. Caller must hold a.mu.
func (a *Agent) processUsageRowLocked(client telemt.ClientUsage, restarted, baselineTick bool, seen map[string]struct{}) (*gatewayrpc.ClientUsageSnapshot, bool) {
	clientID := client.ClientID
	if clientID == "" && client.ClientName != "" {
		clientID = a.clientIDForNameLocked(client.ClientName)
	}

	// Use clientID as tracking key when available, fall back to name.
	trackingKey := clientID
	if trackingKey == "" {
		if client.ClientName == "" {
			return nil, false
		}
		trackingKey = "name:" + client.ClientName
	}

	currentTotal := client.TrafficUsedBytes
	previousTotal := a.lastOctets[trackingKey]
	delta := currentTotal
	switch {
	case baselineTick:
		// First tick of the process: adopt current totals as the baseline
		// without emitting them as traffic. Counting resumes on the next
		// tick from this baseline.
		delta = 0
	case !restarted && currentTotal >= previousTotal:
		delta = currentTotal - previousTotal
	}
	connectionsChanged := a.lastConnections[trackingKey] != client.ActiveTCPConns

	a.lastOctets[trackingKey] = currentTotal
	a.lastConnections[trackingKey] = client.ActiveTCPConns
	seen[trackingKey] = struct{}{}

	if delta == 0 && !connectionsChanged && client.CurrentIPsUsed == 0 {
		return nil, false
	}
	return &gatewayrpc.ClientUsageSnapshot{
		ClientId:          clientID,
		ClientName:        client.ClientName,
		TrafficDeltaBytes: delta,
		// IN-L1: clamp to int32 so a malformed/huge telemt gauge cannot wrap
		// to a negative count on the wire.
		UniqueIpsUsed:      clampInt32(client.UniqueIPsUsed),
		ActiveTcpConns:     clampInt32(client.ActiveTCPConns),
		ActiveUniqueIps:    clampInt32(client.CurrentIPsUsed),
		QuotaUsedBytes:     client.QuotaUsedBytes,
		QuotaLastResetUnix: client.QuotaLastResetUnix,
	}, true
}

// clampInt32 bounds a non-negative count into the int32 wire range so an
// out-of-range telemt gauge cannot wrap to a negative value (IN-L1).
func clampInt32(v int) int32 {
	if v < 0 {
		return 0
	}
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(v)
}

// cleanupStaleUsageStateLocked drops tracker entries for clients absent
// from the latest sample. Caller must hold a.mu.
func (a *Agent) cleanupStaleUsageStateLocked(seen map[string]struct{}) {
	for clientID := range a.lastConnections {
		if _, ok := seen[clientID]; ok {
			continue
		}
		delete(a.lastConnections, clientID)
		delete(a.lastOctets, clientID)
	}
}

// PollActiveIPs updates the local accumulated IP set from the lightweight Telemt endpoint.
func (a *Agent) PollActiveIPs(ctx context.Context) error {
	users, err := a.telemt.FetchActiveIPs(ctx)
	if err != nil {
		return err
	}
	a.ipCollector.Update(users)
	return nil
}

// BuildIPSnapshot flushes accumulated IP sets into a protobuf snapshot.
func (a *Agent) BuildIPSnapshot(observedAt time.Time) *gatewayrpc.Snapshot {
	users := a.ipCollector.Flush()
	clientIPs := make([]*gatewayrpc.ClientIPSnapshot, 0, len(users))
	for _, user := range users {
		clientID := a.clientIDForName(user.Username)
		if clientID == "" && user.Username == "" {
			continue
		}
		clientIPs = append(clientIPs, &gatewayrpc.ClientIPSnapshot{
			ClientId:   clientID,
			ClientName: user.Username,
			ActiveIps:  append([]string(nil), user.ActiveIPs...),
		})
	}
	snapshot := a.baseSnapshot(observedAt)
	snapshot.ClientIps = clientIPs
	snapshot.HasClientIps = true
	return snapshot
}

// RestoreIPSnapshot re-merges a previously-built IP snapshot back into the
// collector. BuildIPSnapshot flushes (clears) the collector, so when the
// outbound send subsequently fails (backpressure), the caller must restore
// the flushed IPs here — otherwise the accumulated union is lost permanently
// (telemt only reports currently-active IPs; the union is never re-derived).
func (a *Agent) RestoreIPSnapshot(snap *gatewayrpc.Snapshot) {
	if snap == nil || len(snap.GetClientIps()) == 0 {
		return
	}
	users := make([]telemt.UserActiveIPs, 0, len(snap.GetClientIps()))
	for _, c := range snap.GetClientIps() {
		users = append(users, telemt.UserActiveIPs{
			Username:  c.GetClientName(),
			ActiveIPs: append([]string(nil), c.GetActiveIps()...),
		})
	}
	a.ipCollector.Update(users)
}

func (a *Agent) baseSnapshot(observedAt time.Time) *gatewayrpc.Snapshot {
	return &gatewayrpc.Snapshot{
		AgentId:        a.config.AgentID,
		NodeName:       a.config.NodeName,
		FleetGroupId:   a.config.FleetGroupID,
		Version:        a.config.Version,
		ObservedAtUnix: observedAt.UTC().Unix(),
	}
}

// HandleJob executes a supported job command and returns an execution result envelope.
func (a *Agent) HandleJob(ctx context.Context, job *gatewayrpc.JobCommand, observedAt time.Time) *gatewayrpc.JobResult {
	if cachedResult, ok := a.findCompletedJobResult(job.GetId(), observedAt); ok {
		return cachedResult
	}

	result := &gatewayrpc.JobResult{
		AgentId:        a.config.AgentID,
		JobId:          job.GetId(),
		ObservedAtUnix: observedAt.UTC().Unix(),
	}
	defer a.rememberCompletedJobResult(job.GetId(), result, observedAt)

	switch job.GetAction() {
	case "runtime.reload":
		return a.handleRuntimeReloadJob(ctx, result)
	case "runtime.restart":
		return a.handleRuntimeRestartJob(ctx, result)
	case "telemetry.refresh_diagnostics":
		a.telemt.InvalidateSlowDataCache()
		result.Success = true
		result.Message = "diagnostics refresh requested"
		return result
	case "client.create", "client.update", jobActionRotateSecret, "client.delete":
		return a.handleClientJob(ctx, job, result)
	case jobActionResetQuota:
		return a.handleClientResetQuotaJob(ctx, job, result)
	case "agent.self-update":
		return a.handleSelfUpdateJob(ctx, job, result)
	case "switch_transport_mode":
		return a.handleSwitchTransportModeJob(result, job)
	case "config.apply":
		return a.handleConfigApplyJob(ctx, job, result)
	case "config.fetch":
		return a.handleConfigFetchJob(ctx, result)
	default:
		result.Message = fmt.Sprintf("unsupported action %s", job.GetAction())
		return result
	}
}

// handleRuntimeReloadJob runs a runtime.reload job and returns the populated result.
func (a *Agent) handleRuntimeReloadJob(ctx context.Context, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	if err := a.telemt.ExecuteRuntimeReload(ctx); err != nil {
		result.Message = err.Error()
		return result
	}
	result.Success = true
	result.Message = "runtime reloaded"
	return result
}

// handleRuntimeRestartJob restarts the local Telemt process via the agent's
// configured restart strategy. When no strategy is configured the restarter
// is nil; we report a typed failure so the panel can surface "restart not
// available on this node" instead of silently succeeding.
func (a *Agent) handleRuntimeRestartJob(ctx context.Context, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	if a.restarter == nil {
		result.Message = "restart not available: no restart strategy configured on this agent"
		return result
	}
	if err := a.restarter.Restart(ctx); err != nil {
		result.Message = fmt.Sprintf("telemt restart failed: %v", err)
		return result
	}
	result.Success = true
	result.Message = "telemt restarted"
	return result
}

// handleSwitchTransportModeJob processes a switch_transport_mode job.
// It validates the requested mode, then delegates to Config.UpdateTransport
// (if wired) to persist the change and signal the reconnect loop. If
// UpdateTransport is nil (tests, shutdown in progress) the job succeeds
// immediately so the panel does not re-queue it.
func (a *Agent) handleSwitchTransportModeJob(result *gatewayrpc.JobResult, job *gatewayrpc.JobCommand) *gatewayrpc.JobResult {
	var payload struct {
		Mode       string `json:"mode"`
		ListenAddr string `json:"listen_addr,omitempty"`
		PanelURL   string `json:"panel_url,omitempty"`
	}
	if err := json.Unmarshal([]byte(job.GetPayloadJson()), &payload); err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("switch_transport_mode: payload: %v", err)
		return result
	}
	if payload.Mode != "dial" && payload.Mode != "listen" {
		result.Success = false
		result.Message = fmt.Sprintf("switch_transport_mode: invalid mode %q", payload.Mode)
		return result
	}
	if a.config.UpdateTransport == nil {
		// No callback wired (tests, or agent shutdown in progress) — ack so the
		// panel doesn't re-queue the job; state is already pending on disk if the
		// caller wrote it before constructing this agent.
		result.Success = true
		result.Message = "switch_transport_mode: no transport-update callback wired"
		return result
	}
	if err := a.config.UpdateTransport(payload.Mode, payload.ListenAddr, payload.PanelURL); err != nil {
		result.Success = false
		result.Message = fmt.Sprintf("switch_transport_mode: %v", err)
		return result
	}
	result.Success = true
	return result
}

// clientJobPayload mirrors the JSON envelope shared by all client.* jobs.
type clientJobPayload struct {
	ClientID          string `json:"client_id"`
	PreviousName      string `json:"previous_name"`
	Name              string `json:"name"`
	Secret            string `json:"secret"`
	UserADTag         string `json:"user_ad_tag"`
	Enabled           bool   `json:"enabled"`
	MaxTCPConns       int    `json:"max_tcp_conns"`
	MaxUniqueIPs      int    `json:"max_unique_ips"`
	DataQuotaBytes    int64  `json:"data_quota_bytes"`
	ExpirationRFC3339 string `json:"expiration_rfc3339"`
}

func (p clientJobPayload) toManagedClient() telemt.ManagedClient {
	return telemt.ManagedClient{
		PreviousName:      p.PreviousName,
		Name:              p.Name,
		Secret:            p.Secret,
		UserADTag:         p.UserADTag,
		Enabled:           p.Enabled,
		MaxTCPConns:       p.MaxTCPConns,
		MaxUniqueIPs:      p.MaxUniqueIPs,
		DataQuotaBytes:    p.DataQuotaBytes,
		ExpirationRFC3339: p.ExpirationRFC3339,
	}
}

// handleClientJob dispatches client.create/update/rotate_secret/delete actions.
func (a *Agent) handleClientJob(ctx context.Context, job *gatewayrpc.JobCommand, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	var payload clientJobPayload
	if err := json.Unmarshal([]byte(job.GetPayloadJson()), &payload); err != nil {
		result.Message = fmt.Sprintf("invalid client payload: %v", err)
		return result
	}
	managedClient := payload.toManagedClient()

	switch job.GetAction() {
	case "client.create":
		return a.handleClientCreateJob(ctx, payload, managedClient, result)
	case "client.update", jobActionRotateSecret:
		return a.handleClientUpdateJob(ctx, job, payload, managedClient, result)
	default:
		return a.handleClientDeleteJob(ctx, payload, managedClient, result)
	}
}

func (a *Agent) handleClientCreateJob(ctx context.Context, payload clientJobPayload, managedClient telemt.ManagedClient, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	// A client created in the disabled state must not be deployed to the
	// node: Telemt has no per-user enabled flag, so the only way to keep a
	// disabled client from proxying is to not register it at all. Record
	// the name mapping so a later enable can patch/create it.
	if !managedClient.Enabled {
		a.setClientName(payload.ClientID, managedClient.Name)
		result.Success = true
		result.Message = "client created (disabled; not deployed to node)"
		return result
	}
	applyResult, err := a.telemt.CreateClient(ctx, managedClient)
	if err != nil {
		result.Message = err.Error()
		return result
	}
	result.Success = true
	result.Message = "client created"
	result.ResultJson = marshalClientJobResult(applyResult)
	a.setClientName(payload.ClientID, managedClient.Name)
	return result
}

func (a *Agent) handleClientUpdateJob(ctx context.Context, job *gatewayrpc.JobCommand, payload clientJobPayload, managedClient telemt.ManagedClient, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	// Disabled client → ensure it is absent from Telemt so it stops
	// proxying. Telemt has no enabled flag (the field is silently dropped),
	// so disable maps to a delete. Deleting an already-absent user is a
	// success (idempotent disable); other failures (e.g. Telemt refusing to
	// remove the last configured user) are surfaced to the operator.
	if !managedClient.Enabled {
		err := a.telemt.DeleteClient(ctx, managedClient.Name)
		if err != nil && !errors.Is(err, telemt.ErrClientNotFound) {
			result.Message = err.Error()
			return result
		}
		a.setClientName(payload.ClientID, managedClient.Name)
		result.Success = true
		result.Message = "client disabled (removed from node)"
		return result
	}

	applyResult, err := a.telemt.UpdateClient(ctx, managedClient)
	// Re-enabling a previously-disabled client (or healing config drift)
	// patches a user Telemt no longer has → 404. Fall back to create so the
	// user is restored on the node.
	if errors.Is(err, telemt.ErrClientNotFound) {
		applyResult, err = a.telemt.CreateClient(ctx, managedClient)
	}
	if err != nil {
		result.Message = err.Error()
		return result
	}
	result.Success = true
	if job.GetAction() == jobActionRotateSecret {
		result.Message = "client secret rotated"
	} else {
		result.Message = "client updated"
	}
	result.ResultJson = marshalClientJobResult(applyResult)
	a.setClientName(payload.ClientID, managedClient.Name)
	return result
}

func (a *Agent) handleClientDeleteJob(ctx context.Context, payload clientJobPayload, managedClient telemt.ManagedClient, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	// Deleting an already-absent user is an idempotent success — mirrors the
	// disable path in handleClientUpdateJob. Without this a re-delivered
	// client.delete (panel retry after a lost ack) on an already-removed
	// client would fail forever instead of confirming the terminal state.
	if err := a.telemt.DeleteClient(ctx, managedClient.Name); err != nil && !errors.Is(err, telemt.ErrClientNotFound) {
		result.Message = err.Error()
		return result
	}
	result.Success = true
	result.Message = "client deleted"
	a.deleteClientName(payload.ClientID)
	return result
}

// clientResetQuotaJobPayload is the JSON envelope panel→agent for a
// client.reset_quota job. Only the Telemt username is needed; the
// panel resolves it from the client's centrally-stored Name.
type clientResetQuotaJobPayload struct {
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
}

// clientResetQuotaJobResult is the JSON envelope agent→panel inside
// JobResult.result_json. UnsupportedTelemt / ReadOnlyTelemt let the
// panel surface a friendly per-deployment message instead of a generic
// transport error when the agent's local Telemt cannot honour the
// reset (older Telemt version, or read-only mode). Both flags are
// independent of result.Success — when either is true, success stays
// false and the panel renders the typed reason from the UI.
type clientResetQuotaJobResult struct {
	UsedBytes          uint64 `json:"used_bytes"`
	LastResetEpochSecs uint64 `json:"last_reset_epoch_secs"`
	UnsupportedTelemt  bool   `json:"unsupported_telemt,omitempty"`
	ReadOnlyTelemt     bool   `json:"read_only_telemt,omitempty"`
}

// handleClientResetQuotaJob runs the client.reset_quota action against
// the local Telemt instance. The two typed errors from the telemt
// client (ErrResetQuotaUnsupported / ErrResetQuotaReadOnly) surface as
// flagged failures so the panel UI can show a precise reason instead
// of a generic transport error. All other failures fall back to the
// default success=false + message=err.Error() shape so existing
// observability paths still apply.
func (a *Agent) handleClientResetQuotaJob(ctx context.Context, job *gatewayrpc.JobCommand, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	var payload clientResetQuotaJobPayload
	if err := json.Unmarshal([]byte(job.GetPayloadJson()), &payload); err != nil {
		result.Message = fmt.Sprintf("invalid reset_quota payload: %v", err)
		return result
	}
	if strings.TrimSpace(payload.Name) == "" {
		result.Message = "invalid reset_quota payload: empty name"
		return result
	}

	snapshot, err := a.telemt.ResetUserQuota(ctx, payload.Name)
	if err != nil {
		typed := clientResetQuotaJobResult{}
		switch {
		case errors.Is(err, telemt.ErrResetQuotaUnsupported):
			typed.UnsupportedTelemt = true
		case errors.Is(err, telemt.ErrResetQuotaReadOnly):
			typed.ReadOnlyTelemt = true
		}
		result.Message = err.Error()
		if typed.UnsupportedTelemt || typed.ReadOnlyTelemt {
			result.ResultJson = marshalResetQuotaResult(typed)
		}
		return result
	}

	result.Success = true
	result.Message = "client quota reset"
	result.ResultJson = marshalResetQuotaResult(clientResetQuotaJobResult{
		UsedBytes:          snapshot.UsedBytes,
		LastResetEpochSecs: snapshot.LastResetEpochSecs,
	})
	return result
}

func marshalResetQuotaResult(payload clientResetQuotaJobResult) string {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(bytes)
}

// selfUpdateExecute is swapped in tests so handler-level behaviour can be
// exercised without touching the network.
var selfUpdateExecute = updater.Execute

// handleSelfUpdateJob runs the agent.self-update action. A3 contract: the
// JobResult is RETURNED (and flushed by the job worker) before any restart
// happens; the restart itself is delegated to Config.ScheduleSelfRestart.
func (a *Agent) handleSelfUpdateJob(ctx context.Context, job *gatewayrpc.JobCommand, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	var payload updater.Payload
	if err := json.Unmarshal([]byte(job.GetPayloadJson()), &payload); err != nil {
		result.Message = fmt.Sprintf("invalid update payload: %v", err)
		return result
	}
	outcome, err := selfUpdateExecute(ctx, payload, a.config.Version, slog.Default())
	if err != nil {
		result.Message = err.Error()
		return result
	}
	if outcome == updater.OutcomeNoop {
		result.Success = true
		result.Message = fmt.Sprintf("already at target version %s", a.config.Version)
		return result
	}
	result.Success = true
	result.Message = "self-update applied; restart scheduled"
	if a.config.ScheduleSelfRestart == nil {
		slog.Warn("self-update: no restart hook configured; new binary takes effect on next process restart")
		return result
	}
	a.config.ScheduleSelfRestart()
	return result
}

// CompletedJobResult returns the cached result for a previously-executed
// job, if still retained. Used by the receive path to answer a duplicate
// delivery with the real result instead of a bare ack — so a JobResult lost
// in transit after the first ack still reaches the control-plane on the
// CP's retry, without re-executing the job.
func (a *Agent) CompletedJobResult(jobID string, observedAt time.Time) (*gatewayrpc.JobResult, bool) {
	return a.findCompletedJobResult(jobID, observedAt)
}

func (a *Agent) findCompletedJobResult(jobID string, observedAt time.Time) (*gatewayrpc.JobResult, bool) {
	if strings.TrimSpace(jobID) == "" {
		return nil, false
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.pruneCompletedJobsLocked(observedAt)

	completed, ok := a.completedJobs[jobID]
	if !ok {
		return nil, false
	}

	return &gatewayrpc.JobResult{
		AgentId:        a.config.AgentID,
		JobId:          jobID,
		Success:        completed.Success,
		Message:        completed.Message,
		ResultJson:     completed.ResultJSON,
		ObservedAtUnix: observedAt.UTC().Unix(),
	}, true
}

func (a *Agent) rememberCompletedJobResult(jobID string, result *gatewayrpc.JobResult, observedAt time.Time) {
	if strings.TrimSpace(jobID) == "" || result == nil {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.pruneCompletedJobsLocked(observedAt)
	a.completedJobs[jobID] = completedJobRecord{
		CompletedAt: observedAt.UTC(),
		Success:     result.Success,
		Message:     result.Message,
		ResultJSON:  result.ResultJson,
	}
}

func (a *Agent) pruneCompletedJobsLocked(now time.Time) {
	if a.completedJobRetention <= 0 {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	cutoff := now.Add(-a.completedJobRetention)
	for jobID, completed := range a.completedJobs {
		if completed.CompletedAt.Before(cutoff) {
			delete(a.completedJobs, jobID)
		}
	}
}

// HandleClientDataRequest fetches all configured Telemt users and returns them as ClientDetailRecords.
// This enables the control-plane to discover users that exist on the server but are not managed by the panel.
func (a *Agent) HandleClientDataRequest(ctx context.Context, requestID string) *gatewayrpc.ClientDataResponse {
	configPath := a.resolveTelemtConfigPath(ctx)

	users, err := a.telemt.FetchDiscoveredUsers(ctx, configPath)
	if err != nil {
		slog.WarnContext(ctx, "client discovery skipped: telemt user list unavailable",
			"request_id", requestID, "error", err)
		return &gatewayrpc.ClientDataResponse{
			RequestId:         requestID,
			TelemtUnreachable: true,
		}
	}

	records := make([]*gatewayrpc.ClientDetailRecord, 0, len(users))
	for _, u := range users {
		clientID := a.clientIDForName(u.Username)
		records = append(records, &gatewayrpc.ClientDetailRecord{
			ClientId:           clientID,
			ClientName:         u.Username,
			Secret:             u.Secret,
			UserAdTag:          u.UserADTag,
			Enabled:            u.Enabled,
			TotalOctets:        u.TotalOctets,
			CurrentConnections: int32(u.CurrentConnections),
			ActiveUniqueIps:    int32(u.ActiveUniqueIPs),
			ConnectionLinks:    u.ConnectionLinks,
			MaxTcpConns:        int32(u.MaxTCPConns),
			MaxUniqueIps:       int32(u.MaxUniqueIPs),
			DataQuotaBytes:     u.DataQuotaBytes,
			Expiration:         u.ExpirationRFC3339,
		})
	}

	return &gatewayrpc.ClientDataResponse{
		RequestId: requestID,
		Clients:   records,
	}
}

// resolveTelemtConfigPath returns the path to the Telemt config file.
// Priority: explicit config setting → /v1/system/info API response.
func (a *Agent) resolveTelemtConfigPath(ctx context.Context) string {
	if a.config.TelemtConfigPath != "" {
		return a.config.TelemtConfigPath
	}

	info, err := a.telemt.FetchSystemInfo(ctx)
	if err != nil {
		return ""
	}
	return info.ConfigPath
}

func marshalClientJobResult(result telemt.ClientApplyResult) string {
	payload, err := json.Marshal(map[string]any{
		"connection_links": result.ConnectionLinks,
	})
	if err != nil {
		return ""
	}

	return string(payload)
}

func (a *Agent) clientIDForName(name string) string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.clientIDForNameLocked(name)
}

func (a *Agent) clientIDForNameLocked(name string) string {
	for clientID, clientName := range a.clientNames {
		if clientName == name {
			return clientID
		}
	}
	return ""
}

func (a *Agent) setClientName(clientID, name string) {
	if clientID == "" || name == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.clientNames[clientID] = name
}

func (a *Agent) deleteClientName(clientID string) {
	if clientID == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	delete(a.clientNames, clientID)
}
