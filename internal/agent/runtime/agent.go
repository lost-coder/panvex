package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/panvex/panvex/internal/agent/telemt"
	"github.com/panvex/panvex/internal/gatewayrpc"
)

type telemtClient interface {
	FetchRuntimeState(context.Context) (telemt.RuntimeState, error)
	ExecuteRuntimeReload(context.Context) error
}

// Config describes the control-plane identity reported by the agent.
type Config struct {
	AgentID       string
	NodeName      string
	EnvironmentID string
	FleetGroupID  string
	Version       string
}

// Agent builds snapshots and executes control-plane commands against local Telemt.
type Agent struct {
	config Config
	telemt telemtClient
}

// New constructs a runtime agent bound to one local Telemt client.
func New(config Config, client telemtClient) *Agent {
	return &Agent{
		config: config,
		telemt: client,
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

	return &gatewayrpc.Snapshot{
		AgentID:        a.config.AgentID,
		NodeName:       a.config.NodeName,
		EnvironmentID:  a.config.EnvironmentID,
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
	default:
		result.Message = fmt.Sprintf("unsupported action %s", job.Action)
		return result
	}
}
