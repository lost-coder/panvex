package api

import "time"

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
	TransportReconnectPending bool `json:"transport_reconnect_pending,omitempty"`
	// BootstrapState is the enrollment lifecycle state of the underlying
	// agents row: "pending" (provisioned, awaiting first enrollment),
	// "expired" (bootstrap token lapsed before enrollment), or "active"
	// (enrolled). Empty for legacy inbound rows. Meaningful for the UI only
	// while the node has never connected (presence != online) — a
	// half-added node renders as pending/expired instead of offline (R9a).
	BootstrapState string `json:"bootstrap_state,omitempty"`
	// DialTransportMode is the persisted transport mode of the agents row —
	// "inbound" (agent dials the panel) or "outbound" (panel dials the
	// agent). Distinct from Runtime.TransportMode, which is Telemt's
	// classic/middle_proxy runtime mode (R9a).
	DialTransportMode string `json:"dial_transport_mode,omitempty"`
	// DialAddress is the panel-dials-agent target (host:port) persisted for
	// outbound rows. Not exposed in the public JSON shape — it is carried on
	// the live snapshot only so the transport-drift self-heal can rebuild the
	// agent listen bind without a store round-trip (R10b Task 4 / R-4).
	DialAddress string `json:"-"`
	// TransportDrift is true when the live connection direction of the last
	// accepted stream disagreed with DialTransportMode (agent still dialing IN
	// while the DB says outbound, or vice versa). Request-time derived from the
	// in-memory drift marker; the panel re-enqueues the switch job to converge.
	TransportDrift      bool                                   `json:"transport_drift,omitempty"`
	CertificateRecovery *AgentCertificateRecoveryGrantResponse `json:"certificate_recovery,omitempty"`
	CertIssuedAt        *time.Time                             `json:"cert_issued_at,omitempty"`
	CertExpiresAt       *time.Time                             `json:"cert_expires_at,omitempty"`
	// CertSerial is the serial of the most recently issued client cert.
	// Used to pin agent identity at gRPC connect time (Q4.U-S-04). Not
	// exposed in the public JSON shape — operators don't need it and
	// it's noise in the dashboard.
	CertSerial string `json:"-"`
	// TelemtUpdateProbe reports whether (and how) this node's telemt
	// install can be updated in place: which supervisor fronts the
	// process, and — when no in-place update path exists — why. Cached by
	// the agent once at process startup and stamped on every snapshot; nil
	// until the agent has sent at least one such snapshot (older agents
	// that predate the probe never populate it).
	TelemtUpdateProbe *TelemtUpdateProbe `json:"telemt_update_probe,omitempty"`
	Runtime           AgentRuntime       `json:"runtime"`
	LastSeenAt        time.Time          `json:"last_seen_at"`
}

// TelemtUpdateProbe is the presentation view of gatewayrpc.TelemtUpdateProbe
// — the result of the agent probing its local process supervisor
// (systemd/OpenRC/procd/runit/docker/none) once at startup, used by the
// panel to decide whether an in-place telemt update is safe to offer.
type TelemtUpdateProbe struct {
	// Mode is "binary" (a supported supervisor owns telemt; a binary swap +
	// supervised restart is safe), "docker" (telemt runs in a container;
	// updates must go through the image), or "none" (nothing detected).
	Mode string `json:"mode"`
	// SuggestedRestartSpec is the telemtrestart spec ("systemd:telemt",
	// "openrc:telemt", "procd:telemt", "runit:telemt") to use for the
	// supervised restart after a binary swap. Empty when Mode != "binary".
	SuggestedRestartSpec string `json:"suggested_restart_spec"`
	// BinaryPath is the best-effort resolved path to the telemt
	// executable, used as the swap target. May be empty if it could not be
	// resolved.
	BinaryPath string `json:"binary_path"`
	// Available is whether an in-place update is offered at all.
	Available bool `json:"available"`
	// Reason is a localizable CODE (not a human-readable phrase)
	// explaining why Available is false, e.g. "docker_only",
	// "no_service_manager_detected". Empty when Available is true.
	Reason string `json:"reason"`
}

// AgentCertificateRecoveryGrantResponse is the operator-facing view of an
// agent certificate-recovery grant (nested in Agent and returned directly by
// the recovery endpoints). Pure presentation; the storage<->view conversion
// lives in the server package.
type AgentCertificateRecoveryGrantResponse struct {
	AgentID       string `json:"agent_id"`
	Status        string `json:"status"`
	IssuedAtUnix  int64  `json:"issued_at_unix"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
	UsedAtUnix    *int64 `json:"used_at_unix,omitempty"`
	RevokedAtUnix *int64 `json:"revoked_at_unix,omitempty"`
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
	// UserTelemetrySuppressed reports that Telemt on this node is not
	// emitting per-user series — user telemetry is switched off, or the
	// 4096-user labeled-series cap truncated them. The per-client traffic
	// and quota-usage figures the panel shows for this node are therefore
	// stale or zero, and the UI raises a warning banner.
	//
	// Deliberately NOT an input to node status or reason text: unlike
	// TelemtUnreachable this says nothing about the node's ability to serve
	// traffic, only about the fidelity of one telemetry view. Polarity
	// matches TelemtUnreachable — false (the zero value, and the proto3
	// default) is the healthy case.
	UserTelemetrySuppressed bool `json:"user_telemetry_suppressed"`
	// ReportedObservedAt — the agent's own snapshot timestamp, kept ONLY
	// for clock-skew diagnostics (P3-3.2, audit #25b). All liveness/freshness
	// decisions are made on the panel clock: LastSeenAt,
	// Runtime.UpdatedAt, and presence.Heartbeat are stamped with s.now() at the
	// moment the snapshot is received. The UI does not use this field for statuses.
	ReportedObservedAt time.Time `json:"reported_observed_at"`
	UpdatedAt          time.Time `json:"updated_at"`
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
