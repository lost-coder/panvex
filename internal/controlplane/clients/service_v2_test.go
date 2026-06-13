package clients

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// fakeRepo is a minimal in-memory clients.Repository for Service tests.
// Methods needed for Restore are implemented; others return a sentinel
// error unless failOn is set to inject a specific failure.
type fakeRepo struct {
	clientsByID         map[ClientID]Client
	assignmentsByClient map[ClientID][]Assignment
	deploymentsByClient map[ClientID][]Deployment
	usage               []Usage
	// failOn is the method name to fail on (e.g. "SaveAssignments").
	failOn string
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

func (r *fakeRepo) GetBySubscriptionToken(_ context.Context, token string) (Client, error) {
	if token == "" {
		return Client{}, storage.ErrNotFound
	}
	for _, c := range r.clientsByID {
		if c.SubscriptionToken == token {
			return c, nil
		}
	}
	return Client{}, storage.ErrNotFound
}

func (r *fakeRepo) List(_ context.Context) ([]Client, error) {
	out := make([]Client, 0, len(r.clientsByID))
	for _, c := range r.clientsByID {
		out = append(out, c)
	}
	return out, nil
}

func (r *fakeRepo) Save(_ context.Context, c Client) error {
	if r.failOn == "Save" {
		return errors.New("fakeRepo: Save: injected failure")
	}
	r.clientsByID[c.ID] = c
	return nil
}

func (r *fakeRepo) Delete(_ context.Context, id ClientID) error {
	if r.failOn == "Delete" {
		return errors.New("fakeRepo: Delete: injected failure")
	}
	delete(r.clientsByID, id)
	return nil
}

func (r *fakeRepo) ListAssignments(_ context.Context, clientID ClientID) ([]Assignment, error) {
	return append([]Assignment(nil), r.assignmentsByClient[clientID]...), nil
}

func (r *fakeRepo) SaveAssignments(_ context.Context, clientID ClientID, assignments []Assignment) error {
	if r.failOn == "SaveAssignments" {
		return errors.New("fakeRepo: SaveAssignments: injected failure")
	}
	r.assignmentsByClient[clientID] = append([]Assignment(nil), assignments...)
	return nil
}

func (r *fakeRepo) DeleteAssignments(_ context.Context, _ ClientID) error {
	return errors.New("fakeRepo: DeleteAssignments: not implemented")
}

func (r *fakeRepo) ListDeployments(_ context.Context, clientID ClientID) ([]Deployment, error) {
	return append([]Deployment(nil), r.deploymentsByClient[clientID]...), nil
}

func (r *fakeRepo) SaveDeployments(_ context.Context, clientID ClientID, deployments []Deployment) error {
	if r.failOn == "SaveDeployments" {
		return errors.New("fakeRepo: SaveDeployments: injected failure")
	}
	r.deploymentsByClient[clientID] = append([]Deployment(nil), deployments...)
	return nil
}

func (r *fakeRepo) PutDeployment(_ context.Context, d Deployment) error {
	if r.failOn == "PutDeployment" {
		return errors.New("fakeRepo: PutDeployment: injected failure")
	}
	existing := r.deploymentsByClient[d.ClientID]
	for i, e := range existing {
		if e.AgentID == d.AgentID {
			existing[i] = d
			r.deploymentsByClient[d.ClientID] = existing
			return nil
		}
	}
	r.deploymentsByClient[d.ClientID] = append(existing, d)
	return nil
}

func (r *fakeRepo) UpsertUsage(_ context.Context, u Usage) error {
	if r.failOn == "UpsertUsage" {
		return errors.New("fakeRepo: UpsertUsage: injected failure")
	}
	// last-write-wins by (clientID, agentID)
	for i, row := range r.usage {
		if row.ClientID == u.ClientID && row.AgentID == u.AgentID {
			r.usage[i] = u
			return nil
		}
	}
	r.usage = append(r.usage, u)
	return nil
}

func (r *fakeRepo) UpsertUsageBulk(ctx context.Context, batch []Usage) error {
	if r.failOn == "UpsertUsageBulk" {
		return errors.New("fakeRepo: UpsertUsageBulk: injected failure")
	}
	for _, u := range batch {
		_ = r.UpsertUsage(ctx, u)
	}
	return nil
}

func (r *fakeRepo) ListUsage(_ context.Context) ([]Usage, error) {
	return append([]Usage(nil), r.usage...), nil
}

func (r *fakeRepo) DeleteUsageByClient(_ context.Context, _ ClientID) error {
	return errors.New("fakeRepo: DeleteUsageByClient: not implemented")
}

// --- fakeRepoSet: implements ClientsRepoSet ---

type fakeRepoSet struct {
	clients    Repository
	discovered discovered.Repository
}

func (r *fakeRepoSet) Clients() Repository               { return r.clients }
func (r *fakeRepoSet) Discovered() discovered.Repository { return r.discovered }

// --- fakeUoW: implements ServiceUoW ---

type fakeUoW struct {
	rs ClientsRepoSet
}

func newFakeUoW(rs ClientsRepoSet) *fakeUoW { return &fakeUoW{rs: rs} }

func (u *fakeUoW) Do(_ context.Context, fn func(rs ClientsRepoSet) error) error {
	return fn(u.rs)
}

// --- fakeDiscoveredRepo: implements discovered.Repository ---

type fakeDiscoveredRepo struct {
	byID map[discovered.DiscoveredID]discovered.DiscoveredClient
}

func newFakeDiscoveredRepo() *fakeDiscoveredRepo {
	return &fakeDiscoveredRepo{byID: make(map[discovered.DiscoveredID]discovered.DiscoveredClient)}
}

func (r *fakeDiscoveredRepo) Get(_ context.Context, id discovered.DiscoveredID) (discovered.DiscoveredClient, error) {
	dc, ok := r.byID[id]
	if !ok {
		return discovered.DiscoveredClient{}, errors.New("fakeDiscoveredRepo: Get: not found")
	}
	return dc, nil
}

func (r *fakeDiscoveredRepo) GetByAgentAndName(_ context.Context, _, _ string) (discovered.DiscoveredClient, error) {
	return discovered.DiscoveredClient{}, errors.New("fakeDiscoveredRepo: GetByAgentAndName: not implemented")
}

func (r *fakeDiscoveredRepo) List(_ context.Context) ([]discovered.DiscoveredClient, error) {
	out := make([]discovered.DiscoveredClient, 0, len(r.byID))
	for _, dc := range r.byID {
		out = append(out, dc)
	}
	return out, nil
}

func (r *fakeDiscoveredRepo) ListByAgent(_ context.Context, _ string) ([]discovered.DiscoveredClient, error) {
	return nil, errors.New("fakeDiscoveredRepo: ListByAgent: not implemented")
}

func (r *fakeDiscoveredRepo) Save(_ context.Context, dc discovered.DiscoveredClient) error {
	r.byID[dc.ID] = dc
	return nil
}

func (r *fakeDiscoveredRepo) UpdateStatus(_ context.Context, id discovered.DiscoveredID, status discovered.Status, _ time.Time) error {
	dc, ok := r.byID[id]
	if !ok {
		return errors.New("fakeDiscoveredRepo: UpdateStatus: not found")
	}
	dc.Status = status
	r.byID[id] = dc
	return nil
}

func (r *fakeDiscoveredRepo) UpdateStatusBulk(_ context.Context, ids []discovered.DiscoveredID, status discovered.Status, _ time.Time) error {
	for _, id := range ids {
		dc, ok := r.byID[id]
		if !ok {
			continue
		}
		dc.Status = status
		r.byID[id] = dc
	}
	return nil
}

func (r *fakeDiscoveredRepo) Delete(_ context.Context, id discovered.DiscoveredID) error {
	delete(r.byID, id)
	return nil
}

// --- makeTestVault: returns a no-op vault for tests ---

// makeTestVault returns a nil vault, which causes encryptSecret to
// pass plaintext through unchanged. Tests that want real encryption
// can construct a secretvault.Vault directly.
func makeTestVault(_ *testing.T) *secretvault.Vault {
	return nil
}

// --- Phase 6.2 tests: Service.Restore ---

func TestService_Restore_PopulatesMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.clientsByID["c-1"] = Client{ID: "c-1", Name: "one"}
	repo.clientsByID["c-2"] = Client{ID: "c-2", Name: "two"}
	repo.assignmentsByClient["c-1"] = []Assignment{{ID: "a-1", ClientID: "c-1"}}
	repo.usage = []Usage{
		{
			ClientID:         "c-1",
			AgentID:          "agent-1",
			TrafficUsedBytes: 1024,
			LastSeq:          5,
			ObservedAt:       time.Now(),
		},
	}

	svc := NewService(ServiceConfig{Repo: repo})
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

func TestService_Restore_Idempotent(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.clientsByID["c-1"] = Client{ID: "c-1", Name: "one"}

	svc := NewService(ServiceConfig{Repo: repo})
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

	svc := NewService(ServiceConfig{Repo: newFakeRepo()})
	_, err := svc.Get(context.Background(), ClientID("missing"))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestService_GetList_FromMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.clientsByID["c-1"] = Client{ID: "c-1", Name: "alpha"}

	svc := NewService(ServiceConfig{Repo: repo})
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
	svc := NewService(ServiceConfig{Repo: newFakeRepo()})
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

// --- Phase 6.4 tests: Service.Save ---

func TestService_Save_EncryptsAndPersists(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: makeTestVault(t),
	})

	plain := "plaintext-secret"
	c := Client{ID: ClientID("c-new"), Name: "n", Secret: plain}
	if err := svc.Save(context.Background(), c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// With nil vault, plaintext passes through — the important thing is
	// the repo was called and the mirror is updated.
	stored, ok := repo.clientsByID[c.ID]
	if !ok {
		t.Fatal("client not written to repo")
	}
	_ = stored // vault is nil, so Secret passes through unchanged in this test

	got, err := svc.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Secret != plain {
		t.Fatalf("mirror Secret = %q, want plaintext %q", got.Secret, plain)
	}
}

func TestService_Save_VaultEncryptsSecret(t *testing.T) {
	t.Parallel()

	vault, err := secretvault.New("test-passphrase-32bytes-long-ok!", secretvault.AllDomains)
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: vault,
	})

	plain := "deadbeefdeadbeefdeadbeefdeadbeef"
	c := Client{ID: ClientID("c-enc"), Name: "enc", Secret: plain}
	if err := svc.Save(context.Background(), c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	stored := repo.clientsByID[c.ID]
	if stored.Secret == plain {
		t.Fatal("Secret stored as plaintext — encryption boundary violated")
	}
	// Mirror must hold plaintext.
	got, err := svc.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Secret != plain {
		t.Fatalf("mirror Secret = %q, want plaintext %q", got.Secret, plain)
	}
}

