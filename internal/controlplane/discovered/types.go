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
	Status             Status
	TotalOctets        uint64
	CurrentConnections uint32
	ActiveUniqueIPs    uint32
	FirstSeen          time.Time
	UpdatedAt          time.Time
	// secret-related fields handled at Service boundary; not exposed on
	// the domain type at rest.
}
