package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/panvex/panvex/internal/agent/telemt"
	"github.com/panvex/panvex/internal/gatewayrpc"
)

type telemtClient interface {
	FetchRuntimeState(context.Context) (telemt.RuntimeState, error)
	FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error)
	FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error)
	ExecuteRuntimeReload(context.Context) error
	CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error)
	UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error)
	DeleteClient(context.Context, string) error
}

// Config describes the control-plane identity reported by the agent.
type Config struct {
	AgentID      string
	NodeName     string
	FleetGroupID string
	Version      string
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

// BuildRuntimeSnapshot converts the current Telemt runtime state into a gateway snapshot.
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
		UseMiddleProxy:            state.Gates.UseMiddleProxy,
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
	}
	snapshot.TotalActiveConnections = int32(state.ConnectionTotals.CurrentConnections)
	snapshot.TotalActiveUsers = int32(state.ConnectionTotals.ActiveUsers)

	return snapshot, nil
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
		if clientID == "" {
			continue
		}

		currentTotal := client.TrafficUsedBytes
		previousTotal := a.lastOctets[clientID]
		delta := currentTotal
		if !restarted && currentTotal >= previousTotal {
			delta = currentTotal - previousTotal
		}
		connectionsChanged := a.lastConnections[clientID] != client.ActiveTCPConns

		a.lastOctets[clientID] = currentTotal
		a.lastConnections[clientID] = client.ActiveTCPConns
		seen[clientID] = struct{}{}

		if delta == 0 && !connectionsChanged && client.CurrentIPsUsed == 0 {
			continue
		}
		clients = append(clients, &gatewayrpc.ClientUsageSnapshot{
			ClientId:          clientID,
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
		if clientID == "" {
			continue
		}
		clientIPs = append(clientIPs, &gatewayrpc.ClientIPSnapshot{
			ClientId:  clientID,
			ActiveIps: append([]string(nil), user.ActiveIPs...),
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
