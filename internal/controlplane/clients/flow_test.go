package clients

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// newFlowTestService builds a repo-backed Service with wired deps + a
// call-recording job queue, ready for the mutation-flow tests.
func newFlowTestService(t *testing.T) (*Service, *fakeDeps, *fakeJobQueue, *fakeRepo) {
	t.Helper()
	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: makeTestVault(t),
	})
	deps := &fakeDeps{ttl: time.Minute}
	jq := &fakeJobQueue{enqueueJob: jobs.Job{ID: "job-1"}}
	svc.SetDeps(deps, jq, nil)
	return svc, deps, jq, repo
}

func registerAgents(deps *fakeDeps, agentIDs ...string) {
	registered := make(map[string]struct{}, len(agentIDs))
	for _, id := range agentIDs {
		registered[id] = struct{}{}
	}
	deps.topology = AgentTopology{RegisteredAgents: registered}
}

func TestCreateEmptyNameRejected(t *testing.T) {
	t.Parallel()
	svc, _, jq, _ := newFlowTestService(t)

	_, _, _, err := svc.Create(context.Background(), "actor", MutationInput{Name: "  "}, time.Now())
	if !errors.Is(err, ErrNameRequired) {
		t.Fatalf("Create empty name: err = %v, want ErrNameRequired", err)
	}
	if len(jq.enqueued) != 0 {
		t.Fatalf("Create empty name: enqueued %d jobs, want 0", len(jq.enqueued))
	}
}

func TestCreateNoTargetsRejected(t *testing.T) {
	t.Parallel()
	svc, _, jq, repo := newFlowTestService(t)
	// AgentIDs points at an agent that is NOT in the topology, so it
	// resolves to zero targets.
	_, _, _, err := svc.Create(context.Background(), "actor", MutationInput{
		Name:     "alice",
		Secret:   "0123456789abcdef0123456789abcdef",
		AgentIDs: []string{"agent-unknown"},
	}, time.Now())
	if !errors.Is(err, ErrTargetsRequired) {
		t.Fatalf("Create no targets: err = %v, want ErrTargetsRequired", err)
	}
	if len(jq.enqueued) != 0 {
		t.Fatalf("Create no targets: enqueued %d jobs, want 0", len(jq.enqueued))
	}
	if len(repo.clientsByID) != 0 {
		t.Fatalf("Create no targets: persisted %d clients, want 0", len(repo.clientsByID))
	}
}

// TestUpdateNameChangeRejected (audit F2): Telemt has no rename operation
// (PatchUserRequest has no username field), so the client name is immutable
// after create. An update carrying a different name must be rejected with
// ErrNameImmutable before anything is persisted or dispatched; re-submitting
// the current name stays a normal save.
func TestUpdateNameChangeRejected(t *testing.T) {
	t.Parallel()
	svc, deps, jq, _ := newFlowTestService(t)
	registerAgents(deps, "agent-a")

	client, _, _, err := svc.Create(context.Background(), "actor", MutationInput{
		Name:     "alice",
		Secret:   "0123456789abcdef0123456789abcdef",
		AgentIDs: []string{"agent-a"},
	}, time.Now())
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	jq.enqueued = nil

	_, _, _, err = svc.Update(context.Background(), string(client.ID), "actor", MutationInput{
		Name:     "alice-new",
		Secret:   "0123456789abcdef0123456789abcdef",
		AgentIDs: []string{"agent-a"},
	}, time.Now())
	if !errors.Is(err, ErrNameImmutable) {
		t.Fatalf("Update with name change: err = %v, want ErrNameImmutable", err)
	}
	if len(jq.enqueued) != 0 {
		t.Fatalf("Update with name change: enqueued %d jobs, want 0", len(jq.enqueued))
	}

	// Same name → regular save.
	if _, _, _, err := svc.Update(context.Background(), string(client.ID), "actor", MutationInput{
		Name:     "alice",
		Secret:   "0123456789abcdef0123456789abcdef",
		AgentIDs: []string{"agent-a"},
	}, time.Now()); err != nil {
		t.Fatalf("Update with unchanged name: err = %v, want nil", err)
	}

	// Empty name still gets the dedicated required error, not immutable.
	if _, _, _, err := svc.Update(context.Background(), string(client.ID), "actor", MutationInput{
		Name:     "   ",
		AgentIDs: []string{"agent-a"},
	}, time.Now()); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("Update with empty name: err = %v, want ErrNameRequired", err)
	}
}

