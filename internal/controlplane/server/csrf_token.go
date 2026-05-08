package server

import (
	"net/http"

	"github.com/lost-coder/panvex/internal/controlplane/csrf"
)

// csrfTokenMiddleware enforces the double-submit token check on every
// state-changing request that carries an authenticated session cookie.
//
// Layered on top of csrfOriginCheck (Origin === Host) and the
// SameSite=Strict cookie attribute. Each layer is independent:
//   - SameSite=Strict      browser refuses to send the cookie cross-site
//   - csrfOriginCheck      Origin must match Host
//   - csrfTokenMiddleware  X-CSRF-Token must equal HMAC(secret, cookie)
//
// An attacker has to bypass all three to land a forged request. This
// stays in the server package because it reads request-scoped state
// (auth context + session cookie) — pure CSRF crypto lives in the
// csrf package.
func (s *Server) csrfTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isStateChangingMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		_, _, ok := requestAuthContext(r)
		if !ok {
			// No session yet (e.g. login itself). Origin check and
			// rate limiting cover unauthenticated state-changing paths.
			next.ServeHTTP(w, r)
			return
		}
		// S22 Task 5: derive the CSRF token from the *cookie value* the
		// browser sent, not from the internal Session.ID. The cookie
		// is what the panel UI also has, so the double-submit
		// comparison stays correct without exposing or relying on the
		// server-side lookup hash.
		cookieValue := readSessionCookie(r)
		if cookieValue == "" {
			writeError(w, http.StatusForbidden, "CSRF token missing or invalid")
			return
		}
		expected := s.csrfManager.TokenForSession(cookieValue)
		if !s.csrfManager.TokenMatches(r.Header.Get(csrf.TokenHeader), expected) {
			writeError(w, http.StatusForbidden, "CSRF token missing or invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isStateChangingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// handleCSRFToken returns the token for the current session. The
// frontend calls this once per page load and caches the value for the
// remainder of the session.
func (s *Server) handleCSRFToken() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, _, ok := requestAuthContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "no active session")
			return
		}
		// S22 Task 5: CSRF token is bound to the cookie value, not
		// the internal Session.ID; see csrf.TokenForSession.
		cookieValue := readSessionCookie(r)
		if cookieValue == "" {
			writeError(w, http.StatusUnauthorized, "no active session")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Token string `json:"token"`
		}{Token: s.csrfManager.TokenForSession(cookieValue)})
	}
}
