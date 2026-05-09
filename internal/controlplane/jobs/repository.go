// internal/controlplane/jobs/repository.go
//
// Repository is the minimal persistence surface needed by callers that
// must enqueue jobs inside multi-domain transactions (e.g.
// clients.Service.AdoptDiscovered). The full job query surface
// remains on storage.JobStore until a later wave promotes it.
package jobs

import "context"

// Repository is the write-side contract for jobs.
// Implementations live in storage/postgres and storage/sqlite.
type Repository interface {
	// Put upserts a single job row. Job.Targets and Job.TargetAgentIDs
	// are not written by this method — they require separate calls via the
	// full JobStore surface. Phase 4 minimum surface covers only the jobs
	// table row.
	Put(ctx context.Context, j Job) error
}
