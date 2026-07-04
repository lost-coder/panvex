package server

import (
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Agent stores the control-plane view of a connected host agent.
type Agent struct {
	ID            string `json:"id"`
	NodeName      string `json:"node_name"`
	FleetGroupID  string `json:"fleet_group_id"`
	Version       string `json:"version"`
	ReadOnly      bool   `json:"read_only"`
	PresenceState string `json:"presence_state"`
	// TransportReconnectPending is true when the operator switched this
	// agent to outbound transport and no agent session has been accepted
	// since (A2 "switched but never reconnected"). Request-time derived.
	TransportReconnectPending bool                                   `json:"transport_reconnect_pending,omitempty"`
	CertificateRecovery       *agentCertificateRecoveryGrantResponse `json:"certificate_recovery,omitempty"`
	CertIssuedAt              *time.Time                             `json:"cert_issued_at,omitempty"`
	CertExpiresAt             *time.Time                             `json:"cert_expires_at,omitempty"`
	// CertSerial is the serial of the most recently issued client cert.
	// Used to pin agent identity at gRPC connect time (Q4.U-S-04). Not
	// exposed in the public JSON shape — operators don't need it and
	// it's noise in the dashboard.
	CertSerial string       `json:"-"`
	Runtime    AgentRuntime `json:"runtime"`
	LastSeenAt time.Time    `json:"last_seen_at"`
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
	FreshAliveWriters  int     `json:"fresh_alive_writers"`
	FreshCoveragePct   float64 `json:"fresh_coverage_pct"`
	RTTMs              float64 `json:"rtt_ms"`
	Load               int     `json:"load"`
}

// RuntimeUpstream stores one upstream health row reported by the local Telemt runtime.
type RuntimeUpstream struct {
	UpstreamID         int      `json:"upstream_id"`
	RouteKind          string   `json:"route_kind"`
	Address            string   `json:"address"`
	Healthy            bool     `json:"healthy"`
	Fails              int      `json:"fails"`
	EffectiveLatencyMs float64  `json:"effective_latency_ms"`
	Weight             int      `json:"weight"`
	LastCheckAgeSecs   int      `json:"last_check_age_secs"`
	Scopes             []string `json:"scopes,omitempty"`
}

// ConnectionClassCount is one (class, total) pair from Telemt's
// classified bad-connection and handshake-failure counters introduced
// in Telemt 3.4.10. The class set is open-ended; the panel forwards
// rows as-is so a newer Telemt label surfaces in the UI without a
// control-plane upgrade.
type ConnectionClassCount struct {
	Class string `json:"class"`
	Total uint64 `json:"total"`
}

// AgentRuntime stores the normalized Telemt operator overview for one agent.
type AgentRuntime struct {
	AcceptingNewConnections bool `json:"accepting_new_connections"`
	MERuntimeReady          bool `json:"me_runtime_ready"`
	ME2DCFallbackEnabled    bool `json:"me2dc_fallback_enabled"`
	// IN-H5: route_mode/reroute_active/me2dc_fast_enabled arrive in the
	// snapshot (proto fields 31/32/33) but were previously dropped on the
	// panel — the operator could not see the node's actual routing mode or
	// active reroute/fast-fallback state.
	ME2DCFastEnabled          bool                   `json:"me2dc_fast_enabled"`
	RouteMode                 string                 `json:"route_mode"`
	RerouteActive             bool                   `json:"reroute_active"`
	UseMiddleProxy            bool                   `json:"use_middle_proxy"`
	StartupStatus             string                 `json:"startup_status"`
	StartupStage              string                 `json:"startup_stage"`
	StartupProgressPct        float64                `json:"startup_progress_pct"`
	InitializationStatus      string                 `json:"initialization_status"`
	Degraded                  bool                   `json:"degraded"`
	LifecycleState            string                 `json:"lifecycle_state"`
	InitializationStage       string                 `json:"initialization_stage"`
	InitializationProgressPct float64                `json:"initialization_progress_pct"`
	TransportMode             string                 `json:"transport_mode"`
	CurrentConnections        int                    `json:"current_connections"`
	CurrentConnectionsME      int                    `json:"current_connections_me"`
	CurrentConnectionsDirect  int                    `json:"current_connections_direct"`
	ActiveUsers               int                    `json:"active_users"`
	UptimeSeconds             float64                `json:"uptime_seconds"`
	ConnectionsTotal          uint64                 `json:"connections_total"`
	ConnectionsBadTotal       uint64                 `json:"connections_bad_total"`
	ConnectionsBadByClass     []ConnectionClassCount `json:"connections_bad_by_class"`
	HandshakeFailuresByClass  []ConnectionClassCount `json:"handshake_failures_by_class"`
	HandshakeTimeoutsTotal    uint64                 `json:"handshake_timeouts_total"`
	ConfiguredUsers           int                    `json:"configured_users"`
	DCCoveragePct             float64                `json:"dc_coverage_pct"`
	HealthyUpstreams          int                    `json:"healthy_upstreams"`
	TotalUpstreams            int                    `json:"total_upstreams"`
	UnhealthyUpstreams        int                    `json:"unhealthy_upstreams"`
	DirectUpstreams           int                    `json:"direct_upstreams"`
	Socks4Upstreams           int                    `json:"socks4_upstreams"`
	Socks5Upstreams           int                    `json:"socks5_upstreams"`
	ShadowsocksUpstreams      int                    `json:"shadowsocks_upstreams"`
	// FailRatePct5m and FailRateKnown encode the same "nil-is-unknown"
	// pattern as RuntimeUpstreamSummary on the agent side. The wire format
	// (JSON tags fail_rate_pct_5m + fail_rate_known) is split for
	// backward-compatible consumers; internal Go callers should prefer
	// FailRatePct5mPtr() / SetFailRatePct5m() so the pair stays in lockstep.
	FailRatePct5m        float64 `json:"fail_rate_pct_5m"`
	FailRateKnown        bool    `json:"fail_rate_known"`
	ConnectAttemptTotal  uint64  `json:"connect_attempt_total"`
	ConnectSuccessTotal  uint64  `json:"connect_success_total"`
	ConnectFailTotal     uint64  `json:"connect_fail_total"`
	ConnectFailfastTotal uint64  `json:"connect_failfast_total"`
	// FallbackEnteredAtUnix is the unix timestamp the panel saw this agent
	// transition into ME->DC fallback. Absent (omitempty) when the agent is
	// not currently in fallback. Sourced from the in-memory
	// fallbackEnteredAt map; surfaced so the dashboard can render a live
	// "fallback for Xm" timer without a second round-trip.
	FallbackEnteredAtUnix      *int64                   `json:"fallback_entered_at_unix,omitempty"`
	DCs                        []RuntimeDC              `json:"dcs"`
	Upstreams                  []RuntimeUpstream        `json:"upstreams"`
	RecentEvents               []RuntimeEvent           `json:"recent_events"`
	SystemLoad                 RuntimeSystemLoad        `json:"system_load"`
	MeWritersSummary           *RuntimeMeWritersSummary `json:"me_writers_summary,omitempty"`
	TelemtUnreachable          bool                     `json:"telemt_unreachable"`
	TelemtUnreachableSinceUnix int64                    `json:"telemt_unreachable_since_unix"`
	UpdatedAt                  time.Time                `json:"updated_at"`
}

// FailRatePct5mPtr returns the 5-minute upstream connect fail-rate as a
// pointer, with nil indicating "unknown" (FailRateKnown == false). Mirrors
// the agent-side helper so internal call sites can avoid touching the
// parallel FailRatePct5m / FailRateKnown fields directly.
func (r AgentRuntime) FailRatePct5mPtr() *float64 {
	if !r.FailRateKnown {
		return nil
	}
	v := r.FailRatePct5m
	return &v
}

// SetFailRatePct5m updates FailRatePct5m and FailRateKnown together: a nil
// pointer marks the rate unknown, a non-nil pointer stores the value and
// flips FailRateKnown to true. Always prefer this over touching the
// parallel fields directly.
func (r *AgentRuntime) SetFailRatePct5m(rate *float64) {
	if rate == nil {
		r.FailRatePct5m = 0
		r.FailRateKnown = false
		return
	}
	r.FailRatePct5m = *rate
	r.FailRateKnown = true
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

// RuntimeMeWritersSummary carries the ME writers pool aggregate returned by Telemt.
type RuntimeMeWritersSummary struct {
	ConfiguredEndpoints int     `json:"configured_endpoints"`
	AvailableEndpoints  int     `json:"available_endpoints"`
	CoveragePct         float64 `json:"coverage_pct"`
	FreshAliveWriters   int     `json:"fresh_alive_writers"`
	FreshCoveragePct    float64 `json:"fresh_coverage_pct"`
	RequiredWriters     int     `json:"required_writers"`
	AliveWriters        int     `json:"alive_writers"`
}

// Instance stores the Telemt runtime metadata discovered through an agent.
type Instance struct {
	ID                string    `json:"id"`
	AgentID           string    `json:"agent_id"`
	Name              string    `json:"name"`
	Version           string    `json:"version"`
	ConfigFingerprint string    `json:"config_fingerprint"`
	ManagedConfigHash string    `json:"managed_config_hash"`
	ManagedConfigJSON string    `json:"managed_config_json"` // last non-empty observed editable sections (canonical JSON)
	Connections       int       `json:"connections"`
	ReadOnly          bool      `json:"read_only"`
	UpdatedAt         time.Time `json:"updated_at"`
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
		FleetGroupID:  agent.FleetGroupID,
		Version:       agent.Version,
		ReadOnly:      agent.ReadOnly,
		LastSeenAt:    agent.LastSeenAt.UTC(),
		CertIssuedAt:  agent.CertIssuedAt,
		CertExpiresAt: agent.CertExpiresAt,
	}
}

func agentFromRecord(record storage.AgentRecord) Agent {
	return Agent{
		ID:            record.ID,
		NodeName:      record.NodeName,
		FleetGroupID:  record.FleetGroupID,
		Version:       record.Version,
		ReadOnly:      record.ReadOnly,
		LastSeenAt:    record.LastSeenAt.UTC(),
		CertIssuedAt:  record.CertIssuedAt,
		CertExpiresAt: record.CertExpiresAt,
	}
}

func instanceToRecord(instance Instance) storage.InstanceRecord {
	return storage.InstanceRecord{
		ID:                instance.ID,
		AgentID:           instance.AgentID,
		Name:              instance.Name,
		Version:           instance.Version,
		ConfigFingerprint: instance.ConfigFingerprint,
		Connections:       instance.Connections,
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
		Connections:       record.Connections,
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
		Details:   normalizeAuditDetails(record.Details),
	}
}