// --- Phase 6.5 tests: Service.SaveState ---

func TestService_SaveState_AtomicAcrossThreeWrites(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.failOn = "SaveAssignments"
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: makeTestVault(t),
	})

	c := Client{ID: ClientID("c-1"), Name: "n", Secret: "s"}
	a := Assignment{ID: AssignmentID("a-1"), ClientID: c.ID}

	err := svc.SaveState(context.Background(), c, []Assignment{a}, nil)
	if err == nil {
		t.Fatal("expected failure from SaveAssignments injection")
	}
	// Mirror must NOT have c-1 (Tx rolled back).
	if _, getErr := svc.Get(context.Background(), c.ID); !errors.Is(getErr, ErrNotFound) {
		t.Fatal("mirror updated despite Tx rollback")
	}
}

func TestService_SaveState_UpdatesMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: makeTestVault(t),
	})

	c := Client{ID: ClientID("c-1"), Name: "n", Secret: "s"}
	a := Assignment{ID: AssignmentID("a-1"), ClientID: c.ID}
	d := Deployment{ClientID: c.ID, AgentID: "a-1"}

	if err := svc.SaveState(context.Background(), c, []Assignment{a}, []Deployment{d}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := svc.Get(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "n" {
		t.Fatal("mirror not updated")
	}
}

// --- Phase 6.7 tests: Service.Delete ---

func TestService_Delete_RemovesFromMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.clientsByID[ClientID("c-del")] = Client{ID: ClientID("c-del"), Name: "del"}
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: makeTestVault(t),
	})
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if err := svc.Delete(context.Background(), ClientID("c-del")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(context.Background(), ClientID("c-del")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: err = %v, want ErrNotFound", err)
	}
}

