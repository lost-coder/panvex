package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
)

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
