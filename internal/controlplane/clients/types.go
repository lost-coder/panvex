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
	ID                string
	Name              string
	Secret            string
	UserADTag         string
	Enabled           bool
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    int64
	ExpirationRFC3339 string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

// Assignment attaches a Client to an Agent either directly or via a
// FleetGroup. TargetType distinguishes the two cases; exactly one of
// FleetGroupID / AgentID is populated per row, matching the TargetType.
type Assignment struct {
	ID           string
	ClientID     string
	TargetType   string
	FleetGroupID string
	AgentID      string
	CreatedAt    time.Time
}

// Deployment is the per-(client, agent) desired-state row. Status reflects
// the outcome of the last reconcile attempt for this pair; ConnectionLink
// is the agent-reported MTProto link (populated on a successful apply).
type Deployment struct {
	ClientID         string
	AgentID          string
	DesiredOperation string
	Status           string
	LastError        string
	ConnectionLink   string
	LastAppliedAt    *time.Time
	UpdatedAt        time.Time
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
	ConnectionLink     string
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
//
// Seq is the monotonic per-agent snapshot sequence number (proto field
// 7). Zero means the field was absent on the wire (legacy agent or
// internal synthetic entry); the dedup path treats zero as "unknown"
// and accumulates unconditionally, preserving pre-P2-LOG-06 behavior.
// See Service.ApplyUsageSnapshot for the dedup semantics.
type UsageSnapshot struct {
	ClientID         string
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
	ActiveUniqueIPs  int
	ObservedAt       time.Time
	Seq              uint64
}

// AggregatedUsage is the sum-over-agents of UsageSnapshot for a single
// client. Returned by Service.AggregateUsage and the equivalent method
// on controlplane/server.Server.
type AggregatedUsage struct {
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
}