func TestService_Delete_PropagatesRepoError(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.failOn = "Delete"
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: makeTestVault(t),
	})
	if err := svc.Delete(context.Background(), ClientID("c-x")); err == nil {
		t.Fatal("expected error from injected Delete failure")
	}
}

// --- Phase 6.8 tests: Service.UpsertUsage / UpsertUsageBulk ---

func TestService_UpsertUsageBulk_PersistsAndUpdatesMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(ServiceConfig{Repo: repo})
	batch := []Usage{
		{ClientID: ClientID("c-1"), AgentID: "a-1", TrafficUsedBytes: 100, LastSeq: 5},
		{ClientID: ClientID("c-2"), AgentID: "a-1", TrafficUsedBytes: 200, LastSeq: 6},
	}
	if err := svc.UpsertUsageBulk(context.Background(), batch); err != nil {
		t.Fatalf("UpsertUsageBulk: %v", err)
	}
	if len(repo.usage) != 2 {
		t.Fatalf("repo.usage = %d, want 2", len(repo.usage))
	}
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	if svc.mirrorUsage[ClientID("c-1")]["a-1"].TrafficUsedBytes != 100 {
		t.Fatal("mirror usage c-1/a-1 not updated")
	}
	if svc.mirrorLastUsageSeq["a-1"] != 6 {
		t.Fatalf("lastUsageSeq[a-1] = %d, want 6", svc.mirrorLastUsageSeq["a-1"])
	}
}

