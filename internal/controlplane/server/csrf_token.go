package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
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

// csrfSecretStoreKey is the cp_secrets row used to persist the CSRF
// HMAC seed across restarts (Q2.U-S-24). Stable on purpose: every
// restart recovers the same value so in-flight panel forms remain
// valid.
const csrfSecretStoreKey = "csrf_secret_v1"

// loadOrCreateCSRFSecret returns the persisted CSRF secret, generating
// and persisting a fresh one if no row exists or no store is wired.
// Best-effort persistence: a write failure logs but does not abort
// startup — the panel keeps running with the in-memory secret.
func loadOrCreateCSRFSecret(store storage.Store) ([]byte, error) {
	if store != nil {
		ctx := context.Background()
		existing, err := store.GetCPSecret(ctx, csrfSecretStoreKey)
		if err == nil && len(existing) == csrfSecretBytes {
			return existing, nil
		}
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			slog.Warn("control-plane: load CSRF secret failed; minting fresh", "error", err)
		}
		fresh, freshErr := newCSRFSecret()
		if freshErr != nil {
			return nil, freshErr
		}
		if putErr := store.PutCPSecret(ctx, csrfSecretStoreKey, fresh); putErr != nil {
			slog.Warn("control-plane: persist CSRF secret failed", "error", putErr)
		}
		return fresh, nil
	}
	return newCSRFSecret()
}

// vaultHKDFSaltStoreKey is the cp_secrets row used to persist the
// per-install HKDF salt that the secretvault binds its domain keys to.
// Stable name — once written it must keep the same row so legacy
// PVS2-encrypted values stay decryptable across restarts.
const vaultHKDFSaltStoreKey = "vault_hkdf_salt_v1"

// loadOrCreateVaultSalt returns the persisted per-install HKDF salt,
// generating and persisting a fresh one if no row exists. A store
// without an existing row plus a write failure means the operator
// would lose the only path to decrypt later writes — so we fail loud
// rather than silently fall back to the legacy hard-coded salt.
func loadOrCreateVaultSalt(store storage.Store) ([]byte, error) {
	if store == nil {
		// No store wired (in-memory dev/tests). Mint a transient salt
		// — values encrypted in this process won't survive a restart,
		// which matches the no-store contract elsewhere.
		fresh := make([]byte, 32)
		if _, err := rand.Read(fresh); err != nil {
			return nil, err
		}
		return fresh, nil
	}
	ctx := context.Background()
	existing, err := store.GetCPSecret(ctx, vaultHKDFSaltStoreKey)
	if err == nil && len(existing) >= 16 {
		return existing, nil
	}
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return nil, fmt.Errorf("load vault HKDF salt: %w", err)
	}
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, fmt.Errorf("mint vault HKDF salt: %w", err)
	}
	if err := store.PutCPSecret(ctx, vaultHKDFSaltStoreKey, fresh); err != nil {
		return nil, fmt.Errorf("persist vault HKDF salt: %w", err)
	}
	return fresh, nil
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
