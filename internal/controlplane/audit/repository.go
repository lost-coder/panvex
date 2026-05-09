// internal/controlplane/audit/repository.go
//
// Repository is the minimal persistence surface needed by callers that
// must write audit events inside multi-domain transactions (e.g.
// clients.Service.AdoptDiscovered). The full audit query surface
// remains on storage.AuditStore until a later wave promotes it.
package audit

import (
	"context"
	"time"
)

// Event is the domain type for a single audit-log entry.
type Event struct {
	ID        string
	ActorID   string
	Action    string
	TargetID  string
	CreatedAt time.Time
	Details   map[string]any
	PrevHash  string
	EventHash string
}

// Repository is the write-side contract for audit events.
// Implementations live in storage/postgres and storage/sqlite.
type Repository interface {
	// Append persists one audit event. Callers are responsible for
	// computing PrevHash / EventHash before calling Append.
	Append(ctx context.Context, e Event) error
}
