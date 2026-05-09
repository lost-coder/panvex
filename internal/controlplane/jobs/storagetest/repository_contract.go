// internal/controlplane/jobs/storagetest/repository_contract.go
//
// RunContract exercises any jobs.Repository implementation. Backends
// invoke this from their own *_test.go to verify they meet the contract.
package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// OpenRepo is a factory that creates a fresh, empty Repository for a single
// sub-test. Each sub-test receives its own instance so state does not leak.
type OpenRepo func(t *testing.T) jobs.Repository

// RunContract runs all repository contract sub-tests against the given
// OpenRepo factory. Backends call this once from their *_test.go files.
func RunContract(t *testing.T, open OpenRepo) {
	t.Helper()
	t.Run("PutRoundTrip", func(t *testing.T) { runPutRoundTrip(t, open(t)) })
	t.Run("PutUpsert", func(t *testing.T) { runPutUpsert(t, open(t)) })
	t.Run("PutNilPayload", func(t *testing.T) { runPutNilPayload(t, open(t)) })
}

func runPutRoundTrip(t *testing.T, repo jobs.Repository) {
	t.Helper()
	ctx := context.Background()
	j := jobs.Job{
		ID:             "job-rt-1",
		Action:         jobs.Action("client.create"),
		IdempotencyKey: "idem-rt-1",
		ActorID:        "user-1",
		Status:         jobs.Status("queued"),
		CreatedAt:      time.Unix(1700000000, 0).UTC(),
		TTL:            time.Hour,
		PayloadJSON:    `{"key":"value"}`,
	}
	if err := repo.Put(ctx, j); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func runPutUpsert(t *testing.T, repo jobs.Repository) {
	t.Helper()
	ctx := context.Background()
	j := jobs.Job{
		ID:             "job-upsert-1",
		Action:         jobs.Action("client.create"),
		IdempotencyKey: "idem-upsert-1",
		ActorID:        "user-1",
		Status:         jobs.Status("queued"),
		CreatedAt:      time.Unix(1700000000, 0).UTC(),
		TTL:            time.Hour,
		PayloadJSON:    `{"v":1}`,
	}
	if err := repo.Put(ctx, j); err != nil {
		t.Fatalf("Put initial: %v", err)
	}
	// Update: change status to "running" — must not fail.
	j.Status = jobs.Status("running")
	j.PayloadJSON = `{"v":2}`
	if err := repo.Put(ctx, j); err != nil {
		t.Fatalf("Put upsert: %v", err)
	}
}

func runPutNilPayload(t *testing.T, repo jobs.Repository) {
	t.Helper()
	ctx := context.Background()
	j := jobs.Job{
		ID:             "job-nil-1",
		Action:         jobs.Action("runtime.reload"),
		IdempotencyKey: "idem-nil-1",
		ActorID:        "user-2",
		Status:         jobs.Status("queued"),
		CreatedAt:      time.Unix(1700000001, 0).UTC(),
		TTL:            30 * time.Minute,
		PayloadJSON:    "",
	}
	if err := repo.Put(ctx, j); err != nil {
		t.Fatalf("Put with empty payload: %v", err)
	}
}
