package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
)

// csrfSecretBytes is the size of the per-server HMAC secret used to
// derive CSRF tokens. 32 bytes = full SHA-256 output, more than enough
// to make brute-forcing a token computationally infeasible.
const csrfSecretBytes = 32

// csrfTokenHeader is the request header the panel UI must populate on
// state-changing requests. Match for the cookie value (recovered from
// the session cookie via the per-server secret) is the double-submit
// guarantee.
const csrfTokenHeader = "X-CSRF-Token"

// newCSRFSecret produces a fresh random secret. Called once at Server
// construction; on restart every client transparently re-fetches via
// /api/auth/csrf-token.
func newCSRFSecret() ([]byte, error) {
	buf := make([]byte, csrfSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// csrfTokenForSession derives a stable, opaque CSRF token from the
// session ID. HMAC means the token cannot be forged without the
// per-server secret; basing it on session ID means the token is bound
// to exactly that session — rotating the cookie rotates the token.
func csrfTokenForSession(sessionID string, secret []byte) string {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(sessionID))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// csrfTokenMatches is constant-time: subtle.ConstantTimeCompare avoids
// leaking the byte position of the first mismatching character via
// timing. Returns false when either string is empty so the zero
// header value never accidentally validates.
func csrfTokenMatches(supplied, expected string) bool {
	if supplied == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) == 1
}

// csrfTokenMiddleware enforces the double-submit token check on every
// state-changing request that carries an authenticated session cookie.
//
// Layered on top of csrfOriginCheck (Origin === Host) and the
// SameSite=Strict cookie attribute. Each layer is independent:
//   - SameSite=Strict      browser refuses to send the cookie cross-site
//   - csrfOriginCheck      Origin must match Host
//   - csrfTokenMiddleware  X-CSRF-Token must equal HMAC(secret, session.ID)
//
// An attacker has to bypass all three to land a forged request.
func (s *Server) csrfTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isStateChangingMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		session, _, ok := requestAuthContext(r)
		if !ok {
			// No session yet (e.g. login itself). Origin check and
			// rate limiting cover unauthenticated state-changing paths.
			next.ServeHTTP(w, r)
			return
		}
		expected := csrfTokenForSession(session.ID, s.csrfSecret)
		if !csrfTokenMatches(r.Header.Get(csrfTokenHeader), expected) {
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
		session, _, ok := requestAuthContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "no active session")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Token string `json:"token"`
		}{Token: csrfTokenForSession(session.ID, s.csrfSecret)})
	}
}
