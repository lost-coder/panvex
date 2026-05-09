package clients

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/audit"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
)

// fakeRepo is a minimal in-memory clients.Repository for Service tests.
// Methods needed for Restore are implemented; others return a sentinel
// error unless failOn is set to inject a specific failure.
type fakeRepo struct {
	clientsByID         map[ClientID]Client
	assignmentsByClient map[ClientID][]Assignment
	deploymentsByClient map[ClientID][]Deployment
	usageRows           []Usage
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

// --- fakeRepoSet: implements ClientsRepoSet ---

type fakeRepoSet struct {
	clients    Repository
	discovered discovered.Repository
	audit      audit.Repository
}

func (r *fakeRepoSet) Clients() Repository               { return r.clients }
func (r *fakeRepoSet) Discovered() discovered.Repository { return r.discovered }
func (r *fakeRepoSet) Audit() audit.Repository           { return r.audit }

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

// --- fakeAuditRepo: implements audit.Repository ---

type fakeAuditRepo struct {
	events []audit.Event
}

func newFakeAuditRepo() *fakeAuditRepo { return &fakeAuditRepo{} }

func (r *fakeAuditRepo) Append(_ context.Context, e audit.Event) error {
	r.events = append(r.events, e)
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

// --- Phase 6.4 tests: Service.Save ---

func TestService_Save_EncryptsAndPersists(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo(), audit: newFakeAuditRepo()}
	svc := NewServiceV2(ServiceConfig{
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
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo(), audit: newFakeAuditRepo()}
	svc := NewServiceV2(ServiceConfig{
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
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo(), audit: newFakeAuditRepo()}
	svc := NewServiceV2(ServiceConfig{
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
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo(), audit: newFakeAuditRepo()}
	svc := NewServiceV2(ServiceConfig{
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

// --- Phase 6.6 tests: Service.AdoptDiscovered ---

func TestService_AdoptDiscovered_AtomicCrossDomain(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	discoveredRepo := newFakeDiscoveredRepo()
	discoveredRepo.byID[discovered.DiscoveredID("d-1")] = discovered.DiscoveredClient{
		ID:         discovered.DiscoveredID("d-1"),
		ClientName: "alpha",
		AgentID:    "a-1",
		Status:     discovered.StatusPending,
	}
	auditRepo := newFakeAuditRepo()
	rs := &fakeRepoSet{clients: repo, discovered: discoveredRepo, audit: auditRepo}
	svc := NewServiceV2(ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: discoveredRepo,
		UoW:            newFakeUoW(rs),
		Vault:          makeTestVault(t),
	})

	c, err := svc.AdoptDiscovered(context.Background(), AdoptInput{
		DiscoveredID: discovered.DiscoveredID("d-1"),
		ActorID:      "u-admin",
	})
	if err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if c.Name != "alpha" {
		t.Fatalf("client name = %q, want alpha", c.Name)
	}
	// Discovered status flipped.
	if discoveredRepo.byID[discovered.DiscoveredID("d-1")].Status != discovered.StatusAdopted {
		t.Fatal("discovered status not flipped")
	}
	// Audit event appended.
	if len(auditRepo.events) != 1 {
		t.Fatalf("audit events = %d, want 1", len(auditRepo.events))
	}
	if auditRepo.events[0].Action != "client.adopt" {
		t.Fatalf("audit action = %q, want client.adopt", auditRepo.events[0].Action)
	}
}

func TestService_AdoptDiscovered_NonPendingFails(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	discoveredRepo := newFakeDiscoveredRepo()
	discoveredRepo.byID[discovered.DiscoveredID("d-2")] = discovered.DiscoveredClient{
		ID:     discovered.DiscoveredID("d-2"),
		Status: discovered.StatusAdopted,
	}
	rs := &fakeRepoSet{clients: repo, discovered: discoveredRepo, audit: newFakeAuditRepo()}
	svc := NewServiceV2(ServiceConfig{
		Repo: repo, DiscoveredRepo: discoveredRepo,
		UoW: newFakeUoW(rs), Vault: makeTestVault(t),
	})

	_, err := svc.AdoptDiscovered(context.Background(), AdoptInput{
		DiscoveredID: discovered.DiscoveredID("d-2"),
		ActorID:      "u-admin",
	})
	if err == nil {
		t.Fatal("expected error adopting non-pending discovered client")
	}
}

