// internal/controlplane/server/clients_uow_adapter.go
//
// clientsUoWAdapter bridges uow.UnitOfWork → clients.ServiceUoW.
//
// uow.RepoSet satisfies clients.ClientsRepoSet structurally (both expose
// Clients(), Discovered(), and Audit()), but Go's type system requires an
// explicit adapter because the callback signatures differ:
//
//	uow.UnitOfWork.Do(ctx, func(uow.RepoSet) error)
//	clients.ServiceUoW.Do(ctx, func(clients.ClientsRepoSet) error)
//
// The adapter wraps the concrete uow.UnitOfWork and converts the inner
// callback so the server/bootstrap layer can hand clients.NewServiceV2 a
// clients.ServiceUoW without importing the uow package from the clients
// package (which would create an import cycle).
package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/audit"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
)

// clientsUoWAdapter wraps a concrete uow.UnitOfWork and presents the
// clients.ServiceUoW interface. uow.RepoSet is a superset of
// clients.ClientsRepoSet (it adds Jobs()), so the inner fn can be forwarded
// directly by passing the uow.RepoSet as a clients.ClientsRepoSet.
//
// When clientsOverride is non-nil, the adapter substitutes the override for
// rs.Clients() inside the tx callback. Used by failure-injection tests
// (Options.ClientsRepoOverride) so SaveState's Repository writes hit the
// failingClientsRepository instead of the tx-bound real repo. Production
// wiring leaves clientsOverride nil.
type clientsUoWAdapter struct {
	inner           uow.UnitOfWork
	clientsOverride clients.Repository
}

// newClientsUoWAdapter constructs an adapter that satisfies clients.ServiceUoW
// and delegates to the given uow.UnitOfWork. Pass the concrete sqlite.NewUoW or
// postgres.NewUoW result here; the adapter is wired in lifecycle.go.
func newClientsUoWAdapter(u uow.UnitOfWork) clients.ServiceUoW {
	return &clientsUoWAdapter{inner: u}
}

// newClientsUoWAdapterWithOverride is like newClientsUoWAdapter but also swaps
// the tx-bound rs.Clients() with the given override Repository. Test-only.
func newClientsUoWAdapterWithOverride(u uow.UnitOfWork, override clients.Repository) clients.ServiceUoW {
	return &clientsUoWAdapter{inner: u, clientsOverride: override}
}

// Do satisfies clients.ServiceUoW. uow.RepoSet structurally satisfies
// clients.ClientsRepoSet (Clients(), Discovered(), Audit() are all present),
// so we forward the concrete rs directly — unless clientsOverride is set, in
// which case we wrap rs to swap out the Clients() method.
func (a *clientsUoWAdapter) Do(ctx context.Context, fn func(rs clients.ClientsRepoSet) error) error {
	return a.inner.Do(ctx, func(rs uow.RepoSet) error {
		if a.clientsOverride == nil {
			return fn(rs)
		}
		return fn(&overrideClientsRepoSet{inner: rs, clients: a.clientsOverride})
	})
}

// overrideClientsRepoSet satisfies clients.ClientsRepoSet by delegating
// Discovered/Audit to the underlying tx-bound RepoSet, while returning the
// override Repository from Clients(). Jobs() is also forwarded so the type
// still satisfies the broader uow.RepoSet shape if anything inspects it.
type overrideClientsRepoSet struct {
	inner   uow.RepoSet
	clients clients.Repository
}

func (s *overrideClientsRepoSet) Clients() clients.Repository       { return s.clients }
func (s *overrideClientsRepoSet) Discovered() discovered.Repository { return s.inner.Discovered() }
func (s *overrideClientsRepoSet) Audit() audit.Repository           { return s.inner.Audit() }
func (s *overrideClientsRepoSet) Jobs() jobs.Repository             { return s.inner.Jobs() }
