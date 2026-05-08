package csrf

import (
	"context"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// ctxCapturingStore records the ctx it received on the first
// GetCPSecret call, then returns ErrNotFound so callers proceed to
// the mint path. Pins the contract that loaders must thread the
// caller ctx through to storage instead of substituting
// context.Background().
type ctxCapturingStore struct {
	storage.Store
	captured context.Context
}

func (s *ctxCapturingStore) GetCPSecret(ctx context.Context, _ string) ([]byte, error) {
	if s.captured == nil {
		s.captured = ctx
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, storage.ErrNotFound
}

func (*ctxCapturingStore) PutCPSecret(ctx context.Context, _ string, _ []byte) error {
	return ctx.Err()
}

// TestLoadOrCreateSecret_PropagatesCallerCtx pins the contract: the
// CSRF secret loader must hand the caller's ctx to storage so a
// Close() during a wedged GetCPSecret aborts it via serverCtx
// cancellation. The loader itself is best-effort (a storage failure
// logs and falls back to the in-memory fresh secret) so this test
// asserts ctx propagation, not that LoadOrCreateSecret returns a
// context.Canceled error.
func TestLoadOrCreateSecret_PropagatesCallerCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &ctxCapturingStore{}
	if _, err := LoadOrCreateSecret(ctx, store, nil); err != nil {
		t.Fatalf("LoadOrCreateSecret returned error: %v", err)
	}
	if store.captured == nil {
		t.Fatal("GetCPSecret was not invoked")
	}
	if store.captured != ctx {
		t.Fatalf("GetCPSecret ctx = %v, want caller ctx %v", store.captured, ctx)
	}
}

// TestNewManager_LoadsSecret pins that NewManager actually populates
// Secret via the loader instead of constructing a zero-value Manager.
func TestNewManager_LoadsSecret(t *testing.T) {
	mgr, err := NewManager(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if len(mgr.Secret) != SecretBytes {
		t.Fatalf("Secret length = %d, want %d", len(mgr.Secret), SecretBytes)
	}
	if mgr.Logger == nil {
		t.Fatal("Logger must default to slog.Default(), got nil")
	}
}

func TestTokenForSession_Stable(t *testing.T) {
	secret := []byte("fixed-server-secret-32-bytes-okok")
	a := TokenForSession("sess-1", secret)
	b := TokenForSession("sess-1", secret)
	if a != b {
		t.Fatalf("token must be stable for same input, got %q vs %q", a, b)
	}
}

func TestTokenForSession_DifferentSession(t *testing.T) {
	secret := []byte("fixed-server-secret-32-bytes-okok")
	if TokenForSession("sess-1", secret) == TokenForSession("sess-2", secret) {
		t.Fatal("different sessions must yield different tokens")
	}
}

func TestTokenForSession_DifferentSecret(t *testing.T) {
	a := TokenForSession("sess-1", []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	b := TokenForSession("sess-1", []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	if a == b {
		t.Fatal("different secrets must yield different tokens")
	}
}

func TestTokenMatches_ConstantTime(t *testing.T) {
	if TokenMatches("", "abc") {
		t.Fatal("empty supplied must not match")
	}
	if TokenMatches("abc", "") {
		t.Fatal("empty expected must not match")
	}
	if !TokenMatches("abc", "abc") {
		t.Fatal("equal strings must match")
	}
	if TokenMatches("abc", "abd") {
		t.Fatal("differing strings must not match")
	}
}

// TestManagerMethodsMatchPackageFuncs pins that Manager methods
// agree with the package-level helpers — call-sites can use either
// without behavioural drift.
func TestManagerMethodsMatchPackageFuncs(t *testing.T) {
	secret := []byte("any-secret-32-bytes-zero-padded.")
	mgr := &Manager{Secret: secret}
	want := TokenForSession("cookie-A", secret)
	if got := mgr.TokenForSession("cookie-A"); got != want {
		t.Fatalf("Manager.TokenForSession = %q, want %q", got, want)
	}
	if !mgr.TokenMatches(want, want) {
		t.Fatal("Manager.TokenMatches must accept identical token")
	}
	if mgr.TokenMatches(want, "tampered") {
		t.Fatal("Manager.TokenMatches must reject mismatched token")
	}
}