func TestService_UpsertUsageBulk_EmptySlice(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(ServiceConfig{Repo: repo})
	if err := svc.UpsertUsageBulk(context.Background(), nil); err != nil {
		t.Fatalf("empty bulk: %v", err)
	}
	if len(repo.usage) != 0 {
		t.Fatal("repo.usage non-empty after empty bulk")
	}
}

func TestService_UpsertUsage_Single(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(ServiceConfig{Repo: repo})
	u := Usage{ClientID: ClientID("c-x"), AgentID: "a-x", TrafficUsedBytes: 42, LastSeq: 1}
	if err := svc.UpsertUsage(context.Background(), u); err != nil {
		t.Fatalf("UpsertUsage: %v", err)
	}
	if len(repo.usage) != 1 || repo.usage[0].TrafficUsedBytes != 42 {
		t.Fatalf("repo.usage = %+v", repo.usage)
	}
}

// --- C1 follow-up: mirror must update unconditionally on DB-persist failure ---
//
// Client usage totals are cumulative absolutes and the seq cursor advances
// unconditionally (server.shouldApplyClientUsageDelta -> MirrorSetLastUsageSeq)
// before the persist call. If the mirror total is gated on DB success, a
// failed persist leaves the cursor advanced but the total stale, permanently
// dropping the failed delta's bytes from the running total. The mirror (live
// accumulator) must therefore be updated whether or not the DB write succeeds,
// while the DB error is still propagated to the caller (which alerts on
// client_usage_persist_failed).

func TestService_UpsertUsageBulk_PersistFailure_StillUpdatesMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.failOn = "UpsertUsageBulk"
	svc := NewService(ServiceConfig{Repo: repo})
	batch := []Usage{
		{ClientID: ClientID("c-1"), AgentID: "a-1", TrafficUsedBytes: 100, LastSeq: 5},
	}

	err := svc.UpsertUsageBulk(context.Background(), batch)
	if err == nil {
		t.Fatal("expected DB persist error to be propagated, got nil")
	}

	snap := svc.MirrorSnapshot()
	got, ok := snap.Usage[ClientID("c-1")]["a-1"]
	if !ok {
		t.Fatal("mirror usage c-1/a-1 missing after failed persist (mirror not updated)")
	}
	if got.TrafficUsedBytes != 100 {
		t.Fatalf("mirror TrafficUsedBytes = %d, want 100 despite DB error", got.TrafficUsedBytes)
	}
	if snap.LastUsageSeq["a-1"] != 5 {
		t.Fatalf("mirror LastUsageSeq[a-1] = %d, want 5", snap.LastUsageSeq["a-1"])
	}
}

