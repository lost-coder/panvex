package jobs

import (
	"testing"
	"time"
)

func TestServiceEnqueueRejectsDuplicateIdempotencyKey(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	first, err := service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "same-key",
		ActorID:        "user-1",
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() first error = %v", err)
	}

	if first.Status != StatusQueued {
		t.Fatalf("first.Status = %q, want %q", first.Status, StatusQueued)
	}

	_, err = service.Enqueue(CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "same-key",
		ActorID:        "user-1",
	}, now.Add(10*time.Second))
	if err == nil {
		t.Fatal("Enqueue() duplicate error = nil, want idempotency failure")
	}

	if err != ErrDuplicateIdempotencyKey {
		t.Fatalf("Enqueue() duplicate error = %v, want %v", err, ErrDuplicateIdempotencyKey)
	}
}

func TestServiceEnqueueRejectsMutatingActionForReadOnlyTarget(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	service := NewService()

	_, err := service.Enqueue(CreateJobInput{
		Action:         ActionUsersCreate,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "create-user",
		ActorID:        "user-1",
		ReadOnlyAgents: map[string]bool{
			"agent-1": true,
		},
	}, now)
	if err == nil {
		t.Fatal("Enqueue() error = nil, want read-only failure")
	}

	if err != ErrReadOnlyTarget {
		t.Fatalf("Enqueue() error = %v, want %v", err, ErrReadOnlyTarget)
	}
}
