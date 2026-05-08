// Package csrf owns the double-submit CSRF token primitives used by
// the panel HTTP layer. Splitting this off keeps the server package
// from being the home of every cryptographic helper — the HTTP
// transport glue (middleware, handler) stays in server/ and consumes
// the Manager type defined here.
//
// The token is a stable HMAC of the session-cookie value: any holder
// of the cookie can rederive the token, but an attacker who only sees
// a cross-site request can't forge one without the per-server secret.
// The secret persists across restarts via the cp_secrets table so
// in-flight panel forms stay valid across a panel rolling restart.
package csrf

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// SecretBytes is the size of the per-server HMAC secret used to derive
// CSRF tokens. 32 bytes = full SHA-256 output, more than enough to make
// brute-forcing a token computationally infeasible.
const SecretBytes = 32

// TokenHeader is the request header the panel UI populates on
// state-changing requests. Match against the HMAC of the session
// cookie value is the double-submit guarantee.
const TokenHeader = "X-CSRF-Token"

// secretStoreKey is the cp_secrets row used to persist the CSRF HMAC
// seed across restarts. Stable on purpose: every restart recovers the
// same value so in-flight panel forms remain valid.
const secretStoreKey = "csrf_secret_v1"

// NewSecret produces a fresh random secret. Used both at startup (when
// no persisted row exists) and by tests that want a deterministic
// fixture.
func NewSecret() ([]byte, error) {
	buf := make([]byte, SecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// LoadOrCreateSecret returns the persisted CSRF secret, generating
// and persisting a fresh one if no row exists or no store is wired.
// Best-effort persistence: a write failure logs but does not abort
// startup — the panel keeps running with the in-memory secret.
//
// ctx is the boot-time lifecycle context (Server.serverCtx) so a
// Close() during a wedged GetCPSecret/PutCPSecret aborts the storage
// call instead of leaking it past shutdown.
func LoadOrCreateSecret(ctx context.Context, store storage.Store, logger *slog.Logger) ([]byte, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if store == nil {
		return NewSecret()
	}
	existing, err := store.GetCPSecret(ctx, secretStoreKey)
	if err == nil && len(existing) == SecretBytes {
		return existing, nil
	}
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		logger.Warn("control-plane: load CSRF secret failed; minting fresh", "error", err)
	}
	fresh, freshErr := NewSecret()
	if freshErr != nil {
		return nil, freshErr
	}
	if putErr := store.PutCPSecret(ctx, secretStoreKey, fresh); putErr != nil {
		logger.Warn("control-plane: persist CSRF secret failed", "error", putErr)
	}
	return fresh, nil
}

// TokenForSession derives a stable, opaque CSRF token from the
// session-cookie value. HMAC means the token cannot be forged without
// the per-server secret; basing it on the cookie value means the
// token is bound to exactly that browser cookie — rotating the cookie
// rotates the token.
//
// The argument is named cookieValue (not sessionID) because Session.ID
// is the HMAC-of-cookie DB primary key — the CSRF token must stay
// derivable from a value the *client* possesses, so we hash the raw
// cookie value.
func TokenForSession(cookieValue string, secret []byte) string {
	h := hmac.New(sha256.New, secret)
	h.Write([]byte(cookieValue))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// TokenMatches is constant-time: subtle.ConstantTimeCompare avoids
// leaking the byte position of the first mismatching character via
// timing. Returns false when either string is empty so the zero
// header value never accidentally validates.
func TokenMatches(supplied, expected string) bool {
	if supplied == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(supplied), []byte(expected)) == 1
}

// Manager bundles the per-server CSRF secret with its logger. The
// Server holds one of these and the HTTP transport glue (middleware,
// handler) calls through it instead of reaching for a raw []byte
// field on Server. Logger is held for diagnostics — currently only
// the loader uses it, but middleware extensions can borrow it without
// a constructor change.
type Manager struct {
	Secret []byte
	Logger *slog.Logger
}

// NewManager loads or mints the persistent CSRF secret and wraps it.
// The returned Manager is safe for concurrent use — Secret is set
// once at construction and never mutated.
func NewManager(ctx context.Context, store storage.Store, logger *slog.Logger) (*Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}
	secret, err := LoadOrCreateSecret(ctx, store, logger)
	if err != nil {
		return nil, err
	}
	return &Manager{Secret: secret, Logger: logger}, nil
}

// TokenForSession is the Manager-bound form of the package-level
// helper: callers that already hold a Manager don't need to thread
// the raw secret through.
func (m *Manager) TokenForSession(cookieValue string) string {
	return TokenForSession(cookieValue, m.Secret)
}

// TokenMatches mirrors the package-level helper for symmetry with
// TokenForSession; constant-time comparison does not actually depend
// on the secret, but having both methods on Manager lets call-sites
// stay consistent.
func (*Manager) TokenMatches(supplied, expected string) bool {
	return TokenMatches(supplied, expected)
}
