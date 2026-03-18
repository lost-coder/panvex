package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/panvex/panvex/internal/agent/telemt"
	"github.com/panvex/panvex/internal/gatewayrpc"
)

type telemtClient interface {
	FetchRuntimeState(context.Context) (telemt.RuntimeState, error)
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

// Agent builds snapshots and executes control-plane commands against local Telemt.
type Agent struct {
	config Config
	telemt telemtClient
	mu     sync.RWMutex
	clientNames map[string]string
}

// New constructs a runtime agent bound to one local Telemt client.
func New(config Config, client telemtClient) *Agent {
	return &Agent{
		config: config,
		telemt: client,
		clientNames: make(map[string]string),
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

// BuildSnapshot converts the current Telemt runtime state into a gateway snapshot.
func (a *Agent) BuildSnapshot(ctx context.Context, observedAt time.Time) (*gatewayrpc.Snapshot, error) {
	state, err := a.telemt.FetchRuntimeState(ctx)
	if err != nil {
		return nil, err
	}

	clients := make([]gatewayrpc.ClientUsageSnapshot, 0, len(state.Clients))
	for _, client := range state.Clients {
		clientID := client.ClientID
		if clientID == "" && client.ClientName != "" {
			clientID = a.clientIDForName(client.ClientName)
		}
		if clientID == "" {
			continue
		}
		clients = append(clients, gatewayrpc.ClientUsageSnapshot{
			ClientID:         clientID,
			TrafficUsedBytes: client.TrafficUsedBytes,
			UniqueIPsUsed:    client.UniqueIPsUsed,
			ActiveTCPConns:   client.ActiveTCPConns,
		})
	}

	dcs := make([]gatewayrpc.RuntimeDCSnapshot, 0, len(state.DCs))
	for _, dc := range state.DCs {
		dcs = append(dcs, gatewayrpc.RuntimeDCSnapshot{
			DC:                 dc.DC,
			AvailableEndpoints: dc.AvailableEndpoints,
			AvailablePct:       dc.AvailablePct,
			RequiredWriters:    dc.RequiredWriters,
			AliveWriters:       dc.AliveWriters,
			CoveragePct:        dc.CoveragePct,
			RTTMs:              dc.RTTMs,
			Load:               dc.Load,
		})
	}

	upstreamRows := make([]gatewayrpc.RuntimeUpstreamRowSnapshot, 0, len(state.Upstreams.Rows))
	for _, upstream := range state.Upstreams.Rows {
		upstreamRows = append(upstreamRows, gatewayrpc.RuntimeUpstreamRowSnapshot{
			UpstreamID:         upstream.UpstreamID,
			RouteKind:          upstream.RouteKind,
			Address:            upstream.Address,
			Healthy:            upstream.Healthy,
			Fails:              upstream.Fails,
			EffectiveLatencyMs: upstream.EffectiveLatencyMs,
		})
	}

	recentEvents := make([]gatewayrpc.RuntimeEventSnapshot, 0, len(state.RecentEvents))
	for _, event := range state.RecentEvents {
		recentEvents = append(recentEvents, gatewayrpc.RuntimeEventSnapshot{
			Sequence:      event.Sequence,
			TimestampUnix: event.TimestampUnix,
			EventType:     event.EventType,
			Context:       event.Context,
		})
	}

	return &gatewayrpc.Snapshot{
		AgentID:        a.config.AgentID,
		NodeName:       a.config.NodeName,
		FleetGroupID:   a.config.FleetGroupID,
		Version:        a.config.Version,
		ReadOnly:       state.ReadOnly,
		ObservedAtUnix: observedAt.UTC().Unix(),
		Instances: []gatewayrpc.InstanceSnapshot{
			{
				ID:                "telemt-primary",
				Name:              "telemt-primary",
				Version:           state.Version,
				ConfigFingerprint: "runtime",
				ConnectedUsers:    state.ConnectedUsers,
				ReadOnly:          state.ReadOnly,
			},
		},
		Metrics: map[string]uint64{
			"connected_users": uint64(state.ConnectedUsers),
		},
		Clients: clients,
		Runtime: gatewayrpc.RuntimeSnapshot{
			AcceptingNewConnections:   state.Gates.AcceptingNewConnections,
			MERuntimeReady:            state.Gates.MERuntimeReady,
			ME2DCFallbackEnabled:      state.Gates.ME2DCFallbackEnabled,
			UseMiddleProxy:            state.Gates.UseMiddleProxy,
			StartupStatus:             state.Gates.StartupStatus,
			StartupStage:              state.Gates.StartupStage,
			StartupProgressPct:        state.Gates.StartupProgressPct,
			InitializationStatus:      state.Initialization.Status,
			Degraded:                  state.Initialization.Degraded,
			InitializationStage:       state.Initialization.CurrentStage,
			InitializationProgressPct: state.Initialization.ProgressPct,
			TransportMode:             state.Initialization.TransportMode,
			CurrentConnections:        state.ConnectionTotals.CurrentConnections,
			CurrentConnectionsME:      state.ConnectionTotals.CurrentConnectionsME,
			CurrentConnectionsDirect:  state.ConnectionTotals.CurrentConnectionsDirect,
			ActiveUsers:               state.ConnectionTotals.ActiveUsers,
			ConnectionsTotal:          state.Summary.ConnectionsTotal,
			ConnectionsBadTotal:       state.Summary.ConnectionsBadTotal,
			HandshakeTimeoutsTotal:    state.Summary.HandshakeTimeoutsTotal,
			ConfiguredUsers:           state.Summary.ConfiguredUsers,
			DCs:                       dcs,
			Upstreams: gatewayrpc.RuntimeUpstreamSnapshot{
				ConfiguredTotal: state.Upstreams.ConfiguredTotal,
				HealthyTotal:    state.Upstreams.HealthyTotal,
				UnhealthyTotal:  state.Upstreams.UnhealthyTotal,
				DirectTotal:     state.Upstreams.DirectTotal,
				SOCKS5Total:     state.Upstreams.SOCKS5Total,
				Rows:            upstreamRows,
			},
			RecentEvents: recentEvents,
		},
	}, nil
}

// HandleJob executes a supported job command and returns an execution result envelope.
func (a *Agent) HandleJob(ctx context.Context, job *gatewayrpc.JobCommand, observedAt time.Time) *gatewayrpc.JobResult {
	result := &gatewayrpc.JobResult{
		AgentID:        a.config.AgentID,
		JobID:          job.ID,
		ObservedAtUnix: observedAt.UTC().Unix(),
	}

	switch job.Action {
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
		if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
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

		switch job.Action {
		case "client.create":
			applyResult, err := a.telemt.CreateClient(ctx, managedClient)
			if err != nil {
				result.Message = err.Error()
				return result
			}
			result.Success = true
			result.Message = "client created"
			result.ResultJSON = marshalClientJobResult(applyResult)
			a.setClientName(payload.ClientID, managedClient.Name)
			return result
		case "client.update", "client.rotate_secret":
			applyResult, err := a.telemt.UpdateClient(ctx, managedClient)
			if err != nil {
				result.Message = err.Error()
				return result
			}
			result.Success = true
			if job.Action == "client.rotate_secret" {
				result.Message = "client secret rotated"
			} else {
				result.Message = "client updated"
			}
			result.ResultJSON = marshalClientJobResult(applyResult)
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
		result.Message = fmt.Sprintf("unsupported action %s", job.Action)
		return result
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
