package clients

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
)

// TestSaveStateEncryptsClientSecretWhenVaultEnabled verifies that
// Service.SaveState (the UoW path) encrypts the client secret before
// writing it to the Repository, matching the encryption boundary
// previously tested via PersistState.
func TestSaveStateEncryptsClientSecretWhenVaultEnabled(t *testing.T) {
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

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	uow := newFakeUoW(rs)

	svc := NewServiceV2(ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: newFakeDiscoveredRepo(),
		UoW:            uow,
		Vault:          vault,
	})
	if err := svc.SaveState(context.Background(), client, nil, nil); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	stored, ok := repo.clientsByID[client.ID]
	if !ok {
		t.Fatal("SaveState() did not persist client record")
	}
	if !strings.HasPrefix(stored.Secret, secretvault.Prefix) {
		t.Fatalf("Secret = %q, want PVS1: prefix when vault enabled", stored.Secret)
	}
	if strings.Contains(stored.Secret, plaintextSecret) {
		t.Fatalf("Secret contains plaintext %q", stored.Secret)
	}

	// Round-trip: the V2 in-memory mirror retains the plaintext secret.
	mirrored, err := svc.Get(context.Background(), client.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if mirrored.Secret != plaintextSecret {
		t.Fatalf("mirror Secret = %q, want %q", mirrored.Secret, plaintextSecret)
	}
}

// TestSaveStateLeavesPlaintextWhenVaultDisabled verifies that without a
// vault (nil) the secret is stored verbatim in the Repository.
func TestSaveStateLeavesPlaintextWhenVaultDisabled(t *testing.T) {
	client := Client{
		ID:     "client-0000002",
		Name:   "alpha",
		Secret: "still-plain-secret",
	}

	repo := newFakeRepo()
	rs := &fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}
	uow := newFakeUoW(rs)

	svc := NewServiceV2(ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: newFakeDiscoveredRepo(),
		UoW:            uow,
	})
	if err := svc.SaveState(context.Background(), client, nil, nil); err != nil {
		t.Fatalf("SaveState(nil vault) error = %v", err)
	}
	stored := repo.clientsByID[client.ID]
	if stored.Secret != "still-plain-secret" {
		t.Fatalf("nil vault should pass through, got %q", stored.Secret)
	}
}
