// internal/controlplane/fleet/repository.go
//
// Repository is the narrow persistence surface that fleet.Service
// actually exercises. It exists so the service depends on exactly the
// methods it calls rather than the whole storage.Store aggregate
// (audit finding A6): the concrete store passed to NewService still
// satisfies this subset, so wiring is unchanged.
//
// Method signatures are copied verbatim from storage.FleetStore /
// storage.IntegrationStore / storage.Store so any storage.Store value
// implements Repository without an adapter. Transact keeps storage.TxFn
// (the tx callback receives the full storage.Store) so the existing
// transactional Delete path composes unchanged.
package fleet

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Repository is the minimal store contract consumed by fleet.Service.
// Implementations live in storage/postgres and storage/sqlite (any
// storage.Store satisfies it).
type Repository interface {
	// Fleet groups.
	CreateFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error
	UpdateFleetGroup(ctx context.Context, group storage.FleetGroupRecord) error
	GetFleetGroup(ctx context.Context, id string) (storage.FleetGroupRecord, error)
	GetFleetGroupByName(ctx context.Context, name string) (storage.FleetGroupRecord, error)
	ListFleetGroups(ctx context.Context) ([]storage.FleetGroupRecord, error)
	DeleteFleetGroup(ctx context.Context, id string) error
	ReassignFleetGroupMembers(ctx context.Context, fromID, toID string) (storage.ReassignCounts, error)
	CountFleetGroupMembers(ctx context.Context, fleetGroupID string) (storage.ReassignCounts, error)

	// Integration providers.
	CreateIntegrationProvider(ctx context.Context, provider storage.IntegrationProviderRecord) error
	UpdateIntegrationProvider(ctx context.Context, provider storage.IntegrationProviderRecord) error
	GetIntegrationProvider(ctx context.Context, id string) (storage.IntegrationProviderRecord, error)
	ListIntegrationProviders(ctx context.Context) ([]storage.IntegrationProviderRecord, error)
	DeleteIntegrationProvider(ctx context.Context, id string) error

	// Fleet-group integrations.
	CreateFleetGroupIntegration(ctx context.Context, integration storage.FleetGroupIntegrationRecord) error
	UpdateFleetGroupIntegration(ctx context.Context, integration storage.FleetGroupIntegrationRecord) error
	GetFleetGroupIntegration(ctx context.Context, id string) (storage.FleetGroupIntegrationRecord, error)
	ListFleetGroupIntegrations(ctx context.Context, fleetGroupID string) ([]storage.FleetGroupIntegrationRecord, error)
	DeleteFleetGroupIntegration(ctx context.Context, id string) error

	// Transact runs fn inside a single database transaction. The tx
	// argument is the full storage.Store bound to the transaction, so
	// Service.Delete's reassignment callback composes unchanged.
	Transact(ctx context.Context, fn storage.TxFn) error
}
