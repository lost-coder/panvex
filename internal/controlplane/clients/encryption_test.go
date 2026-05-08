package clients

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// fakeClientStore is a minimal storage.Store fake that captures the
// records written via PutClient. Only the methods PersistState calls
// are implemented; the rest panic so accidental call sites are loud.
type fakeClientStore struct {
	mu          sync.Mutex
	clients     map[string]storage.ClientRecord
	assignments map[string][]storage.ClientAssignmentRecord
	deployments map[string]storage.ClientDeploymentRecord
}

func newFakeClientStore() *fakeClientStore {
	return &fakeClientStore{
		clients:     map[string]storage.ClientRecord{},
		assignments: map[string][]storage.ClientAssignmentRecord{},
		deployments: map[string]storage.ClientDeploymentRecord{},
	}
}

func (f *fakeClientStore) PutClient(_ context.Context, record storage.ClientRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients[record.ID] = record
	return nil
}

func (f *fakeClientStore) DeleteClientAssignments(_ context.Context, clientID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.assignments, clientID)
	return nil
}

func (f *fakeClientStore) PutClientAssignment(_ context.Context, record storage.ClientAssignmentRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assignments[record.ClientID] = append(f.assignments[record.ClientID], record)
	return nil
}

func (f *fakeClientStore) PutClientDeployment(_ context.Context, record storage.ClientDeploymentRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deployments[record.ClientID+":"+record.AgentID] = record
	return nil
}

func (f *fakeClientStore) ListClients(_ context.Context) ([]storage.ClientRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.ClientRecord, 0, len(f.clients))
	for _, r := range f.clients {
		out = append(out, r)
	}
	return out, nil
}

func TestPersistStateEncryptsClientSecretWhenVaultEnabled(t *testing.T) {
	plaintextSecret := "deadbeef0123456789abcdef01234567"
	client := Client{
		ID:        "client-0000001",
		Name:      "alpha",
		Secret:    plaintextSecret,
		Enabled:   true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	vault, err := secretvault.New("operator-passphrase", secretvault.AllDomains)
	if err != nil {
		t.Fatalf("secretvault.New() error = %v", err)
	}

	// Reuse the fake store to drive PersistState only — the call sites
	// inside server cluster on top-level Store rather than the slim
	// surface this test exercises, so we wrap our fake into a store
	// adapter that satisfies just the methods PersistState uses.
	store := &persistStoreAdapter{fake: newFakeClientStore()}
	if err := PersistState(context.Background(), store, client, nil, nil, vault); err != nil {
		t.Fatalf("PersistState() error = %v", err)
	}

	stored, ok := store.fake.clients[string(client.ID)]
	if !ok {
		t.Fatal("PersistState() did not persist client record")
	}
	if !strings.HasPrefix(stored.SecretCiphertext, secretvault.Prefix) {
		t.Fatalf("SecretCiphertext = %q, want PVS1: prefix when vault enabled", stored.SecretCiphertext)
	}
	if strings.Contains(stored.SecretCiphertext, plaintextSecret) {
		t.Fatalf("SecretCiphertext contains plaintext %q", plaintextSecret)
	}

	// Round-trip: decrypted record yields the original Client.Secret.
	decoded, err := DecryptClientRecord(stored, vault)
	if err != nil {
		t.Fatalf("DecryptClientRecord() error = %v", err)
	}
	if decoded.SecretCiphertext != plaintextSecret {
		t.Fatalf("DecryptClientRecord() Secret = %q, want %q", decoded.SecretCiphertext, plaintextSecret)
	}
}

func TestPersistStateLeavesPlaintextWhenVaultDisabled(t *testing.T) {
	client := Client{
		ID:     "client-0000002",
		Name:   "alpha",
		Secret: "still-plain-secret",
	}
	store := &persistStoreAdapter{fake: newFakeClientStore()}
	if err := PersistState(context.Background(), store, client, nil, nil, nil); err != nil {
		t.Fatalf("PersistState(nil vault) error = %v", err)
	}
	stored := store.fake.clients[string(client.ID)]
	if stored.SecretCiphertext != "still-plain-secret" {
		t.Fatalf("nil vault should pass through, got %q", stored.SecretCiphertext)
	}
}

// persistStoreAdapter satisfies storage.Store via a nil embedded
// interface; only the four methods PersistState calls are overridden,
// and they delegate to the fake. Anything else panics.
type persistStoreAdapter struct {
	storage.Store
	fake *fakeClientStore
}

func (p *persistStoreAdapter) PutClient(ctx context.Context, r storage.ClientRecord) error {
	return p.fake.PutClient(ctx, r)
}

func (p *persistStoreAdapter) DeleteClientAssignments(ctx context.Context, id string) error {
	return p.fake.DeleteClientAssignments(ctx, id)
}

func (p *persistStoreAdapter) PutClientAssignment(ctx context.Context, r storage.ClientAssignmentRecord) error {
	return p.fake.PutClientAssignment(ctx, r)
}

func (p *persistStoreAdapter) PutClientDeployment(ctx context.Context, r storage.ClientDeploymentRecord) error {
	return p.fake.PutClientDeployment(ctx, r)
}
