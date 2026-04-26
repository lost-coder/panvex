package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runJobsContract extracts the job and job_target contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runJobsContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("job and job target persistence round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		job := storage.JobRecord{
			ID:             "job-000001",
			Action:         "runtime.reload",
			ActorID:        "user-000001",
			Status:         "queued",
			CreatedAt:      time.Date(2026, time.March, 15, 8, 30, 0, 0, time.UTC),
			TTL:            time.Minute,
			IdempotencyKey: "reload-1",
			PayloadJSON:    `{"scope":"telemt"}`,
		}
		target := storage.JobTargetRecord{
			JobID:      job.ID,
			AgentID:    "agent-000001",
			Status:     "queued",
			UpdatedAt:  job.CreatedAt,
			ResultText: "",
			ResultJSON: `{"accepted":true}`,
		}

		if err := store.PutJob(ctx, job); err != nil {
			t.Fatalf("PutJob() error = %v", err)
		}

		if err := store.PutJobTarget(ctx, target); err != nil {
			t.Fatalf("PutJobTarget() error = %v", err)
		}

		storedJob, err := store.GetJobByIdempotencyKey(ctx, job.IdempotencyKey)
		if err != nil {
			t.Fatalf("GetJobByIdempotencyKey() error = %v", err)
		}

		if storedJob.ID != job.ID {
			t.Fatalf("GetJobByIdempotencyKey() ID = %q, want %q", storedJob.ID, job.ID)
		}
		if storedJob.PayloadJSON != job.PayloadJSON {
			t.Fatalf("GetJobByIdempotencyKey() PayloadJSON = %q, want %q", storedJob.PayloadJSON, job.PayloadJSON)
		}

		targets, err := store.ListJobTargets(ctx, job.ID)
		if err != nil {
			t.Fatalf("ListJobTargets() error = %v", err)
		}

		if len(targets) != 1 {
			t.Fatalf("len(ListJobTargets()) = %d, want 1", len(targets))
		}
		if targets[0].ResultJSON != target.ResultJSON {
			t.Fatalf("ListJobTargets()[0].ResultJSON = %q, want %q", targets[0].ResultJSON, target.ResultJSON)
		}
	})


}
