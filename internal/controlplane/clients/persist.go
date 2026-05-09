package clients

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// PersistState writes client + assignments + deployments via the given
// storage.Store in the canonical order (client, then replace
// assignments, then upsert deployments). Extracted so the same
// sequence can be driven either against a top-level Store directly OR
// against a tx-bound Store inside Store.Transact. See P2-ARCH-01.
//
// Ordering invariant: the client row is written first so foreign-key
// constraints on the assignment/deployment tables are always
// satisfied. DeleteClientAssignments erases any previous rows before
// the new set is inserted — this preserves the all-or-nothing
// semantics that the server relies on.
//
// The vault parameter, when non-nil and enabled, encrypts the client
// secret at-rest. Passing nil keeps legacy plaintext behaviour for
// dev/tests where no encryption key is configured.
//
// Deprecated: new code should use Service.SaveState (UoW-backed, uses
// clients.Repository). PersistState is retained only because
// persistAdoptedClient in server/clients_discovery.go drives it inside
// a Store.Transact closure where a full UoW migration is not yet
// feasible (P2-ARCH-01). AC#12 deletion is deferred to the wave that
// migrates persistAdoptedClient to Service.AdoptDiscovered.
func PersistState(
	ctx context.Context,
	store storage.Store,
	client Client,
	assignments []Assignment,
	deployments []Deployment,
	vault *secretvault.Vault,
) error {
	if store == nil {
		return nil
	}
	record, err := EncryptClientRecord(ClientToRecord(client), vault)
	if err != nil {
		return err
	}
	if err := store.PutClient(ctx, record); err != nil {
		return err
	}
	if err := store.DeleteClientAssignments(ctx, string(client.ID)); err != nil {
		return err
	}
	for _, assignment := range assignments {
		if err := store.PutClientAssignment(ctx, AssignmentToRecord(assignment)); err != nil {
			return err
		}
	}
	for _, deployment := range deployments {
		if err := store.PutClientDeployment(ctx, DeploymentToRecord(deployment)); err != nil {
			return err
		}
	}
	return nil
}
