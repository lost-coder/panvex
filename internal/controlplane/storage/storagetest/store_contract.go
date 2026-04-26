package storagetest

import (
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// OpenStore constructs a fresh storage backend for one contract test run.
type OpenStore func(t *testing.T) storage.Store

// testFleetGroupID is a deterministic UUIDv4 used as the fleet-group
// primary key inside contract-test fixtures. Postgres stores ids in a
// UUID column since migration 0014, so every PutFleetGroup call must
// pass a real UUID. We pick a fixed value so assertions that mention
// the id stay readable.
const testFleetGroupID = "00000000-0000-4000-a000-000000000001"

// RunStoreContract verifies that a storage backend satisfies the shared
// persistence contract. The body delegates to per-domain helpers so each
// thematic file stays under the 400 LOC ceiling (R-Q-18). New domains
// should land as a fresh runFooContract helper in store_contract_foo.go
// and be wired in here.
func RunStoreContract(t *testing.T, open OpenStore) {
	t.Helper()

	runClientsContract(t, open)
	runSettingsContract(t, open)
	runAuthorityContract(t, open)
	runUsersContract(t, open)
	runEnrollmentContract(t, open)
	runAgentsContract(t, open)
	runJobsContract(t, open)
	runAuditContract(t, open)
	runMetricsContract(t, open)
	runTelemetryContract(t, open)
	runSessionsContract(t, open)
	runDiscoveredContract(t, open)

	// Transact contract (P2-ARCH-01) lives in store_contract_transact.go.
	// Bulk-write helpers (P3-PERF-01a) live in store_contract_bulk.go.
	runTransactContract(t, open)
	runBulkWriteContract(t, open)
}
