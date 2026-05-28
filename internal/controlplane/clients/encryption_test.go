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

// TestRestoreDecryptsClientSecret reproduces the production bug where,
// after a panel restart, assigning an (adopted or created) client to a
// new node fails with "apply client failed: bad_request: secret must be
// exactly 32 hex characters". SaveState stores the secret encrypted
// (PVS2:) and keeps plaintext in the live mirror, but Restore re-reads
// from the Repository on startup — and must decrypt symmetric to Save,
// otherwise the mirror (and every client-apply job built from it) ships
// the PVS2: ciphertext to telemt, which rejects it.
func TestRestoreDecryptsClientSecret(t *testing.T) {
	plaintextSecret := "deadbeef0123456789abcdef01234567"
	client := Client{
		ID:        "client-0000003",
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

	writer := NewServiceV2(ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: newFakeDiscoveredRepo(),
		UoW:            uow,
		Vault:          vault,
	})
	if err := writer.SaveState(context.Background(), client, nil, nil); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	// Simulate a panel restart: a fresh Service over the same Repository
	// whose mirror is rebuilt solely from Restore.
	reloaded := NewServiceV2(ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: newFakeDiscoveredRepo(),
		UoW:            uow,
		Vault:          vault,
	})
	if err := reloaded.Restore(context.Background()); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	got, err := reloaded.Get(context.Background(), client.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Secret != plaintextSecret {
		t.Fatalf("after Restore mirror Secret = %q, want plaintext %q — secret was not decrypted on load", got.Secret, plaintextSecret)
	}
}

// TestRestoreHealsDoubleEncryptedSecret covers the secondary corruption:
// before the decrypt-on-load fix, editing a client (e.g. assigning a new
// node) while the mirror held ciphertext re-encrypted the secret into the
// DB as PVS2:PVS2:…. Restore must peel every layer so such a row recovers
// to plaintext without manual intervention.
func TestRestoreHealsDoubleEncryptedSecret(t *testing.T) {
	plaintextSecret := "deadbeef0123456789abcdef01234567"
	vault, err := secretvault.New("operator-passphrase", secretvault.AllDomains)
	if err != nil {
		t.Fatalf("secretvault.New() error = %v", err)
	}

	ct1, err := vault.Encrypt(secretvault.DomainClientSecret, plaintextSecret)
	if err != nil {
		t.Fatalf("first Encrypt() error = %v", err)
	}
	ct2, err := vault.Encrypt(secretvault.DomainClientSecret, ct1)
	if err != nil {
		t.Fatalf("second Encrypt() error = %v", err)
	}

	repo := newFakeRepo()
	// Seed the repo directly with a double-wrapped secret, as the buggy
	// save path would have left it in the DB.
	repo.clientsByID["client-0000004"] = Client{
		ID:      "client-0000004",
		Name:    "alpha",
		Secret:  ct2,
		Enabled: true,
	}

	svc := NewServiceV2(ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: newFakeDiscoveredRepo(),
		Vault:          vault,
	})
	if err := svc.Restore(context.Background()); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	got, err := svc.Get(context.Background(), "client-0000004")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Secret != plaintextSecret {
		t.Fatalf("after Restore mirror Secret = %q, want plaintext %q — double-wrapped secret not healed", got.Secret, plaintextSecret)
	}
}

// TestEncryptSecretIsIdempotent verifies SaveState never double-wraps a
// secret that already carries a vault prefix — the guard that prevents the
// PVS2:PVS2: corruption from recurring on any save path.
func TestEncryptSecretIsIdempotent(t *testing.T) {
	plaintextSecret := "deadbeef0123456789abcdef01234567"
	vault, err := secretvault.New("operator-passphrase", secretvault.AllDomains)
	if err != nil {
		t.Fatalf("secretvault.New() error = %v", err)
	}
	alreadyEncrypted, err := vault.Encrypt(secretvault.DomainClientSecret, plaintextSecret)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
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

	// Save a client whose Secret is already a ciphertext (as a stale
	// mirror would carry it). The guard must store it verbatim.
	client := Client{ID: "client-0000005", Name: "alpha", Secret: alreadyEncrypted, Enabled: true}
	if err := svc.SaveState(context.Background(), client, nil, nil); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	stored := repo.clientsByID["client-0000005"].Secret
	if stored != alreadyEncrypted {
		t.Fatalf("stored Secret = %q, want it left as the single ciphertext %q (no double-wrap)", stored, alreadyEncrypted)
	}
	// And it still decrypts in a single pass to the original plaintext.
	pt, err := vault.Decrypt(secretvault.DomainClientSecret, stored)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if pt != plaintextSecret {
		t.Fatalf("single decrypt = %q, want %q", pt, plaintextSecret)
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
