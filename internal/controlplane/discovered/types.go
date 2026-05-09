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
	ID                 DiscoveredID
	AgentID            string // agents-domain ID; not strong-typed yet (Wave 4.2-agents will)
	ClientName         string
	Secret             string   // Telemt client secret (hex); carried for adopt + sibling-dedup
	Status             Status
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
