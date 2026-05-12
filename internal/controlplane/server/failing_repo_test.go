package server

// failing_repo_test.go — Repository-layer failure injection for chaos /
// persistence-contract tests.
//
// After Wave 4.2 (AC#10) the production path for client persistence is:
//
//	clientsSvc.SaveState → uow.Do → clients.Repository.Save / SaveAssignments / …
//
// The old failingStore.putClientErr / putClientAssignmentErr hooks are now
// dead code for this path because the Repository talks directly to *sql.DB,
// not through the storage.Store interface. failingClientsRepository wraps a
// real Repository and injects errors on specific methods so that chaos tests
// can observe the correct failure semantics without touching the Store layer.

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
)

// failingClientsRepository wraps a real clients.Repository and can inject
// errors on specific methods. Pass it via Options.ClientsRepoOverride in
// tests that need Repository-layer failure injection.
//
// IMPORTANT: when an error field is nil, methods return nil WITHOUT
// delegating to the inner Repository. This is intentional — when the
// override is used inside a UoW callback, the embedded Repository is
// non-tx-bound (built from the raw *sql.DB), so any write through it
// would race the open tx and trigger SQLITE_BUSY. The chaos / persistence
// tests only need to verify failure propagation and mirror invariants,
// not actual persistence — so the no-op success path is sufficient.
// The embedded Repository is still required for interface satisfaction
// (failingClientsRepository must implement every clients.Repository
// method; only Save/SaveAssignments/SaveDeployments/Delete/PutDeployment
// /UpsertUsage/UpsertUsageBulk are overridden — the rest pass through).
type failingClientsRepository struct {
	clients.Repository

	saveErr            error
	saveAssignmentsErr error
	saveDeploymentsErr error
	deleteErr          error
	putDeploymentErr   error
	upsertUsageErr     error
	upsertUsageBulkErr error
}

func (r *failingClientsRepository) Save(ctx context.Context, c clients.Client) error {
	return r.saveErr
}

func (r *failingClientsRepository) SaveAssignments(ctx context.Context, clientID clients.ClientID, assignments []clients.Assignment) error {
	return r.saveAssignmentsErr
}

func (r *failingClientsRepository) SaveDeployments(ctx context.Context, clientID clients.ClientID, deployments []clients.Deployment) error {
	return r.saveDeploymentsErr
}

func (r *failingClientsRepository) Delete(ctx context.Context, id clients.ClientID) error {
	return r.deleteErr
}

func (r *failingClientsRepository) PutDeployment(ctx context.Context, d clients.Deployment) error {
	return r.putDeploymentErr
}

func (r *failingClientsRepository) UpsertUsage(ctx context.Context, u clients.Usage) error {
	return r.upsertUsageErr
}

func (r *failingClientsRepository) UpsertUsageBulk(ctx context.Context, batch []clients.Usage) error {
	return r.upsertUsageBulkErr
}