func TestService_UpsertUsage_PersistFailure_StillUpdatesMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.failOn = "UpsertUsage"
	svc := NewService(ServiceConfig{Repo: repo})
	u := Usage{ClientID: ClientID("c-x"), AgentID: "a-x", TrafficUsedBytes: 42, LastSeq: 3}

	if err := svc.UpsertUsage(context.Background(), u); err == nil {
		t.Fatal("expected DB persist error to be propagated, got nil")
	}

	snap := svc.MirrorSnapshot()
	got, ok := snap.Usage[ClientID("c-x")]["a-x"]
	if !ok {
		t.Fatal("mirror usage c-x/a-x missing after failed persist (mirror not updated)")
	}
	if got.TrafficUsedBytes != 42 {
		t.Fatalf("mirror TrafficUsedBytes = %d, want 42 despite DB error", got.TrafficUsedBytes)
	}
	if snap.LastUsageSeq["a-x"] != 3 {
		t.Fatalf("mirror LastUsageSeq[a-x] = %d, want 3", snap.LastUsageSeq["a-x"])
	}
}

// A failed delta followed by a successful in-order delta must leave the mirror
// total reflecting BOTH deltas (cumulative absolutes), proving no permanent
// byte drop. delta2 carries the new running total (300) — the same absolute
// the agent would send next regardless of the earlier persist outcome.
func TestService_UpsertUsageBulk_FailThenOK_NoPermanentDrop(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(ServiceConfig{Repo: repo})
	ctx := context.Background()

	repo.failOn = "UpsertUsageBulk"
	delta1 := []Usage{{ClientID: ClientID("c-1"), AgentID: "a-1", TrafficUsedBytes: 100, LastSeq: 1}}
	if err := svc.UpsertUsageBulk(ctx, delta1); err == nil {
		t.Fatal("expected DB error on delta1, got nil")
	}

	repo.failOn = ""
	delta2 := []Usage{{ClientID: ClientID("c-1"), AgentID: "a-1", TrafficUsedBytes: 300, LastSeq: 2}}
	if err := svc.UpsertUsageBulk(ctx, delta2); err != nil {
		t.Fatalf("delta2 persist: %v", err)
	}

	snap := svc.MirrorSnapshot()
	if got := snap.Usage[ClientID("c-1")]["a-1"].TrafficUsedBytes; got != 300 {
		t.Fatalf("mirror TrafficUsedBytes = %d, want 300 (both deltas reflected)", got)
	}
	if got := repo.usage[0].TrafficUsedBytes; got != 300 {
		t.Fatalf("repo TrafficUsedBytes = %d, want 300 (cumulative self-heal)", got)
	}
}

// --- Task A1: PersistDeployment keeps the mirror consistent ---

// seedClientWithDeployment writes a client + one deployment through the repo
// and then Restores so the mirror is populated.
func seedClientWithDeployment(t *testing.T, svc *Service, repo *fakeRepo, clientID ClientID, agentID string) {
	t.Helper()
	repo.clientsByID[clientID] = Client{ID: clientID, Name: string(clientID)}
	if err := repo.PutDeployment(context.Background(), Deployment{ClientID: clientID, AgentID: agentID}); err != nil {
		t.Fatalf("seed PutDeployment: %v", err)
	}
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("seed Restore: %v", err)
	}
}

func TestPersistDeploymentUpdatesMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(ServiceConfig{Repo: repo})
	ctx := context.Background()

	clientID := ClientID("client-1")
	seedClientWithDeployment(t, svc, repo, clientID, "agent-1")

	updated := Deployment{
		ClientID:           clientID,
		AgentID:            "agent-1",
		LastResetEpochSecs: 12345,
	}
	if err := svc.PersistDeployment(ctx, updated); err != nil {
		t.Fatalf("PersistDeployment: %v", err)
	}

	snap := svc.MirrorSnapshot()
	got, ok := snap.Deployments[clientID]["agent-1"]
	if !ok {
		t.Fatalf("deployment missing from mirror after PersistDeployment")
	}
	if got.LastResetEpochSecs != 12345 {
		t.Fatalf("mirror LastResetEpochSecs = %d, want 12345", got.LastResetEpochSecs)
	}
}

// --- Phase 6.9 additional coverage tests ---

