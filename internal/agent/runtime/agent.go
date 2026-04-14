package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/agent/updater"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

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
	InvalidateSlowDataCache()
}

// Config describes the control-plane identity reported by the agent.
type Config struct {
	AgentID          string
	NodeName         string
	FleetGroupID     string
	Version          string
	TelemtConfigPath string
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
	config Config
	telemt telemtClient
	mu     sync.RWMutex

	clientNames       map[string]string
	lastOctets        map[string]uint64
	lastConnections   map[string]int
	lastMetricsUptime float64
	lastRuntimeUptime float64
	lastLifecycle     runtimeLifecycleState
	runtimeInitializationActive bool
	runtimeInitializationCooldownUntil time.Time
	completedJobs     map[string]completedJobRecord
	completedJobRetention time.Duration
	ipCollector       *telemt.IPCollector
}

// New constructs a runtime agent bound to one local Telemt client.
func New(config Config, client telemtClient) *Agent {
	return &Agent{
		config:          config,
		telemt:          client,
		clientNames:     make(map[string]string),
		lastOctets:      make(map[string]uint64),
		lastConnections: make(map[string]int),
		completedJobs:   make(map[string]completedJobRecord),
		completedJobRetention: defaultCompletedJobRetention,
		ipCollector:     telemt.NewIPCollector(),
	}
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

func startupLifecycleRegressed(previous runtimeLifecycleState, current runtimeLifecycleState) bool {
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

func (a *Agent) BuildRuntimeSnapshot(ctx context.Context, observedAt time.Time) (*gatewayrpc.Snapshot, error) {
	state, err := a.telemt.FetchRuntimeState(ctx)
	if err != nil {
		return nil, err
	}

	dcs := make([]*gatewayrpc.RuntimeDCSnapshot, 0, len(state.DCs))
	for _, dc := range state.DCs {
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

	upstreamRows := make([]*gatewayrpc.RuntimeUpstreamRowSnapshot, 0, len(state.Upstreams.Rows))
	for _, upstream := range state.Upstreams.Rows {
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

	recentEvents := make([]*gatewayrpc.RuntimeEventSnapshot, 0, len(state.RecentEvents))
	for _, event := range state.RecentEvents {
		recentEvents = append(recentEvents, &gatewayrpc.RuntimeEventSnapshot{
			Sequence:      event.Sequence,
			TimestampUnix: event.TimestampUnix,
			EventType:     event.EventType,
			Context:       event.Context,
		})
	}

	lifecycle := lifecycleStateForRuntime(state)
	wasRestarting := false

	a.mu.Lock()
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
	a.lastRuntimeUptime = state.UptimeSeconds
	a.mu.Unlock()

	snapshot := a.baseSnapshot(observedAt)
	snapshot.ReadOnly = state.ReadOnly
	snapshot.Instances = []*gatewayrpc.InstanceSnapshot{
		{
			Id:                "telemt-primary",
			Name:              "telemt-primary",
			Version:           state.Version,
			ConfigFingerprint: "runtime",
			ConnectedUsers:    int32(state.ConnectedUsers),
			ReadOnly:          state.ReadOnly,
		},
	}
	snapshot.Metrics = map[string]uint64{
		"connected_users": uint64(state.ConnectedUsers),
	}
	snapshot.Runtime = &gatewayrpc.RuntimeSnapshot{
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
		Dcs:                       dcs,
		Upstreams: &gatewayrpc.RuntimeUpstreamSnapshot{
			ConfiguredTotal: int32(state.Upstreams.ConfiguredTotal),
			HealthyTotal:    int32(state.Upstreams.HealthyTotal),
			UnhealthyTotal:  int32(state.Upstreams.UnhealthyTotal),
			DirectTotal:     int32(state.Upstreams.DirectTotal),
			Socks5Total:     int32(state.Upstreams.SOCKS5Total),
			Rows:            upstreamRows,
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
	snapshot.RuntimeDiagnostics = &gatewayrpc.RuntimeDiagnosticsSnapshot{
		State:               state.Diagnostics.State,
		StateReason:         state.Diagnostics.StateReason,
		SystemInfoJson:      state.Diagnostics.SystemInfoJSON,
		EffectiveLimitsJson: state.Diagnostics.EffectiveLimitsJSON,
		SecurityPostureJson: state.Diagnostics.SecurityPostureJSON,
		MinimalAllJson:      state.Diagnostics.MinimalAllJSON,
		MePoolJson:          state.Diagnostics.MEPoolJSON,
		DcsJson:             state.Diagnostics.DcsJSON,
	}
	snapshot.RuntimeSecurityInventory = &gatewayrpc.RuntimeSecurityInventorySnapshot{
		State:       state.SecurityInventory.State,
		StateReason: state.SecurityInventory.StateReason,
		Enabled:     state.SecurityInventory.Enabled,
		EntriesTotal: int32(state.SecurityInventory.EntriesTotal),
		EntriesJson: state.SecurityInventory.EntriesJSON,
	}
	snapshot.TotalActiveConnections = int32(state.ConnectionTotals.CurrentConnections)
	snapshot.TotalActiveUsers = int32(state.ConnectionTotals.ActiveUsers)

	return snapshot, nil
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
	defer a.mu.Unlock()

	restarted := metricsSnapshot.UptimeSeconds > 0 && metricsSnapshot.UptimeSeconds < a.lastMetricsUptime
	clients := make([]*gatewayrpc.ClientUsageSnapshot, 0, len(usageRows))
	seen := make(map[string]struct{}, len(usageRows))
	for _, client := range usageRows {
		clientID := client.ClientID
		if clientID == "" && client.ClientName != "" {
			clientID = a.clientIDForNameLocked(client.ClientName)
		}

		// Use clientID as tracking key when available, fall back to name.
		trackingKey := clientID
		if trackingKey == "" {
			if client.ClientName == "" {
				continue
			}
			trackingKey = "name:" + client.ClientName
		}

		currentTotal := client.TrafficUsedBytes
		previousTotal := a.lastOctets[trackingKey]
		delta := currentTotal
		if !restarted && currentTotal >= previousTotal {
			delta = currentTotal - previousTotal
		}
		connectionsChanged := a.lastConnections[trackingKey] != client.ActiveTCPConns

		a.lastOctets[trackingKey] = currentTotal
		a.lastConnections[trackingKey] = client.ActiveTCPConns
		seen[trackingKey] = struct{}{}

		if delta == 0 && !connectionsChanged && client.CurrentIPsUsed == 0 {
			continue
		}
		clients = append(clients, &gatewayrpc.ClientUsageSnapshot{
			ClientId:          clientID,
			ClientName:        client.ClientName,
			TrafficDeltaBytes: delta,
			UniqueIpsUsed:     int32(client.UniqueIPsUsed),
			ActiveTcpConns:    int32(client.ActiveTCPConns),
			ActiveUniqueIps:   int32(client.CurrentIPsUsed),
		})
	}

	for clientID := range a.lastConnections {
		if _, ok := seen[clientID]; ok {
			continue
		}
		delete(a.lastConnections, clientID)
		delete(a.lastOctets, clientID)
	}
	if metricsSnapshot.UptimeSeconds > 0 {
		a.lastMetricsUptime = metricsSnapshot.UptimeSeconds
	}

	snapshot := a.baseSnapshot(observedAt)
	snapshot.Clients = clients
	snapshot.HasClientUsage = true
	return snapshot, nil
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
		if err := a.telemt.ExecuteRuntimeReload(ctx); err != nil {
			result.Message = err.Error()
			return result
		}

		result.Success = true
		result.Message = "runtime reloaded"
		return result
	case "telemetry.refresh_diagnostics":
		a.telemt.InvalidateSlowDataCache()
		result.Success = true
		result.Message = "diagnostics refresh requested"
		return result
	case "client.create", "client.update", "client.rotate_secret", "client.delete":
		var payload struct {
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
		if err := json.Unmarshal([]byte(job.GetPayloadJson()), &payload); err != nil {
			result.Message = fmt.Sprintf("invalid client payload: %v", err)
			return result
		}

		managedClient := telemt.ManagedClient{
			PreviousName:      payload.PreviousName,
			Name:              payload.Name,
			Secret:            payload.Secret,
			UserADTag:         payload.UserADTag,
			Enabled:           payload.Enabled,
			MaxTCPConns:       payload.MaxTCPConns,
			MaxUniqueIPs:      payload.MaxUniqueIPs,
			DataQuotaBytes:    payload.DataQuotaBytes,
			ExpirationRFC3339: payload.ExpirationRFC3339,
		}

		switch job.GetAction() {
		case "client.create":
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
		case "client.update", "client.rotate_secret":
			applyResult, err := a.telemt.UpdateClient(ctx, managedClient)
			if err != nil {
				result.Message = err.Error()
				return result
			}
			result.Success = true
			if job.GetAction() == "client.rotate_secret" {
				result.Message = "client secret rotated"
			} else {
				result.Message = "client updated"
			}
			result.ResultJson = marshalClientJobResult(applyResult)
			a.setClientName(payload.ClientID, managedClient.Name)
			return result
		default:
			if err := a.telemt.DeleteClient(ctx, managedClient.Name); err != nil {
				result.Message = err.Error()
				return result
			}
			result.Success = true
			result.Message = "client deleted"
			a.deleteClientName(payload.ClientID)
			return result
		}
	case "agent.self-update":
		var payload updater.Payload
		if err := json.Unmarshal([]byte(job.GetPayloadJson()), &payload); err != nil {
			result.Message = fmt.Sprintf("invalid update payload: %v", err)
			return result
		}
		if err := updater.Execute(ctx, payload, slog.Default()); err != nil {
			result.Message = err.Error()
			return result
		}
		result.Success = true
		result.Message = "self-update initiated"
		return result
	default:
		result.Message = fmt.Sprintf("unsupported action %s", job.GetAction())
		return result
	}
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
		return &gatewayrpc.ClientDataResponse{RequestId: requestID}
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
			ConnectionLink:     u.ConnectionLink,
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
	payload, err := json.Marshal(map[string]string{
		"connection_link": result.ConnectionLink,
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

func (a *Agent) setClientName(clientID string, name string) {
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
