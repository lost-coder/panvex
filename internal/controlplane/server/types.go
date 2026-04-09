package server

import (
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

// Agent stores the control-plane view of a connected host agent.
type Agent struct {
	ID           string    `json:"id"`
	NodeName     string    `json:"node_name"`
	FleetGroupID string    `json:"fleet_group_id"`
	Version      string    `json:"version"`
	ReadOnly     bool      `json:"read_only"`
	PresenceState string   `json:"presence_state"`
	CertificateRecovery *agentCertificateRecoveryGrantResponse `json:"certificate_recovery,omitempty"`
	Runtime      AgentRuntime `json:"runtime"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

// RuntimeEvent stores one recent Telemt runtime event normalized by the agent.
type RuntimeEvent struct {
	Sequence      uint64 `json:"sequence"`
	TimestampUnix int64  `json:"timestamp_unix"`
	EventType     string `json:"event_type"`
	Context       string `json:"context"`
}

// RuntimeDC stores one DC health row reported by the local Telemt runtime.
type RuntimeDC struct {
	DC                 int     `json:"dc"`
	AvailableEndpoints int     `json:"available_endpoints"`
	AvailablePct       float64 `json:"available_pct"`
	RequiredWriters    int     `json:"required_writers"`
	AliveWriters       int     `json:"alive_writers"`
	CoveragePct        float64 `json:"coverage_pct"`
	RTTMs              float64 `json:"rtt_ms"`
	Load               int     `json:"load"`
}

// RuntimeUpstream stores one upstream health row reported by the local Telemt runtime.
type RuntimeUpstream struct {
	UpstreamID         int     `json:"upstream_id"`
	RouteKind          string  `json:"route_kind"`
	Address            string  `json:"address"`
	Healthy            bool    `json:"healthy"`
	Fails              int     `json:"fails"`
	EffectiveLatencyMs float64 `json:"effective_latency_ms"`
}

// AgentRuntime stores the normalized Telemt operator overview for one agent.
type AgentRuntime struct {
	AcceptingNewConnections   bool              `json:"accepting_new_connections"`
	MERuntimeReady            bool              `json:"me_runtime_ready"`
	ME2DCFallbackEnabled      bool              `json:"me2dc_fallback_enabled"`
	UseMiddleProxy            bool              `json:"use_middle_proxy"`
	StartupStatus             string            `json:"startup_status"`
	StartupStage              string            `json:"startup_stage"`
	StartupProgressPct        float64           `json:"startup_progress_pct"`
	InitializationStatus      string            `json:"initialization_status"`
	Degraded                  bool              `json:"degraded"`
	LifecycleState            string            `json:"lifecycle_state"`
	InitializationStage       string            `json:"initialization_stage"`
	InitializationProgressPct float64           `json:"initialization_progress_pct"`
	TransportMode             string            `json:"transport_mode"`
	CurrentConnections        int               `json:"current_connections"`
	CurrentConnectionsME      int               `json:"current_connections_me"`
	CurrentConnectionsDirect  int               `json:"current_connections_direct"`
	ActiveUsers               int               `json:"active_users"`
	UptimeSeconds             float64           `json:"uptime_seconds"`
	ConnectionsTotal          uint64            `json:"connections_total"`
	ConnectionsBadTotal       uint64            `json:"connections_bad_total"`
	HandshakeTimeoutsTotal    uint64            `json:"handshake_timeouts_total"`
	ConfiguredUsers           int               `json:"configured_users"`
	DCCoveragePct             float64           `json:"dc_coverage_pct"`
	HealthyUpstreams          int               `json:"healthy_upstreams"`
	TotalUpstreams            int               `json:"total_upstreams"`
	DCs                       []RuntimeDC       `json:"dcs"`
	Upstreams                 []RuntimeUpstream `json:"upstreams"`
	RecentEvents              []RuntimeEvent    `json:"recent_events"`
	SystemLoad                RuntimeSystemLoad `json:"system_load"`
	UpdatedAt                 time.Time         `json:"updated_at"`
}

// RuntimeSystemLoad carries server resource utilization metrics.
type RuntimeSystemLoad struct {
	CPUUsagePct      float64 `json:"cpu_usage_pct"`
	MemoryUsedBytes  uint64  `json:"memory_used_bytes"`
	MemoryTotalBytes uint64  `json:"memory_total_bytes"`
	MemoryUsagePct   float64 `json:"memory_usage_pct"`
	DiskUsedBytes    uint64  `json:"disk_used_bytes"`
	DiskTotalBytes   uint64  `json:"disk_total_bytes"`
	DiskUsagePct     float64 `json:"disk_usage_pct"`
	Load1M           float64 `json:"load_1m"`
	Load5M           float64 `json:"load_5m"`
	Load15M          float64 `json:"load_15m"`
	NetBytesSent     uint64  `json:"net_bytes_sent"`
	NetBytesRecv     uint64  `json:"net_bytes_recv"`
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
		ID:           agent.ID,
		NodeName:     agent.NodeName,
		FleetGroupID: agent.FleetGroupID,
		Version:      agent.Version,
		ReadOnly:     agent.ReadOnly,
		LastSeenAt:   agent.LastSeenAt.UTC(),
	}
}

func agentFromRecord(record storage.AgentRecord) Agent {
	return Agent{
		ID:           record.ID,
		NodeName:     record.NodeName,
		FleetGroupID: record.FleetGroupID,
		Version:      record.Version,
		ReadOnly:     record.ReadOnly,
		LastSeenAt:   record.LastSeenAt.UTC(),
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

func auditEventToRecord(event AuditEvent) storage.AuditEventRecord {
	return storage.AuditEventRecord{
		ID:        event.ID,
		ActorID:   event.ActorID,
		Action:    event.Action,
		TargetID:  event.TargetID,
		CreatedAt: event.CreatedAt.UTC(),
		Details:   event.Details,
	}
}

func auditEventFromRecord(record storage.AuditEventRecord) AuditEvent {
	return AuditEvent{
		ID:        record.ID,
		ActorID:   record.ActorID,
		Action:    record.Action,
		TargetID:  record.TargetID,
		CreatedAt: record.CreatedAt.UTC(),
		Details:   record.Details,
	}
}