func TestService_Save_NilVault_PlaintextRoundtrip(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	// nil Vault: encryptSecret is a no-op, secret stored as plaintext.
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: nil,
	})
	c := Client{ID: "c-plain", Name: "plain", Secret: "my-secret"}
	if err := svc.Save(context.Background(), c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	stored := repo.clientsByID["c-plain"]
	if stored.Secret != "my-secret" {
		t.Fatalf("stored.Secret = %q, want %q (nil vault = plaintext passthrough)", stored.Secret, "my-secret")
	}
	mirror, err := svc.Get(context.Background(), "c-plain")
	if err != nil {
		t.Fatalf("Get after Save: %v", err)
	}
	if mirror.Secret != "my-secret" {
		t.Fatalf("mirror.Secret = %q, want %q", mirror.Secret, "my-secret")
	}
}

func TestService_SaveState_EmptyAssignmentsAndDeployments(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: nil,
	})
	c := Client{ID: "c-empty", Name: "empty"}
	if err := svc.SaveState(context.Background(), c, nil, nil); err != nil {
		t.Fatalf("SaveState with nil slices: %v", err)
	}
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	if len(svc.mirrorAssignments["c-empty"]) != 0 {
		t.Fatalf("mirrorAssignments[c-empty] len = %d, want 0", len(svc.mirrorAssignments["c-empty"]))
	}
	if len(svc.mirrorDeployments["c-empty"]) != 0 {
		t.Fatalf("mirrorDeployments[c-empty] len = %d, want 0", len(svc.mirrorDeployments["c-empty"]))
	}
}

// --- D1 (B3): mirror-consistency methods for the server write-paths ---

// TestService_ZeroLiveGaugesMirror verifies that ZeroLiveGaugesForAgent
// zeros the live connection/IP gauges in the mirror for every client the
// agent owns usage for but did NOT report in the current snapshot, while
// preserving accumulated traffic and leaving reported (seen) clients
// untouched. Mirrors the server's zeroLiveGaugesForUntouchedClients.
func TestService_ZeroLiveGaugesMirror(t *testing.T) {
	t.Parallel()

	svc := NewService(ServiceConfig{Repo: newFakeRepo()})
	// Two clients on agent-1: c-seen (reported this tick) and c-idle (not).
	svc.applyUsageMirror(Usage{ClientID: "c-seen", AgentID: "agent-1", TrafficUsedBytes: 100, ActiveTCPConns: 3, ActiveUniqueIPs: 2})
	svc.applyUsageMirror(Usage{ClientID: "c-idle", AgentID: "agent-1", TrafficUsedBytes: 500, ActiveTCPConns: 7, ActiveUniqueIPs: 4})
	// A different agent's row on c-idle must be left alone.
	svc.applyUsageMirror(Usage{ClientID: "c-idle", AgentID: "agent-2", TrafficUsedBytes: 9, ActiveTCPConns: 1, ActiveUniqueIPs: 1})

	svc.ZeroLiveGaugesForAgent("agent-1", map[string]struct{}{"c-seen": {}})

	svc.mu.RLock()
	defer svc.mu.RUnlock()

	seen := svc.mirrorUsage["c-seen"]["agent-1"]
	if seen.ActiveTCPConns != 3 || seen.ActiveUniqueIPs != 2 {
		t.Fatalf("seen client gauges changed: %+v", seen)
	}
	idle := svc.mirrorUsage["c-idle"]["agent-1"]
	if idle.ActiveTCPConns != 0 || idle.ActiveUniqueIPs != 0 {
		t.Fatalf("idle client gauges not zeroed: %+v", idle)
	}
	if idle.TrafficUsedBytes != 500 {
		t.Fatalf("idle client traffic mutated: %d, want 500", idle.TrafficUsedBytes)
	}
	other := svc.mirrorUsage["c-idle"]["agent-2"]
	if other.ActiveTCPConns != 1 || other.ActiveUniqueIPs != 1 {
		t.Fatalf("other agent's gauges mutated: %+v", other)
	}
}

