// internal/controlplane/discovered/types.go
package discovered

import "time"

type Status string

const (
	StatusPending Status = "pending_review"
	StatusAdopted Status = "adopted"
	StatusIgnored Status = "ignored"
)

// DiscoveredClient is the operator-visible snapshot of a Telemt client
// observed on an agent that the panel does not yet manage. The agent
// publishes (re-publishes) these via FULL_SNAPSHOT reconcile messages;
// the panel dedupes by (AgentID, ClientName) before persisting.
type DiscoveredClient struct {
	ID         DiscoveredID
	AgentID    string // agents-domain ID; not strong-typed yet (Wave 4.2-agents will)
	ClientName string
	// Secret is the Telemt client secret (hex). It is carried here for the
	// adopt path and for sibling deduplication, and it MUST NOT reach the wire:
	// this type has no json tags and is never serialised — the HTTP layer
	// projects it onto discoveredClientResponse, which has no secret field.
	// (Audit item verified, not a leak; keep it that way.)
	Secret string
	Status Status
	// UserADTag is the Telemt ad-tag reported by the agent for this client.
	// Enabled mirrors the client's enabled flag on the node. Both feed the
	// adopt path so the managed client starts from the node's real state —
	// dropping them meant the first post-adopt edit PATCHed user_ad_tag:null
	// to the node and wiped the tag (wire-audit I4).
	UserADTag          string
	Enabled            bool
	TotalOctets        uint64
	CurrentConnections uint32
	ActiveUniqueIPs    uint32
	ConnectionLinks    []string // per-agent Telegram connection links reported by Telemt
	MaxTCPConns        int
	MaxUniqueIPs       int
	DataQuotaBytes     int64
	Expiration         string // RFC3339 or empty; sourced from Telemt agent report
	FirstSeen          time.Time
	UpdatedAt          time.Time
}
