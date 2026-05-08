package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/csrf"
)

// newCSRFTestServer builds a minimal Server with a deterministic CSRF
// Manager so middleware tests don't need to thread a fixture secret
// through every call site.
func newCSRFTestServer() *Server {
	return &Server{
		csrfManager: &csrf.Manager{Secret: []byte("any-secret-32-bytes-zero-padded.")},
	}
}

func TestCSRFTokenMiddleware_AllowsSafeMethods(t *testing.T) {
	srv := newCSRFTestServer()

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
	srv := newCSRFTestServer()
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
	srv := newCSRFTestServer()
	handler := srv.csrfTokenMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not be invoked when token is missing")
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/anything", nil)
	// S22 Task 5: CSRF token is bound to the cookie value, so the
	// fixture must carry both the session context (post-auth) and
	// the cookie that the browser would have sent.
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "cookie-token-1"})
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
	srv := newCSRFTestServer()
	called := false
	handler := srv.csrfTokenMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/anything", nil)
	// S22 Task 5: token derives from the cookie value the browser
	// sends, not from the internal Session.ID. Set both so the
	// middleware can read the cookie out of the request and match
	// the supplied X-CSRF-Token.
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "cookie-token-1"})
	req.Header.Set(csrf.TokenHeader, srv.csrfManager.TokenForSession("cookie-token-1"))
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
	srv := newCSRFTestServer()
	handler := srv.csrfTokenMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not be invoked when token is for a different session")
	}))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/anything", nil)
	// The browser carries cookie A but the attacker forwards a
	// CSRF token derived from cookie B — must not validate.
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "cookie-token-1"})
	req.Header.Set(csrf.TokenHeader, srv.csrfManager.TokenForSession("other-cookie-token"))
	req = withRequestAuthContext(req,
		auth.Session{ID: "sess-1", UserID: "user-1"},
		auth.User{ID: "user-1", Username: "alice"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
