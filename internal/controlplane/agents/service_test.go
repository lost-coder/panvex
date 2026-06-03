package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// fakeRepo is a minimal in-memory agents.Repository for Service tests.
// ListAgents returns the seeded rows; PutAgent upserts by ID. failOn
// injects a specific failure keyed by method name.
type fakeRepo struct {
	agentsByID map[string]storage.AgentRecord
	failOn     string
	listErr    error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{agentsByID: make(map[string]storage.AgentRecord)}
}

func (r *fakeRepo) ListAgents(_ context.Context) ([]storage.AgentRecord, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	out := make([]storage.AgentRecord, 0, len(r.agentsByID))
	for _, a := range r.agentsByID {
		out = append(out, a)
	}
	return out, nil
}

func (r *fakeRepo) PutAgent(_ context.Context, a storage.AgentRecord) error {
	if r.failOn == "PutAgent" {
		return errors.New("fakeRepo: PutAgent: injected failure")
	}
	r.agentsByID[a.ID] = a
	return nil
}

func fixedNow() func() time.Time {
	t := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func TestServiceRestorePopulatesMirror(t *testing.T) {
	repo := newFakeRepo()
	repo.agentsByID["agent-1"] = storage.AgentRecord{ID: "agent-1", NodeName: "alpha", Version: "1.2.3"}
	repo.agentsByID["agent-2"] = storage.AgentRecord{ID: "agent-2", NodeName: "beta", ReadOnly: true}

	svc := NewService(repo, fixedNow())
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got := svc.List()
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2", len(got))
	}

	a1, ok := svc.Get("agent-1")
	if !ok {
		t.Fatalf("Get(agent-1): not found")
	}
	if a1.NodeName != "alpha" || a1.Version != "1.2.3" {
		t.Fatalf("Get(agent-1) = %+v, want NodeName=alpha Version=1.2.3", a1)
	}

	a2, ok := svc.Get("agent-2")
	if !ok || !a2.ReadOnly {
		t.Fatalf("Get(agent-2) = %+v ok=%v, want ReadOnly=true", a2, ok)
	}
}

func TestServiceRestorePropagatesListError(t *testing.T) {
	repo := newFakeRepo()
	repo.listErr = errors.New("boom")
	svc := NewService(repo, fixedNow())
	if err := svc.Restore(context.Background()); err == nil {
		t.Fatalf("Restore: want error, got nil")
	}
}

func TestServiceRestoreIsFullSnapshot(t *testing.T) {
	repo := newFakeRepo()
	repo.agentsByID["agent-1"] = storage.AgentRecord{ID: "agent-1", NodeName: "alpha"}
	svc := NewService(repo, fixedNow())
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	// Replace the repo contents and Restore again: the mirror must reflect
	// only the new snapshot, not a union.
	delete(repo.agentsByID, "agent-1")
	repo.agentsByID["agent-9"] = storage.AgentRecord{ID: "agent-9", NodeName: "zeta"}
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore (2): %v", err)
	}
	if _, ok := svc.Get("agent-1"); ok {
		t.Fatalf("agent-1 still present after re-Restore; Restore is not a full snapshot")
	}
	if _, ok := svc.Get("agent-9"); !ok {
		t.Fatalf("agent-9 missing after re-Restore")
	}
}

func TestServiceUpsertIdentityWritesThroughAndMirrors(t *testing.T) {
	repo := newFakeRepo()
	svc := NewService(repo, fixedNow())

	rec := storage.AgentRecord{ID: "agent-1", NodeName: "alpha", Version: "1.0.0"}
	if err := svc.UpsertIdentity(context.Background(), rec); err != nil {
		t.Fatalf("UpsertIdentity: %v", err)
	}

	// Persisted to the repo.
	if got, ok := repo.agentsByID["agent-1"]; !ok || got.NodeName != "alpha" {
		t.Fatalf("repo missing agent-1 after UpsertIdentity: %+v ok=%v", got, ok)
	}
	// Reflected in the mirror.
	mirrored, ok := svc.Get("agent-1")
	if !ok || mirrored.Version != "1.0.0" {
		t.Fatalf("mirror Get(agent-1) = %+v ok=%v, want Version=1.0.0", mirrored, ok)
	}

	// Update overwrites in place.
	rec.Version = "2.0.0"
	if err := svc.UpsertIdentity(context.Background(), rec); err != nil {
		t.Fatalf("UpsertIdentity (update): %v", err)
	}
	mirrored, _ = svc.Get("agent-1")
	if mirrored.Version != "2.0.0" {
		t.Fatalf("mirror Version = %q, want 2.0.0", mirrored.Version)
	}
}

func TestServiceUpsertIdentityRepoErrorLeavesMirrorUnchanged(t *testing.T) {
	repo := newFakeRepo()
	repo.failOn = "PutAgent"
	svc := NewService(repo, fixedNow())

	rec := storage.AgentRecord{ID: "agent-1", NodeName: "alpha"}
	if err := svc.UpsertIdentity(context.Background(), rec); err == nil {
		t.Fatalf("UpsertIdentity: want error from repo, got nil")
	}
	if _, ok := svc.Get("agent-1"); ok {
		t.Fatalf("mirror updated despite repo write failure")
	}
}

func TestServiceRemoveEvictsFromMirror(t *testing.T) {
	repo := newFakeRepo()
	repo.agentsByID["agent-1"] = storage.AgentRecord{ID: "agent-1", NodeName: "alpha"}
	svc := NewService(repo, fixedNow())
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	svc.Remove("agent-1")
	if _, ok := svc.Get("agent-1"); ok {
		t.Fatalf("agent-1 still in mirror after Remove")
	}
	if len(svc.List()) != 0 {
		t.Fatalf("List not empty after Remove")
	}
}

func TestServiceListReturnsDeepCopies(t *testing.T) {
	repo := newFakeRepo()
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	repo.agentsByID["agent-1"] = storage.AgentRecord{
		ID:             "agent-1",
		NodeName:       "alpha",
		CertIssuedAt:   &issued,
		CertSPKISHA256: []byte{1, 2, 3},
	}
	svc := NewService(repo, fixedNow())
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got := svc.List()
	if len(got) != 1 {
		t.Fatalf("List len = %d, want 1", len(got))
	}
	// Mutating the returned copy must not affect the mirror.
	got[0].NodeName = "MUTATED"
	got[0].CertSPKISHA256[0] = 0xff
	if got[0].CertIssuedAt != nil {
		*got[0].CertIssuedAt = time.Time{}
	}

	fresh, _ := svc.Get("agent-1")
	if fresh.NodeName != "alpha" {
		t.Fatalf("mirror NodeName mutated to %q via returned copy", fresh.NodeName)
	}
	if fresh.CertSPKISHA256[0] != 1 {
		t.Fatalf("mirror CertSPKISHA256 mutated via returned slice")
	}
	if fresh.CertIssuedAt == nil || !fresh.CertIssuedAt.Equal(issued) {
		t.Fatalf("mirror CertIssuedAt mutated via returned pointer: %v", fresh.CertIssuedAt)
	}
}

func TestServiceGetMissingReturnsFalse(t *testing.T) {
	svc := NewService(newFakeRepo(), fixedNow())
	if _, ok := svc.Get("nope"); ok {
		t.Fatalf("Get(nope): want ok=false")
	}
}
