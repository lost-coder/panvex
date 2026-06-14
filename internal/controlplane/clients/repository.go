// internal/controlplane/clients/repository.go
//
// Repository is the aggregate-level CRUD contract for the clients
// domain. Implementations live in storage/postgres and storage/sqlite.
//
// Convention:
//   - Read methods return domain types (Client, Assignment, Deployment, Usage).
//   - Write methods accept domain types.
//   - The Secret field on Client is OPAQUE BYTES at this layer — Service
//     is responsible for vault.Encrypt before Save and vault.Decrypt
//     after Get. Repository never touches plaintext.
//   - Errors: storage.ErrNotFound for missing rows, storage.ErrConflict
//     for unique-constraint violations. Other errors are wrapped with %w.
package clients

import (
	"context"
)

type Repository interface {
	// Single-client aggregate ops.
	Get(ctx context.Context, id ClientID) (Client, error)
	// GetBySubscriptionToken looks up a non-deleted client by its
	// subscription_token column. Returns storage.ErrNotFound when no
	// matching row exists or the token is blank.
	GetBySubscriptionToken(ctx context.Context, token string) (Client, error)
	List(ctx context.Context) ([]Client, error)
	Save(ctx context.Context, c Client) error
	Delete(ctx context.Context, id ClientID) error

	// Assignment ops (per-client list).
	ListAssignments(ctx context.Context, clientID ClientID) ([]Assignment, error)
	SaveAssignments(ctx context.Context, clientID ClientID, assignments []Assignment) error
	DeleteAssignments(ctx context.Context, clientID ClientID) error

	// Deployment ops (per-client list).
	ListDeployments(ctx context.Context, clientID ClientID) ([]Deployment, error)
	SaveDeployments(ctx context.Context, clientID ClientID, deployments []Deployment) error
	// PutDeployment upserts a single deployment record (per-client, per-agent).
	// Used by PersistDeployment to replace the legacy s.store.PutClientDeployment path.
	PutDeployment(ctx context.Context, d Deployment) error

	// Per-(client, agent) usage counters.
	UpsertUsage(ctx context.Context, u Usage) error
	UpsertUsageBulk(ctx context.Context, batch []Usage) error
	ListUsage(ctx context.Context) ([]Usage, error)
	DeleteUsageByClient(ctx context.Context, id ClientID) error
}
