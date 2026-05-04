package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// ctxCapturingStore is a minimal storage.Store stub that records the ctx
// it received on the first GetCPSecret call, then returns ErrNotFound so
// callers proceed to the mint path. Used by the cancellation tests to pin
// Plan 3 Task 3: the secret loaders must thread the caller ctx through to
// storage instead of substituting context.Background().
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

// TestLoadOrCreateCSRFSecret_PropagatesCallerCtx pins Plan 3 Task 3: the
// CSRF secret loader must hand the caller's ctx to storage so a Close()
// during a wedged GetCPSecret aborts it via serverCtx cancellation. The
// loader itself is best-effort (a storage failure logs and falls back to
// the in-memory fresh secret) so this test asserts ctx propagation, not
// that loadOrCreateCSRFSecret returns a context.Canceled error.
func TestLoadOrCreateCSRFSecret_PropagatesCallerCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := &ctxCapturingStore{}
	if _, err := loadOrCreateCSRFSecret(ctx, store); err != nil {
		t.Fatalf("loadOrCreateCSRFSecret returned error: %v", err)
	}
	if store.captured == nil {
		t.Fatal("GetCPSecret was not invoked")
	}
	if store.captured != ctx {
		t.Fatalf("GetCPSecret ctx = %v, want caller ctx %v", store.captured, ctx)
	}
}

// TestLoadOrCreateVaultSalt_RespectsContextCancellation pins the same
// contract for the vault HKDF salt loader. Unlike the CSRF loader the
// vault salt loader is fail-loud — a storage error must propagate so the
// operator does not silently lose the only path to decrypt later writes.
func TestLoadOrCreateVaultSalt_RespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := loadOrCreateVaultSalt(ctx, &ctxCapturingStore{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loadOrCreateVaultSalt error = %v, want context.Canceled", err)
	}
}

func TestCSRFTokenForSession_Stable(t *testing.T) {
	secret := []byte("fixed-server-secret-32-bytes-okok")
	a := csrfTokenForSession("sess-1", secret)
	b := csrfTokenForSession("sess-1", secret)
	if a != b {
		t.Fatalf("token must be stable for same input, got %q vs %q", a, b)
	}
}

func TestCSRFTokenForSession_DifferentSession(t *testing.T) {
	secret := []byte("fixed-server-secret-32-bytes-okok")
	if csrfTokenForSession("sess-1", secret) == csrfTokenForSession("sess-2", secret) {
		t.Fatal("different sessions must yield different tokens")
	}
}

func TestCSRFTokenForSession_DifferentSecret(t *testing.T) {
	a := csrfTokenForSession("sess-1", []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	b := csrfTokenForSession("sess-1", []byte("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	if a == b {
		t.Fatal("different secrets must yield different tokens")
	}
}

func TestCSRFTokenMatches_ConstantTime(t *testing.T) {
	if csrfTokenMatches("", "abc") {
		t.Fatal("empty supplied must not match")
	}
	if csrfTokenMatches("abc", "") {
		t.Fatal("empty expected must not match")
	}
	if !csrfTokenMatches("abc", "abc") {
		t.Fatal("equal strings must match")
	}
	if csrfTokenMatches("abc", "abd") {
		t.Fatal("differing strings must not match")
	}
}

func TestCSRFTokenMiddleware_AllowsSafeMethods(t *testing.T) {
	srv := &Server{csrfSecret: []byte("any-secret-32-bytes-zero-padded.")}

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			handler := srv.csrfTokenMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequestWithContext(t.Context(), method, "/api/whatever", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Fatalf("safe method %s should bypass middleware, got status %d", method, rec.Code)
			}
		})
	}
}

func TestCSRFTokenMiddleware_AllowsUnauthenticated(t *testing.T) {
	// State-changing methods reach the middleware without a session
	// (POST /auth/login). The middleware lets them through; downstream
	// auth + Origin check handles the rest.
	srv := &Server{csrfSecret: []byte("any-secret-32-bytes-zero-padded.")}
	handler := srv.csrfTokenMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unauth state-changing should pass middleware, got %d", rec.Code)
	}
}

func TestCSRFTokenMiddleware_RejectsMissingToken(t *testing.T) {
	srv := &Server{csrfSecret: []byte("any-secret-32-bytes-zero-padded.")}
	handler := srv.csrfTokenMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not be invoked when token is missing")
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/anything", nil)
	req = withRequestAuthContext(req,
		auth.Session{ID: "sess-1", UserID: "user-1"},
		auth.User{ID: "user-1", Username: "alice"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestCSRFTokenMiddleware_AcceptsValidToken(t *testing.T) {
	secret := []byte("any-secret-32-bytes-zero-padded.")
	srv := &Server{csrfSecret: secret}
	called := false
	handler := srv.csrfTokenMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/anything", nil)
	req.Header.Set(csrfTokenHeader, csrfTokenForSession("sess-1", secret))
	req = withRequestAuthContext(req,
		auth.Session{ID: "sess-1", UserID: "user-1"},
		auth.User{ID: "user-1", Username: "alice"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !called {
		t.Fatal("handler should be invoked when token is valid")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestCSRFTokenMiddleware_RejectsTokenForOtherSession(t *testing.T) {
	secret := []byte("any-secret-32-bytes-zero-padded.")
	srv := &Server{csrfSecret: secret}
	handler := srv.csrfTokenMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not be invoked when token is for a different session")
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/anything", nil)
	// Token derived from a different session should not validate the
	// current one — prevents an attacker who steals a token from one
	// session using it on another.
	req.Header.Set(csrfTokenHeader, csrfTokenForSession("other-sess", secret))
	req = withRequestAuthContext(req,
		auth.Session{ID: "sess-1", UserID: "user-1"},
		auth.User{ID: "user-1", Username: "alice"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
