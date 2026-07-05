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
	"database/sql"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
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

// testOverrideUoW reproduces the removed production ClientsRepoOverride seam
// on the test side: rs.Clients() inside a UoW callback returns the swapped
// (usually failing) repository, while Discovered() stays the real tx-bound
// one. P5 moved this out of production wiring (audit #19).
type testOverrideUoW struct {
	inner   uow.UnitOfWork
	clients clients.Repository
}

func (a *testOverrideUoW) Do(ctx context.Context, fn func(rs clients.ClientsRepoSet) error) error {
	return a.inner.Do(ctx, func(rs uow.RepoSet) error {
		return fn(&testOverrideRepoSet{inner: rs, clients: a.clients})
	})
}

type testOverrideRepoSet struct {
	inner   uow.RepoSet
	clients clients.Repository
}

func (s *testOverrideRepoSet) Clients() clients.Repository       { return s.clients }
func (s *testOverrideRepoSet) Discovered() discovered.Repository { return s.inner.Discovered() }

// swapClientsRepoForTests rebuilds s.clientsSvc so both the direct Repo and
// rs.Clients() inside the UoW point at repo. Call right after constructing the
// server, before generating load. rawDB is the *sql.DB the test's store wraps
// (failingStore wraps a real SQLite store).
func swapClientsRepoForTests(t *testing.T, s *Server, rawDB *sql.DB, repo clients.Repository) {
	t.Helper()
	s.clientsSvc = clients.NewService(clients.ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: sqlite.NewDiscoveredRepository(rawDB),
		UoW:            &testOverrideUoW{inner: sqlite.NewUoW(rawDB), clients: repo},
		Vault:          s.secretVault,
		Now:            s.now,
	})
}
