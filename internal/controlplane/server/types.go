package server

import (
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

// Agent stores the control-plane view of a connected host agent.
type Agent struct {
	ID            string    `json:"id"`
	NodeName      string    `json:"node_name"`
	EnvironmentID string    `json:"environment_id"`
	FleetGroupID  string    `json:"fleet_group_id"`
	Version       string    `json:"version"`
	ReadOnly      bool      `json:"read_only"`
	LastSeenAt    time.Time `json:"last_seen_at"`
}

// Instance stores the Telemt runtime metadata discovered through an agent.
type Instance struct {
	ID               string    `json:"id"`
	AgentID          string    `json:"agent_id"`
	Name             string    `json:"name"`
	Version          string    `json:"version"`
	ConfigFingerprint string   `json:"config_fingerprint"`
	ConnectedUsers   int       `json:"connected_users"`
	ReadOnly         bool      `json:"read_only"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// MetricSnapshot stores an aggregated view of a single agent or instance metric set.
type MetricSnapshot struct {
	ID         string            `json:"id"`
	AgentID    string            `json:"agent_id"`
	InstanceID string            `json:"instance_id"`
	CapturedAt time.Time         `json:"captured_at"`
	Values     map[string]uint64 `json:"values"`
}

// AuditEvent stores an immutable operator or security event emitted by the control-plane.
type AuditEvent struct {
	ID        string         `json:"id"`
	ActorID   string         `json:"actor_id"`
	Action    string         `json:"action"`
	TargetID  string         `json:"target_id"`
	CreatedAt time.Time      `json:"created_at"`
	Details   map[string]any `json:"details"`
}

func agentToRecord(agent Agent) storage.AgentRecord {
	return storage.AgentRecord{
		ID:            agent.ID,
		NodeName:      agent.NodeName,
		EnvironmentID: agent.EnvironmentID,
		FleetGroupID:  agent.FleetGroupID,
		Version:       agent.Version,
		ReadOnly:      agent.ReadOnly,
		LastSeenAt:    agent.LastSeenAt.UTC(),
	}
}

func agentFromRecord(record storage.AgentRecord) Agent {
	return Agent{
		ID:            record.ID,
		NodeName:      record.NodeName,
		EnvironmentID: record.EnvironmentID,
		FleetGroupID:  record.FleetGroupID,
		Version:       record.Version,
		ReadOnly:      record.ReadOnly,
		LastSeenAt:    record.LastSeenAt.UTC(),
	}
}

func instanceToRecord(instance Instance) storage.InstanceRecord {
	return storage.InstanceRecord{
		ID:                instance.ID,
		AgentID:           instance.AgentID,
		Name:              instance.Name,
		Version:           instance.Version,
		ConfigFingerprint: instance.ConfigFingerprint,
		ConnectedUsers:    instance.ConnectedUsers,
		ReadOnly:          instance.ReadOnly,
		UpdatedAt:         instance.UpdatedAt.UTC(),
	}
}

func instanceFromRecord(record storage.InstanceRecord) Instance {
	return Instance{
		ID:                record.ID,
		AgentID:           record.AgentID,
		Name:              record.Name,
		Version:           record.Version,
		ConfigFingerprint: record.ConfigFingerprint,
		ConnectedUsers:    record.ConnectedUsers,
		ReadOnly:          record.ReadOnly,
		UpdatedAt:         record.UpdatedAt.UTC(),
	}
}

func metricSnapshotToRecord(snapshot MetricSnapshot) storage.MetricSnapshotRecord {
	return storage.MetricSnapshotRecord{
		ID:         snapshot.ID,
		AgentID:    snapshot.AgentID,
		InstanceID: snapshot.InstanceID,
		CapturedAt: snapshot.CapturedAt.UTC(),
		Values:     snapshot.Values,
	}
}

func metricSnapshotFromRecord(record storage.MetricSnapshotRecord) MetricSnapshot {
	return MetricSnapshot{
		ID:         record.ID,
		AgentID:    record.AgentID,
		InstanceID: record.InstanceID,
		CapturedAt: record.CapturedAt.UTC(),
		Values:     record.Values,
	}
}
