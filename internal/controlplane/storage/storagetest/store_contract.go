package storagetest

import (
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// OpenStore constructs a fresh storage backend for one contract test run.
// It returns MigrationStore so that low-level storagetest helpers (agents
// cascade, bulk ops) can access ClientStore methods and the raw *sql.DB
// (via a DB() type-assertion, e.g. the discovered_clients cascade seed)
// without those being part of the Store aggregate.
type OpenStore func(t *testing.T) storage.MigrationStore

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

	// clients and discovered contracts removed in Wave 4.2 Phase 8:
	// superseded by per-domain Repository contracts in
	// internal/controlplane/clients/storagetest/ and
	// internal/controlplane/discovered/storagetest/.
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
	runFallbackContract(t, open)
	runAgentConfigTargetContract(t, open)
	runConfigApplyBatchContract(t, open)

	// Transact contract (P2-ARCH-01) lives in store_contract_transact.go.
	// Bulk-write helpers (P3-PERF-01a) live in store_contract_bulk.go.
	runTransactContract(t, open)
	runBulkWriteContract(t, open)
	runBulkTelemetryContract(t, open)
}
