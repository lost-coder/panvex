package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"golang.org/x/crypto/argon2"
)

// storeLegacyV2Hash rewrites the user's stored hash to the pre-profile v2
// format, exactly as a release before the KDF split would have written it.
func storeLegacyV2Hash(t *testing.T, service *Service, userID, password string) {
	t.Helper()
	salt := []byte("0123456789abcdef")
	derived := argon2.IDKey([]byte(password), salt, 4, 96*1024, 2, 32)
	hash := fmt.Sprintf("argon2id$v=2$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived))

	user, err := service.loadManagedUserByIDCtx(context.Background(), userID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	user.PasswordHash = hash
	if err := service.storeUserWithContext(context.Background(), user); err != nil {
		t.Fatalf("store legacy hash: %v", err)
	}
}

func storedPasswordHash(t *testing.T, service *Service, userID string) string {
	t.Helper()
	user, err := service.loadManagedUserByIDCtx(context.Background(), userID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	return user.PasswordHash
}

func bootstrapRehashUser(t *testing.T, password string) (*Service, string) {
	t.Helper()
	now := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)
	service := NewService()
	service.SetNow(func() time.Time { return now })
	user, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "operator",
		Password: password,
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	return service, user.ID
}

// TestAuthenticateRehashesLegacyHashToActiveProfile proves the low-memory
// profile actually reaches existing installs: without rehash-on-login their
// hashes stay at 96 MiB forever and the profile buys nothing on the login
// path (the one that repeats).
func TestAuthenticateRehashesLegacyHashToActiveProfile(t *testing.T) {
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })
	const password = "Correct1horse2battery"
	service, userID := bootstrapRehashUser(t, password)
	storeLegacyV2Hash(t, service, userID, password)

	if err := kdf.SetActiveProfile("low-memory"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}

	now := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	if _, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: password,
	}, now); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	got := storedPasswordHash(t, service, userID)
	if !strings.HasPrefix(got, "argon2id$v=3$t=2,m=19456,p=1$") {
		t.Fatalf("hash must be rewritten with the active profile, got %s", got)
	}
	// The rewritten hash must still verify the same password — a rehash that
	// breaks the credential would lock the operator out on the next login.
	if err := verifyPassword(got, password); err != nil {
		t.Fatalf("rehashed hash must verify: %v", err)
	}
	// And it must be stable: a second login has nothing left to do.
	if needsRehash(got) {
		t.Fatal("rehashed hash must not need another rehash")
	}
}

// TestAuthenticateDoesNotRehashOnFailedLogin: a wrong password must never
// touch the stored hash. Otherwise an attacker could force writes (and
// 96 MiB derivations) with guesses alone.
func TestAuthenticateDoesNotRehashOnFailedLogin(t *testing.T) {
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })
	const password = "Correct1horse2battery"
	service, userID := bootstrapRehashUser(t, password)
	storeLegacyV2Hash(t, service, userID, password)
	before := storedPasswordHash(t, service, userID)

	if err := kdf.SetActiveProfile("low-memory"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}

	now := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	if _, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: "wrong-password",
	}, now); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Authenticate with wrong password: %v", err)
	}

	if after := storedPasswordHash(t, service, userID); after != before {
		t.Fatalf("failed login must not rewrite the hash: %s -> %s", before, after)
	}
}

// TestAuthenticateSkipsRehashWhenProfileUnchanged keeps the steady state
// cheap: a login on a current hash must not burn a second derivation.
func TestAuthenticateSkipsRehashWhenProfileUnchanged(t *testing.T) {
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })
	if err := kdf.SetActiveProfile("low-memory"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	const password = "Correct1horse2battery"
	service, userID := bootstrapRehashUser(t, password)
	before := storedPasswordHash(t, service, userID)

	derivationsBefore := kdf.Derivations()
	now := time.Date(2026, time.July, 16, 9, 0, 0, 0, time.UTC)
	if _, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: password,
	}, now); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got := kdf.Derivations() - derivationsBefore; got != 1 {
		t.Fatalf("login on a current hash must derive exactly once, got %d", got)
	}
	if after := storedPasswordHash(t, service, userID); after != before {
		t.Fatalf("hash on the active profile must not be rewritten")
	}
}

// failingPutUserStore wraps a UserStore and fails every write, modelling a
// read-only / degraded database.
type failingPutUserStore struct {
	storage.UserStore
	putErr error
}

func (f *failingPutUserStore) PutUser(context.Context, storage.UserRecord) error { return f.putErr }

// TestAuthenticateSucceedsWhenRehashPersistFails: the rehash is an
// opportunistic upgrade, never a precondition for logging in. If the store
// cannot take the write (read-only replica, disk full, degraded DB), the
// operator must still get in — otherwise a storage blip locks EVERY user out of
// the panel exactly when they need it to investigate.
func TestAuthenticateSucceedsWhenRehashPersistFails(t *testing.T) {
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })
	const password = "correct horse battery"
	now := time.Date(2026, time.July, 16, 8, 0, 0, 0, time.UTC)

	backing := newMemoryUserStore()
	service := NewServiceWithStore(backing)
	service.SetNow(func() time.Time { return now })
	user, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "operator",
		Password: password,
		Role:     RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	storeLegacyV2Hash(t, service, user.ID, password)
	hashBefore := storedPasswordHash(t, service, user.ID)

	// Now make every write fail, and switch the profile so a rehash is due.
	service.userStore = &failingPutUserStore{UserStore: backing, putErr: errors.New("attempt to write a readonly database")}
	if err := kdf.SetActiveProfile("low-memory"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}

	if _, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: password,
	}, now); err != nil {
		t.Fatalf("a failed rehash persist must not break the login: %v", err)
	}

	if got := storedPasswordHash(t, service, user.ID); got != hashBefore {
		t.Fatalf("the stored hash must be unchanged when the write failed:\n got %s\nwant %s", got, hashBefore)
	}
}
