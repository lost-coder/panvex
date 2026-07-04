package clients

import "time"

// Assignment-target types for Assignment.TargetType.
const (
	TargetTypeFleetGroup = "fleet_group"
	TargetTypeAgent      = "agent"
)

// Deployment-status values for Deployment.Status.
const (
	DeploymentStatusQueued    = "queued"
	DeploymentStatusSucceeded = "succeeded"
	DeploymentStatusFailed    = "failed"
)

// Discovered-client status values.
const (
	DiscoveredStatusPendingReview = "pending_review"
	DiscoveredStatusAdopted       = "adopted"
	DiscoveredStatusIgnored       = "ignored"
)

// Client is the panel-managed representation of a Telemt proxy client.
// Mirrors storage.ClientRecord but uses time.Time rather than UTC-bound
// strings, and keeps the plaintext Secret field until at-rest encryption
// lands (see controlplane/server/clients_types.go comment).
type Client struct {
	ID                ClientID
	Name              string
	Secret            string
	UserADTag         string
	Enabled           bool
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    int64
	ExpirationRFC3339 string
	SubscriptionToken string // opaque /sub/<token> handle; "" means not yet generated
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

// Assignment attaches a Client to an Agent either directly or via a
// FleetGroup. TargetType distinguishes the two cases; exactly one of
// FleetGroupID / AgentID is populated per row, matching the TargetType.
type Assignment struct {
	ID           AssignmentID
	ClientID     ClientID
	TargetType   string
	FleetGroupID FleetGroupID
	AgentID      string
	CreatedAt    time.Time
}

// Deployment is the per-(client, agent) desired-state row. Status reflects
// the outcome of the last reconcile attempt for this pair; ConnectionLinks
// holds every link Telemt reported for this user (one per tls_domain ×
// host combination), populated on a successful apply.
//
// LastResetEpochSecs is the unix-seconds timestamp captured from Telemt's
// reset-quota response the last time the panel successfully completed a
// client.reset_quota job on this (client, agent). Zero means "never reset
// via the panel"; comparing it to ClientUsage.QuotaLastResetUnix surfaces
// drift between what the panel believes happened and what Telemt still
// reports (e.g. Telemt restart before sidecar persistence, out-of-band
// reset).
// LinkDiagnostic carries an operator-facing warning attached to an
// otherwise-successful apply. The empty string means "all good". IN-M2:
// when a non-delete apply succeeds but the node returns no connection
// links, ConnectionLinks is left untouched (it may now be stale after a
// host/secret change) and this field explains why — so the dashboard can
// flag the link as possibly-stale instead of silently serving it.
type Deployment struct {
	ClientID           ClientID
	AgentID            string
	DesiredOperation   string
	Status             string
	LastError          string
	ConnectionLinks    []string
	LinkDiagnostic     string
	LastAppliedAt      *time.Time
	UpdatedAt          time.Time
	LastResetEpochSecs uint64
}

// DiscoveredRecord describes a proxy client seen on an agent that the
// panel is NOT currently managing. The operator adopts, ignores, or
// re-discovers these via the discovery flow.
type DiscoveredRecord struct {
	ID                 string
	AgentID            string
	ClientName         string
	Secret             string
	Status             string
	TotalOctets        uint64
	CurrentConnections int
	ActiveUniqueIPs    int
	ConnectionLinks    []string
	MaxTCPConns        int
	MaxUniqueIPs       int
	DataQuotaBytes     int64
	Expiration         string
	DiscoveredAt       time.Time
	UpdatedAt          time.Time
}

// UsageSnapshot is the per-(client, agent) live-counter snapshot that
// the usage aggregator accumulates. Mirrors the internal
// clientUsageSnapshot struct on controlplane/server.Server but is
// exposed here for consumers that want to reason about client usage
// without depending on server internals.
type UsageSnapshot struct {
	ClientID           ClientID  `json:"client_id"`
	TrafficUsedBytes   uint64    `json:"traffic_used_bytes"`
	UniqueIPsUsed      int       `json:"unique_ips_used"`
	ActiveTCPConns     int       `json:"active_tcp_conns"`
	ActiveUniqueIPs    int       `json:"active_unique_ips"`
	QuotaUsedBytes     uint64    `json:"quota_used_bytes"`
	QuotaLastResetUnix uint64    `json:"quota_last_reset_unix"`
	ObservedAt         time.Time `json:"observed_at"`
}

// UsageReport is one inbound per-(client, agent) usage row decoded from
// the agent wire snapshot (P4, cumulative counters). TotalBytes is the
// agent-process-cumulative traffic counter; the batch-level agent boot
// id (counter epoch) travels alongside on the server's agentSnapshot.
// The panel derives the accumulation delta against its stored watermark
// — see server.mergeClientUsageBatch.
//
// Distinct from UsageSnapshot (the outbound mirror projection, where
// TrafficUsedBytes is the panel-accumulated absolute) and from Usage
// (the persisted row type).
type UsageReport struct {
	ClientID           ClientID
	TotalBytes         uint64
	UniqueIPsUsed      int
	ActiveTCPConns     int
	ActiveUniqueIPs    int
	QuotaUsedBytes     uint64
	QuotaLastResetUnix uint64
	ObservedAt         time.Time
}

// Usage is the domain-level row type for the (client, agent) traffic
// counter. Mirrors the persisted client_usage row but exposes
// strong-typed ClientID. AgentID stays as plain string until
// agents-domain strong typing lands (Wave 4.2-agents).
//
// Distinct from UsageSnapshot, which is the in-memory mirror's value
// type (missing AgentID since the map key already encodes it).
type Usage struct {
	ClientID           ClientID
	AgentID            string
	TrafficUsedBytes   uint64
	UniqueIPsUsed      int
	ActiveTCPConns     int
	ActiveUniqueIPs    int
	QuotaUsedBytes     uint64
	QuotaLastResetUnix uint64
	// AgentBootID + LastTotalBytes: P4 watermark, see
	// storage.ClientUsageRecord.
	AgentBootID    string
	LastTotalBytes uint64
	ObservedAt     time.Time
}

// AggregatedUsage is the sum-over-agents of UsageSnapshot for a single
// client. Returned by Service.AggregateUsage and the equivalent method
// on controlplane/server.Server.
type AggregatedUsage struct {
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
}
