// internal/controlplane/server/clients_uow_adapter.go
//
// clientsUoWAdapter bridges uow.UnitOfWork → clients.ServiceUoW.
//
// uow.RepoSet satisfies clients.ClientsRepoSet structurally (both expose
// Clients() and Discovered()), but Go's type system requires an
// explicit adapter because the callback signatures differ:
//
//	uow.UnitOfWork.Do(ctx, func(uow.RepoSet) error)
//	clients.ServiceUoW.Do(ctx, func(clients.ClientsRepoSet) error)
//
// The adapter wraps the concrete uow.UnitOfWork and converts the inner
// callback so the server/bootstrap layer can hand clients.NewService a
// clients.ServiceUoW without importing the uow package from the clients
// package (which would create an import cycle).
package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
)

// clientsUoWAdapter wraps a concrete uow.UnitOfWork and presents the
// clients.ServiceUoW interface. uow.RepoSet structurally satisfies
// clients.ClientsRepoSet (both expose Clients() and Discovered()), so the
// inner fn can be forwarded directly by passing the uow.RepoSet as a
// clients.ClientsRepoSet.
type clientsUoWAdapter struct {
	inner uow.UnitOfWork
}

// newClientsUoWAdapter constructs an adapter that satisfies clients.ServiceUoW
// and delegates to the given uow.UnitOfWork. Pass the concrete sqlite.NewUoW or
// postgres.NewUoW result here; the adapter is wired in lifecycle.go.
func newClientsUoWAdapter(u uow.UnitOfWork) clients.ServiceUoW {
	return &clientsUoWAdapter{inner: u}
}

// Do satisfies clients.ServiceUoW by forwarding the concrete tx-bound
// uow.RepoSet directly as a clients.ClientsRepoSet.
func (a *clientsUoWAdapter) Do(ctx context.Context, fn func(rs clients.ClientsRepoSet) error) error {
	return a.inner.Do(ctx, func(rs uow.RepoSet) error {
		return fn(rs)
	})
}