// TestService_DropAgentUsageMirror verifies that DropAgentUsageMirror
// removes every (client, agent) usage row owned by the agent plus its
// per-agent seq cursor, while leaving other agents' rows intact.
func TestService_DropAgentUsageMirror(t *testing.T) {
	t.Parallel()

	svc := NewService(ServiceConfig{Repo: newFakeRepo()})
	svc.applyUsageMirror(Usage{ClientID: "c-1", AgentID: "agent-1", TrafficUsedBytes: 10, LastSeq: 4})
	svc.applyUsageMirror(Usage{ClientID: "c-1", AgentID: "agent-2", TrafficUsedBytes: 20, LastSeq: 9})
	svc.applyUsageMirror(Usage{ClientID: "c-2", AgentID: "agent-1", TrafficUsedBytes: 30, LastSeq: 4})

	svc.DropAgentUsageMirror("agent-1")

	svc.mu.RLock()
	defer svc.mu.RUnlock()

	if _, ok := svc.mirrorUsage["c-1"]["agent-1"]; ok {
		t.Fatal("c-1/agent-1 still present after drop")
	}
	if _, ok := svc.mirrorUsage["c-1"]["agent-2"]; !ok {
		t.Fatal("c-1/agent-2 wrongly dropped")
	}
	// c-2 had only agent-1 — the now-empty inner map should be removed.
	if _, ok := svc.mirrorUsage["c-2"]; ok {
		t.Fatal("c-2 inner map not pruned after dropping its only agent")
	}
	if _, ok := svc.mirrorLastUsageSeq["agent-1"]; ok {
		t.Fatal("mirrorLastUsageSeq[agent-1] not dropped")
	}
	if svc.mirrorLastUsageSeq["agent-2"] != 9 {
		t.Fatalf("mirrorLastUsageSeq[agent-2] = %d, want 9", svc.mirrorLastUsageSeq["agent-2"])
	}
}

// --- BackfillSubscriptionTokens tests ---

// TestService_BackfillSubscriptionTokens_FillsEmpty verifies that clients
// without tokens receive unique non-empty tokens and that already-tokened
// clients and deleted clients are left unchanged.
func TestService_BackfillSubscriptionTokens_FillsEmpty(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo:  repo,
		UoW:   newFakeUoW(rs),
		Vault: nil, // nil vault — plaintext secret passthrough
	})

	now := time.Now().UTC()
	deleted := now
	// c-empty-1 and c-empty-2: need tokens.
	repo.clientsByID["c-empty-1"] = Client{
		ID:      "c-empty-1",
		Name:    "needs-token-1",
		Secret:  "secret1",
		Enabled: true,
	}
	repo.clientsByID["c-empty-2"] = Client{
		ID:      "c-empty-2",
		Name:    "needs-token-2",
		Secret:  "secret2",
		Enabled: true,
	}
	// c-has-token: already has a token — must be skipped.
	repo.clientsByID["c-has-token"] = Client{
		ID:                "c-has-token",
		Name:              "has-token",
		Secret:            "secret3",
		SubscriptionToken: "existing-token-abc",
	}
	// c-deleted: soft-deleted — must be skipped.
	repo.clientsByID["c-deleted"] = Client{
		ID:        "c-deleted",
		Name:      "deleted",
		Secret:    "secret4",
		DeletedAt: &deleted,
	}

	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	n, err := svc.BackfillSubscriptionTokens(context.Background())
	if err != nil {
		t.Fatalf("BackfillSubscriptionTokens: %v", err)
	}
	if n != 2 {
		t.Fatalf("updated count = %d, want 2", n)
	}

	// Both empty clients must now have non-empty tokens in the repo.
	c1 := repo.clientsByID["c-empty-1"]
	c2 := repo.clientsByID["c-empty-2"]
	if c1.SubscriptionToken == "" {
		t.Fatal("c-empty-1: token still empty after backfill")
	}
	if c2.SubscriptionToken == "" {
		t.Fatal("c-empty-2: token still empty after backfill")
	}
	// Tokens must be distinct.
	if c1.SubscriptionToken == c2.SubscriptionToken {
		t.Fatalf("tokens not unique: both = %q", c1.SubscriptionToken)
	}
	// The already-tokened client must be unchanged.
	cHas := repo.clientsByID["c-has-token"]
	if cHas.SubscriptionToken != "existing-token-abc" {
		t.Fatalf("c-has-token token changed: got %q, want %q", cHas.SubscriptionToken, "existing-token-abc")
	}
	// No-token field clobbering: name and secret must survive.
	if c1.Name != "needs-token-1" {
		t.Fatalf("c-empty-1 name clobbered: %q", c1.Name)
	}
	if c1.Secret != "secret1" {
		t.Fatalf("c-empty-1 secret clobbered: %q", c1.Secret)
	}
	if c1.Enabled != true {
		t.Fatal("c-empty-1 enabled clobbered")
	}

	// Mirror must also reflect the new tokens.
	mirror := svc.MirrorSnapshot()
	if mirror.Clients["c-empty-1"].SubscriptionToken == "" {
		t.Fatal("c-empty-1: mirror token still empty after backfill")
	}
}

