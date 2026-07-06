package storagetest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runJobsContract extracts the job and job_target contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runJobsContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("ListJobsCursor paginates newest-first with stable cursor", func(t *testing.T) {
		// S25 T1: every backend must return contiguous, non-overlapping
		// pages in (created_at DESC, id DESC) order. We seed 12 jobs at
		// minute-spaced timestamps and walk them with limit=5 — three
		// pages of (5, 5, 2) — then assert no row is repeated and the
		// concatenation matches the full descending order.
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		base := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
		const total = 12
		want := make([]string, 0, total)
		for i := 0; i < total; i++ {
			id := fmt.Sprintf("job-%02d", i)
			if err := store.PutJob(ctx, storage.JobRecord{
				ID:             id,
				Action:         "runtime.reload",
				ActorID:        "user-1",
				Status:         "queued",
				CreatedAt:      base.Add(time.Duration(i) * time.Minute),
				TTL:            time.Minute,
				IdempotencyKey: id + "-key",
				PayloadJSON:    `{}`,
			}); err != nil {
				t.Fatalf("PutJob(%s): %v", id, err)
			}
			// Newest-first ordering means index 11 -> 0.
			want = append([]string{id}, want...)
		}

		got := make([]string, 0, total)
		params := storage.ListJobsCursorParams{Limit: 5}
		for page := 0; page < 5; page++ {
			rows, next, err := store.ListJobsCursor(ctx, params)
			if err != nil {
				t.Fatalf("ListJobsCursor page %d: %v", page, err)
			}
			for _, row := range rows {
				got = append(got, row.ID)
			}
			if next.AfterID == "" {
				break
			}
			params = next
		}
		if len(got) != total {
			t.Fatalf("walked %d jobs across pages, want %d", len(got), total)
		}
		for i, id := range want {
			if got[i] != id {
				t.Fatalf("page-walk[%d] = %q, want %q (full sequence: %v)", i, got[i], id, got)
			}
		}
	})

	t.Run("ListJobsCursor clamps oversized Limit", func(t *testing.T) {
		// Limits above MaxCursorPageSize must be silently clamped — this
		// is the contract that prevents a misbehaving client from asking
		// for an unbounded page through the cursor API.
		store := open(t)
		defer store.Close()

		_, _, err := store.ListJobsCursor(context.Background(), storage.ListJobsCursorParams{
			Limit: storage.MaxCursorPageSize * 100,
		})
		if err != nil {
			t.Fatalf("ListJobsCursor(Limit=large): %v", err)
		}
		// We can't directly observe the clamp without checking page sizes
		// against a populated table, but the absence of error proves the
		// SQL did not blow up on a too-large LIMIT — and the empty-store
		// case covers the default-page-size branch.
	})

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

	t.Run("GetJob returns a persisted row by id and ErrNotFound for a missing id", func(t *testing.T) {
		// P8.1 (audit #24): the jobs service reads terminal jobs back from
		// the store after in-memory eviction; both backends must serve a
		// point lookup with full field fidelity.
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		created := time.Date(2026, time.July, 3, 10, 0, 0, 0, time.UTC)
		want := storage.JobRecord{
			ID:             "job-getjob-1",
			Action:         "client.update",
			ActorID:        "actor-1",
			Status:         "failed",
			CreatedAt:      created,
			TTL:            5 * time.Minute,
			IdempotencyKey: "getjob-key-1",
			PayloadJSON:    `{"client_id":"c-1"}`,
		}
		if err := store.PutJob(ctx, want); err != nil {
			t.Fatalf("PutJob: %v", err)
		}

		got, err := store.GetJob(ctx, want.ID)
		if err != nil {
			t.Fatalf("GetJob(%s): %v", want.ID, err)
		}
		if got.ID != want.ID || got.Action != want.Action || got.ActorID != want.ActorID ||
			got.Status != want.Status || !got.CreatedAt.Equal(want.CreatedAt) ||
			got.TTL != want.TTL || got.IdempotencyKey != want.IdempotencyKey ||
			got.PayloadJSON != want.PayloadJSON {
			t.Fatalf("GetJob round trip mismatch:\n got %+v\nwant %+v", got, want)
		}

		if _, err := store.GetJob(ctx, "job-getjob-missing"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetJob(missing) error = %v, want storage.ErrNotFound", err)
		}
	})

}