func TestCreateHappyPathPersistsBeforeEnqueueAndPublishes(t *testing.T) {
	t.Parallel()
	svc, deps, jq, repo := newFlowTestService(t)
	registerAgents(deps, "agent-1")

	observedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	client, assignments, deployments, err := svc.Create(context.Background(), "actor-9", MutationInput{
		Name:              "alice",
		Secret:            "0123456789abcdef0123456789abcdef",
		UserADTag:         "0123456789abcdef0123456789abcdef",
		MaxTCPConns:       5,
		MaxUniqueIPs:      3,
		DataQuotaBytes:    1024,
		ExpirationRFC3339: "2026-06-01T00:00:00Z",
		AgentIDs:          []string{"agent-1"},
	}, observedAt)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Client persisted to the repo (SaveState) — the encrypted secret round
	// trips to plaintext through the nil test vault, so it equals the input.
	stored, ok := repo.clientsByID[client.ID]
	if !ok {
		t.Fatalf("Create: client %s not persisted to repo", client.ID)
	}
	if stored.Name != "alice" {
		t.Fatalf("persisted client name = %q, want alice", stored.Name)
	}
	if len(assignments) != 1 || assignments[0].AgentID != "agent-1" {
		t.Fatalf("assignments = %+v, want one for agent-1", assignments)
	}
	if len(deployments) != 1 || deployments[0].AgentID != "agent-1" {
		t.Fatalf("deployments = %+v, want one for agent-1", deployments)
	}

	// Exactly one create job, carrying the full client payload.
	if len(jq.enqueued) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(jq.enqueued))
	}
	if jq.enqueued[0].Action != jobs.ActionClientCreate {
		t.Fatalf("job action = %v, want ActionClientCreate", jq.enqueued[0].Action)
	}
	var payload ClientJobPayload
	if err := json.Unmarshal([]byte(jq.enqueued[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	want := ClientJobPayload{
		ClientID:          string(client.ID),
		Name:              "alice",
		Secret:            "0123456789abcdef0123456789abcdef",
		UserADTag:         "0123456789abcdef0123456789abcdef",
		Enabled:           true,
		MaxTCPConns:       5,
		MaxUniqueIPs:      3,
		DataQuotaBytes:    1024,
		ExpirationRFC3339: "2026-06-01T00:00:00Z",
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("payload = %+v, want %+v", payload, want)
	}

	// clients.updated published for this client.
	if len(deps.updatedClients) != 1 || deps.updatedClients[0] != client.ID {
		t.Fatalf("updatedClients = %+v, want [%s]", deps.updatedClients, client.ID)
	}
}

func TestCreatePersistsEvenWhenEnqueueFails(t *testing.T) {
	t.Parallel()
	svc, deps, jq, repo := newFlowTestService(t)
	registerAgents(deps, "agent-1")
	jq.enqueueErr = errors.New("enqueue boom")

	_, _, _, err := svc.Create(context.Background(), "actor", MutationInput{
		Name:     "alice",
		Secret:   "0123456789abcdef0123456789abcdef",
		AgentIDs: []string{"agent-1"},
	}, time.Now())
	if err == nil {
		t.Fatal("Create: expected enqueue error")
	}
	// The client must already be persisted: SaveState runs BEFORE Enqueue.
	if len(repo.clientsByID) != 1 {
		t.Fatalf("persisted %d clients, want 1 (SaveState must precede Enqueue)", len(repo.clientsByID))
	}
}

func TestDeleteFlowTombstonesAndEnqueuesDelete(t *testing.T) {
	t.Parallel()
	svc, deps, jq, repo := newFlowTestService(t)
	registerAgents(deps, "agent-1")

	observedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	client, _, _, err := svc.Create(context.Background(), "actor", MutationInput{
		Name:     "alice",
		Secret:   "0123456789abcdef0123456789abcdef",
		AgentIDs: []string{"agent-1"},
	}, observedAt)
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	jq.enqueued = nil // drop the create job

	if err := svc.DeleteFlow(context.Background(), string(client.ID), "actor", observedAt.Add(time.Minute)); err != nil {
		t.Fatalf("DeleteFlow: %v", err)
	}

	stored := repo.clientsByID[client.ID]
	if stored.DeletedAt == nil {
		t.Fatal("DeleteFlow: DeletedAt = nil, want tombstone set")
	}
	if stored.Enabled {
		t.Fatal("DeleteFlow: Enabled = true, want false")
	}
	if len(jq.enqueued) != 1 || jq.enqueued[0].Action != jobs.ActionClientDelete {
		t.Fatalf("DeleteFlow: enqueued = %+v, want one ActionClientDelete", jq.enqueued)
	}
}

func TestRotateSecretChangesSecretBeforeEnqueue(t *testing.T) {
	t.Parallel()
	svc, deps, jq, repo := newFlowTestService(t)
	registerAgents(deps, "agent-1")

	const oldSecret = "0123456789abcdef0123456789abcdef"
	client, _, _, err := svc.Create(context.Background(), "actor", MutationInput{
		Name:     "alice",
		Secret:   oldSecret,
		AgentIDs: []string{"agent-1"},
	}, time.Now())
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	jq.enqueued = nil

	updated, _, _, err := svc.RotateSecret(context.Background(), string(client.ID), "actor", time.Now())
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if updated.Secret == oldSecret || len(updated.Secret) != 32 {
		t.Fatalf("RotateSecret: secret = %q, want a fresh 32-hex secret", updated.Secret)
	}
	// The persisted + enqueued secret is the NEW one (persist precedes enqueue).
	if repo.clientsByID[client.ID].Secret != updated.Secret {
		t.Fatal("RotateSecret: repo still holds the old secret")
	}
	if len(jq.enqueued) != 1 {
		t.Fatalf("RotateSecret: enqueued %d jobs, want 1", len(jq.enqueued))
	}
	var payload ClientJobPayload
	if err := json.Unmarshal([]byte(jq.enqueued[0].PayloadJSON), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Secret != updated.Secret {
		t.Fatalf("enqueued payload secret = %q, want new secret %q", payload.Secret, updated.Secret)
	}
}

func TestResetQuotaUnknownTargetNotFound(t *testing.T) {
	t.Parallel()
	svc, deps, _, _ := newFlowTestService(t)
	registerAgents(deps, "agent-1")

	client, _, _, err := svc.Create(context.Background(), "actor", MutationInput{
		Name:     "alice",
		Secret:   "0123456789abcdef0123456789abcdef",
		AgentIDs: []string{"agent-1"},
	}, time.Now())
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}

	_, _, _, _, err = svc.ResetQuota(context.Background(), string(client.ID), "agent-999", "actor", time.Now())
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("ResetQuota bad target: err = %v, want storage.ErrNotFound", err)
	}
}

func TestUpdateDropsRemovedTargetsViaDeleteJob(t *testing.T) {
	t.Parallel()
	svc, deps, jq, _ := newFlowTestService(t)
	registerAgents(deps, "agent-a", "agent-b")

	client, _, _, err := svc.Create(context.Background(), "actor", MutationInput{
		Name:     "alice",
		Secret:   "0123456789abcdef0123456789abcdef",
		AgentIDs: []string{"agent-a", "agent-b"},
	}, time.Now())
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	jq.enqueued = nil

	// Drop agent-b from the target set.
	_, _, _, err = svc.Update(context.Background(), string(client.ID), "actor", MutationInput{
		Name:     "alice",
		Secret:   "0123456789abcdef0123456789abcdef",
		AgentIDs: []string{"agent-a"},
	}, time.Now())
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(jq.enqueued) != 2 {
		t.Fatalf("Update: enqueued %d jobs, want 2 (update + delete)", len(jq.enqueued))
	}
	if jq.enqueued[0].Action != jobs.ActionClientUpdate ||
		len(jq.enqueued[0].TargetAgentIDs) != 1 || jq.enqueued[0].TargetAgentIDs[0] != "agent-a" {
		t.Fatalf("first job = %+v, want update for [agent-a]", jq.enqueued[0])
	}
	if jq.enqueued[1].Action != jobs.ActionClientDelete ||
		len(jq.enqueued[1].TargetAgentIDs) != 1 || jq.enqueued[1].TargetAgentIDs[0] != "agent-b" {
		t.Fatalf("second job = %+v, want delete for [agent-b]", jq.enqueued[1])
	}
}