// TestService_BackfillSubscriptionTokens_Idempotent verifies that a second
// call updates 0 clients when all clients already have tokens.
func TestService_BackfillSubscriptionTokens_Idempotent(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	svc := NewService(ServiceConfig{
		Repo: repo,
		UoW:  newFakeUoW(rs),
	})

	repo.clientsByID["c-a"] = Client{ID: "c-a", Name: "a", Secret: "s"}
	repo.clientsByID["c-b"] = Client{ID: "c-b", Name: "b", Secret: "s"}

	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// First call: fills tokens.
	n1, err := svc.BackfillSubscriptionTokens(context.Background())
	if err != nil {
		t.Fatalf("first BackfillSubscriptionTokens: %v", err)
	}
	if n1 != 2 {
		t.Fatalf("first call updated %d, want 2", n1)
	}

	// Re-restore to simulate a panel restart loading from DB.
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("second Restore: %v", err)
	}

	// Second call: must update 0.
	n2, err := svc.BackfillSubscriptionTokens(context.Background())
	if err != nil {
		t.Fatalf("second BackfillSubscriptionTokens: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second call updated %d, want 0 (idempotent)", n2)
	}
}

// TestService_BackfillSubscriptionTokens_NoRepo verifies that calling
// BackfillSubscriptionTokens on a Service with no repo is a no-op.
func TestService_BackfillSubscriptionTokens_NoRepo(t *testing.T) {
	t.Parallel()

	svc := NewService(ServiceConfig{}) // no repo
	n, err := svc.BackfillSubscriptionTokens(context.Background())
	if err != nil {
		t.Fatalf("expected no error with no repo, got: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 updated with no repo, got: %d", n)
	}
}

// TestService_SeedUsageMirror verifies that SeedUsageMirror writes a
// usage row into the mirror without touching persistence, and only when
// no row already exists for that (client, agent) pair (matching the
// restore-time discovered-seed fallback semantics).
func TestService_SeedUsageMirror(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(ServiceConfig{Repo: repo})

	now := time.Now()
	svc.SeedUsageMirror("c-1", "agent-1", 4096, 5, 3, now)

	svc.mu.RLock()
	um := svc.mirrorUsage["c-1"]["agent-1"]
	svc.mu.RUnlock()
	if um.TrafficUsedBytes != 4096 || um.ActiveTCPConns != 5 || um.ActiveUniqueIPs != 3 || um.UniqueIPsUsed != 3 {
		t.Fatalf("seeded mirror row wrong: %+v", um)
	}
	// Seed-mirror must not persist (it's a display fallback).
	if got, err := repo.ListUsage(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("SeedUsageMirror persisted to repo: rows=%d err=%v", len(got), err)
	}

	// Existing row must not be overwritten.
	svc.SeedUsageMirror("c-1", "agent-1", 1, 1, 1, now.Add(time.Hour))
	svc.mu.RLock()
	um2 := svc.mirrorUsage["c-1"]["agent-1"]
	svc.mu.RUnlock()
	if um2.TrafficUsedBytes != 4096 {
		t.Fatalf("SeedUsageMirror overwrote existing row: %+v", um2)
	}
}

// --- ResolveBySubscriptionToken sentinel tests ---

func TestService_ResolveBySubscriptionToken_BlankToken(t *testing.T) {
	t.Parallel()

	svc := NewService(ServiceConfig{Repo: newFakeRepo()})
	_, err := svc.ResolveBySubscriptionToken(context.Background(), "")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("blank token: err = %v, want storage.ErrNotFound", err)
	}
}

func TestService_ResolveBySubscriptionToken_UnknownToken(t *testing.T) {
	t.Parallel()

	svc := NewService(ServiceConfig{Repo: newFakeRepo()})
	_, err := svc.ResolveBySubscriptionToken(context.Background(), "tok-unknown")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("unknown token: err = %v, want storage.ErrNotFound", err)
	}
}
