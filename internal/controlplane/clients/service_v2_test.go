package clients

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeRepo is a minimal in-memory clients.Repository for Service tests.
// Only the methods exercised by Service.Restore are implemented;
// unimplemented methods return a sentinel error.
type fakeRepo struct {
	clientsByID         map[ClientID]Client
	assignmentsByClient map[ClientID][]Assignment
	deploymentsByClient map[ClientID][]Deployment
	usageRows           []Usage
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		clientsByID:         make(map[ClientID]Client),
		assignmentsByClient: make(map[ClientID][]Assignment),
		deploymentsByClient: make(map[ClientID][]Deployment),
	}
}

// --- Repository interface implementation ---

func (r *fakeRepo) Get(_ context.Context, id ClientID) (Client, error) {
	c, ok := r.clientsByID[id]
	if !ok {
		return Client{}, errors.New("fakeRepo: Get: not found")
	}
	return c, nil
}

func (r *fakeRepo) List(_ context.Context) ([]Client, error) {
	out := make([]Client, 0, len(r.clientsByID))
	for _, c := range r.clientsByID {
		out = append(out, c)
	}
	return out, nil
}

func (r *fakeRepo) Save(_ context.Context, _ Client) error {
	return errors.New("fakeRepo: Save: not implemented")
}

func (r *fakeRepo) Delete(_ context.Context, _ ClientID) error {
	return errors.New("fakeRepo: Delete: not implemented")
}

func (r *fakeRepo) ListAssignments(_ context.Context, clientID ClientID) ([]Assignment, error) {
	return append([]Assignment(nil), r.assignmentsByClient[clientID]...), nil
}

func (r *fakeRepo) SaveAssignments(_ context.Context, _ ClientID, _ []Assignment) error {
	return errors.New("fakeRepo: SaveAssignments: not implemented")
}

func (r *fakeRepo) DeleteAssignments(_ context.Context, _ ClientID) error {
	return errors.New("fakeRepo: DeleteAssignments: not implemented")
}

func (r *fakeRepo) ListDeployments(_ context.Context, clientID ClientID) ([]Deployment, error) {
	return append([]Deployment(nil), r.deploymentsByClient[clientID]...), nil
}

func (r *fakeRepo) SaveDeployments(_ context.Context, _ ClientID, _ []Deployment) error {
	return errors.New("fakeRepo: SaveDeployments: not implemented")
}

func (r *fakeRepo) UpsertUsage(_ context.Context, _ Usage) error {
	return errors.New("fakeRepo: UpsertUsage: not implemented")
}

func (r *fakeRepo) UpsertUsageBulk(_ context.Context, _ []Usage) error {
	return errors.New("fakeRepo: UpsertUsageBulk: not implemented")
}

func (r *fakeRepo) ListUsage(_ context.Context) ([]Usage, error) {
	return append([]Usage(nil), r.usageRows...), nil
}

func (r *fakeRepo) DeleteUsageByClient(_ context.Context, _ ClientID) error {
	return errors.New("fakeRepo: DeleteUsageByClient: not implemented")
}

// --- Phase 6.2 tests: Service.Restore ---

func TestService_Restore_PopulatesMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.clientsByID["c-1"] = Client{ID: "c-1", Name: "one"}
	repo.clientsByID["c-2"] = Client{ID: "c-2", Name: "two"}
	repo.assignmentsByClient["c-1"] = []Assignment{{ID: "a-1", ClientID: "c-1"}}
	repo.usageRows = []Usage{
		{
			ClientID:         "c-1",
			AgentID:          "agent-1",
			TrafficUsedBytes: 1024,
			LastSeq:          5,
			ObservedAt:       time.Now(),
		},
	}

	svc := NewServiceV2(ServiceConfig{Repo: repo})
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Inspect mirror directly (Get/List added in Phase 6.3).
	svc.mu.RLock()
	defer svc.mu.RUnlock()

	if len(svc.mirrorClients) != 2 {
		t.Fatalf("mirrorClients len = %d, want 2", len(svc.mirrorClients))
	}
	c1, ok := svc.mirrorClients["c-1"]
	if !ok || c1.Name != "one" {
		t.Fatalf("mirrorClients[c-1] = %+v, want {Name:one}", c1)
	}

	assigns := svc.mirrorAssignments["c-1"]
	if len(assigns) != 1 || assigns[0].ID != "a-1" {
		t.Fatalf("mirrorAssignments[c-1] = %v, want [{a-1 ...}]", assigns)
	}

	um := svc.mirrorUsage["c-1"]["agent-1"]
	if um.TrafficUsedBytes != 1024 {
		t.Fatalf("usageMirror.TrafficUsedBytes = %d, want 1024", um.TrafficUsedBytes)
	}
	if svc.mirrorLastUsageSeq["agent-1"] != 5 {
		t.Fatalf("mirrorLastUsageSeq[agent-1] = %d, want 5", svc.mirrorLastUsageSeq["agent-1"])
	}
}

func TestService_Restore_NoRepo(t *testing.T) {
	t.Parallel()

	svc := NewServiceV2(ServiceConfig{})
	err := svc.Restore(context.Background())
	if err == nil {
		t.Fatal("Restore with nil repo: expected error, got nil")
	}
}

func TestService_Restore_Idempotent(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.clientsByID["c-1"] = Client{ID: "c-1", Name: "one"}

	svc := NewServiceV2(ServiceConfig{Repo: repo})
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore (1st): %v", err)
	}

	// Replace c-1 with c-2 in repo.
	delete(repo.clientsByID, "c-1")
	repo.clientsByID["c-2"] = Client{ID: "c-2", Name: "two"}

	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore (2nd): %v", err)
	}

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	if len(svc.mirrorClients) != 1 {
		t.Fatalf("after 2nd Restore: mirrorClients len = %d, want 1", len(svc.mirrorClients))
	}
	if _, ok := svc.mirrorClients["c-1"]; ok {
		t.Fatal("after 2nd Restore: c-1 still in mirror, want gone")
	}
	if _, ok := svc.mirrorClients["c-2"]; !ok {
		t.Fatal("after 2nd Restore: c-2 missing from mirror")
	}
}

// --- Phase 6.3 tests: Service.Get + Service.List ---

func TestService_Get_NotFound(t *testing.T) {
	t.Parallel()

	svc := NewServiceV2(ServiceConfig{Repo: newFakeRepo()})
	_, err := svc.Get(context.Background(), ClientID("missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestService_GetList_FromMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.clientsByID["c-1"] = Client{ID: "c-1", Name: "alpha"}

	svc := NewServiceV2(ServiceConfig{Repo: repo})
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	got, err := svc.Get(context.Background(), ClientID("c-1"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "alpha" {
		t.Fatalf("name = %q, want alpha", got.Name)
	}

	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len = %d, want 1", len(list))
	}
}

func TestService_Get_BeforeRestore_Empty(t *testing.T) {
	t.Parallel()

	// Without calling Restore, mirror is empty — Get must return ErrNotFound.
	svc := NewServiceV2(ServiceConfig{Repo: newFakeRepo()})
	_, err := svc.Get(context.Background(), "c-1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before Restore: err = %v, want ErrNotFound", err)
	}
	list, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List before Restore: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("List before Restore: len = %d, want 0", len(list))
	}
}
