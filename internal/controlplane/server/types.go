package server

import "time"

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
