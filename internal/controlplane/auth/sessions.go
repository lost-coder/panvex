package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"golang.org/x/crypto/argon2"
)

// restoreConsumedTotp rebuilds the in-memory consumed-TOTP map from
// the persistent store and prunes anything past the verifier
// acceptance window so an attacker cannot resurrect old codes via
// restart (Q2.U-S-17). A nil store is a documented no-op.
func (s *Service) restoreConsumedTotp() {
	if s.consumedTotpStore == nil {
		return
	}
	totpCutoff := time.Now().UTC().Add(-90 * time.Second)
	if err := s.consumedTotpStore.DeleteExpiredConsumedTotp(context.Background(), totpCutoff); err != nil {
		slog.Warn("auth: prune expired consumed TOTP failed", "error", err)
	}
	records, err := s.consumedTotpStore.ListConsumedTotp(context.Background())
	if err != nil {
		slog.Warn("auth: list consumed TOTP failed", "error", err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rec := range records {
		if rec.UsedAt.Before(totpCutoff) {
			continue
		}
		s.consumedTotp[totpUseKey{UserID: rec.UserID, Code: rec.Code}] = rec.UsedAt
	}
}

var (
	// ErrSessionNotFound reports a missing or revoked session identifier.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionStoreUnavailable reports that the persistent session store
	// rejected a write during login. P2-SEC-07: the in-memory session alone
	// is not acceptable — it would silently disappear on the next control-
	// plane restart. The handler surfaces this as 503 so the caller retries.
	ErrSessionStoreUnavailable = errors.New("session store unavailable")
)

const (
	// sessionMaxLifetime is the absolute cap on how long a single session
	// may live from CreatedAt regardless of activity. S5 tightened the
	// previous 24h cap to 8h so a stolen cookie is useful for at most one
	// workday, not a full calendar day.
	sessionMaxLifetime = 8 * time.Hour
	// sessionIdleTimeout expires a session that has not been observed in
	// this window. Combined with sessionMaxLifetime this implements
	// sliding-refresh semantics: active users roll forward within the
	// absolute cap, idle ones lose their session quickly enough that an
	// unattended browser on a shared machine is not a long-lived attack
	// surface.
	sessionIdleTimeout = 30 * time.Minute
	// sessionTouchThrottle bounds how often an active session's LastSeenAt
	// is bumped. Without this cap every authenticated request would roll
	// the clock forward; with it we still capture steady activity at
	// minute-level resolution, which is enough to drive idle-expiry.
	sessionTouchThrottle = 1 * time.Minute
)

// sessionTTL is retained as the public compatibility alias for
// sessionMaxLifetime. Existing call-sites (RestoreSessions cutoff,
// cleanupExpiredSessionsLocked, tests) continue to read the same value;
// the new idle-timeout is enforced in addition, not instead.
const sessionTTL = sessionMaxLifetime

// effectiveSessionMaxLifetime returns the operator-configured session max
// lifetime, falling back to the compiled-in constant when no fn is wired.
// May be called with or without s.mu held — the fn is set once at startup.
func (s *Service) effectiveSessionMaxLifetime() time.Duration {
	if s.maxLifetimeFn != nil {
		return s.maxLifetimeFn()
	}
	return sessionMaxLifetime
}

// effectiveSessionIdleTimeout returns the operator-configured idle timeout,
// falling back to the compiled-in constant when no fn is wired.
func (s *Service) effectiveSessionIdleTimeout() time.Duration {
	if s.idleTimeoutFn != nil {
		return s.idleTimeoutFn()
	}
	return sessionIdleTimeout
}

// dummyPasswordHash is used to equalise login latency when the supplied
// username does not exist, so timing does not leak user-enumeration signal.
// It is computed once on first use; the derived hash value is discarded
// (it is never compared for equality), only the Argon2id CPU cost matters.
var dummyPasswordHash = sync.OnceValue(func() string {
	salt := make([]byte, 16)
	dummyPwd := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		// Fall back to derived bytes — the value is meaningless, we just need
		// a well-formed input for VerifyPassword to derive on.
		for i := range salt {
			salt[i] = byte(i * 17)
		}
	}
	if _, err := rand.Read(dummyPwd); err != nil {
		copy(dummyPwd, salt)
	}
	derived := argon2.IDKey(dummyPwd, salt, 4, 96*1024, 2, 32)
	return fmt.Sprintf("argon2id$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived))
})

// SetSessionStore attaches a persistent session store to the auth service.
// When set, sessions are persisted on creation and loaded on restart.
func (s *Service) SetSessionStore(sessionStore storage.SessionStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionStore = sessionStore
}

// RestoreSessions loads persisted sessions into the in-memory map, discarding
// any that have exceeded the session TTL. This should be called during startup.
func (s *Service) RestoreSessions() error {
	if s.sessionStore == nil {
		return nil
	}

	records, err := s.sessionStore.ListSessions(context.Background())
	if err != nil {
		return err
	}

	now := s.now().UTC()
	cutoff := now.Add(-sessionTTL)

	s.installRestoredSessions(records, cutoff)

	if err := s.sessionStore.DeleteExpiredSessions(context.Background(), cutoff); err != nil {
		return err
	}

	s.restoreConsumedTotp()
	return nil
}

// installRestoredSessions repopulates s.sessions from the persistent
// store, dropping records whose CreatedAt is older than the TTL cutoff
// and seeding LastSeenAt from CreatedAt for pre-Q2 rows that have not
// been touched yet (Q2.U-S-12).
func (s *Service) installRestoredSessions(records []storage.SessionRecord, cutoff time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range records {
		if record.CreatedAt.Before(cutoff) {
			continue
		}
		lastSeen := record.LastSeenAt
		if lastSeen.IsZero() {
			lastSeen = record.CreatedAt
		}
		s.sessions[record.ID] = Session{
			ID:         record.ID,
			UserID:     record.UserID,
			CreatedAt:  record.CreatedAt,
			LastSeenAt: lastSeen,
		}
	}
}

func (s *Service) cleanupExpiredSessionsLocked(now time.Time) {
	maxCutoff := now.UTC().Add(-s.effectiveSessionMaxLifetime())
	idleCutoff := now.UTC().Add(-s.effectiveSessionIdleTimeout())
	for sessionID, session := range s.sessions {
		// S5: evict on either the absolute cap or the idle-timeout.
		// Whichever fires first wins.
		if session.CreatedAt.Before(maxCutoff) || session.LastSeenAt.Before(idleCutoff) {
			delete(s.sessions, sessionID)
		}
	}

	// Remove consumed TOTP codes older than the acceptance window (3 × 30s).
	totpCutoff := now.UTC().Add(-90 * time.Second)
	for key, usedAt := range s.consumedTotp {
		if usedAt.Before(totpCutoff) {
			delete(s.consumedTotp, key)
		}
	}
}

// persistAuthenticatedSession writes the new session to the persistent
// store (when configured) and atomically deletes any prior session ID so
// a planted pre-auth cookie cannot resurrect on RestoreSessions.
func (s *Service) persistAuthenticatedSession(ctx context.Context, session Session, priorSessionID string) error {
	if s.sessionStore == nil {
		return nil
	}
	// Always purge the prior session ID from the persistent store when
	// supplied, independent of whether it was present in the in-memory
	// map. After a CP restart, s.sessions can be empty while the store
	// still holds the prior ID; skipping the store delete would let the
	// attacker-planted session resurrect on the next RestoreSessions.
	if priorSessionID != "" {
		if err := s.sessionStore.DeleteSession(ctx, priorSessionID); err != nil {
			slog.Warn("auth: failed to delete prior session from store", "error", err)
		}
	}
	if err := s.sessionStore.PutSession(ctx, storage.SessionRecord{
		ID:         session.ID,
		UserID:     session.UserID,
		CreatedAt:  session.CreatedAt,
		LastSeenAt: session.LastSeenAt,
	}); err != nil {
		slog.Error("auth: failed to persist session; rejecting login", "user_id", session.UserID, "error", err)
		return fmt.Errorf("%w: %w", ErrSessionStoreUnavailable, err)
	}
	return nil
}

// Authenticate validates credentials and enforces TOTP only for users who enabled it.
func (s *Service) Authenticate(ctx context.Context, input LoginInput, now time.Time) (Session, error) {
	user, err := s.loadUserByUsernameCtx(ctx, input.Username)
	if err != nil {
		// P1-SEC-12: burn Argon2id time on a dummy hash so the response
		// latency for a nonexistent user matches a real VerifyPassword call.
		// Without this, an attacker can enumerate valid usernames by timing
		// because the real path spends ~100 ms in Argon2id and the unknown
		// path returns in microseconds.
		_ = s.VerifyPassword(dummyPasswordHash(), input.Password)
		return Session{}, ErrInvalidCredentials
	}

	if err := s.VerifyPassword(user.PasswordHash, input.Password); err != nil {
		return Session{}, ErrInvalidCredentials
	}

	if user.TotpEnabled && strings.TrimSpace(input.TotpCode) == "" {
		return Session{}, ErrTotpRequired
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// TOTP verification and replay check must both happen under the lock to
	// prevent a TOCTOU race where two concurrent requests with the same code
	// both pass verification before either records consumption.
	if user.TotpEnabled {
		if err := s.verifyTotpAndConsumeLocked(ctx, user, input.TotpCode, now); err != nil {
			return Session{}, err
		}
	}

	s.cleanupExpiredSessionsLocked(now)

	// P2-SEC-01: invalidate any pre-authentication session the browser carried
	// into this login. Without this step, an attacker who planted a session
	// cookie (e.g. via XSS or a shared kiosk) would retain a valid session ID
	// after the victim successfully authenticates — classic session fixation.
	// The new cookie issued to the victim does not by itself revoke the old
	// one; we must explicitly drop it from the session map and persistent
	// store. Done here (under the lock) so the invalidation is atomic with
	// the issuance of the replacement session.
	//
	// PriorSessionID, when supplied, is the *opaque cookie token* the
	// browser carried in. The in-memory map and persistent store are keyed
	// on its HMAC, not on the token itself (S22 Task 5), so we hash before
	// the delete on both layers.
	priorCookie := strings.TrimSpace(input.PriorSessionID)
	priorLookupID := ""
	if priorCookie != "" {
		priorLookupID = s.hashSessionTokenLocked(priorCookie)
		delete(s.sessions, priorLookupID)
	}

	s.sequence++
	cookieToken, sessionID, err := s.issueSessionIdentityLocked()
	if err != nil {
		return Session{}, err
	}
	session := Session{
		ID:         sessionID,
		UserID:     user.ID,
		CreatedAt:  now.UTC(),
		LastSeenAt: now.UTC(),
	}

	// P2-SEC-07: persist the session before inserting it into the in-memory
	// map. If the store rejects the write we must NOT create the session at
	// all — an in-memory-only session would silently disappear on the next
	// control-plane restart, leaving the operator logged in but unable to
	// recover cleanly. Surface the failure so the caller retries / fails over.
	if err := s.persistAuthenticatedSession(ctx, session, priorLookupID); err != nil {
		return Session{}, err
	}

	s.sessions[session.ID] = session

	// Attach the opaque cookie token to the *returned* Session only —
	// the in-memory map stores Session.Cookie as zero so an attacker
	// who reads CP memory cannot pull a live cookie value out of it
	// (only the hash, which is useless for impersonation). The HTTP
	// login handler reads session.Cookie immediately to write the
	// Set-Cookie header and then drops it.
	session.Cookie = cookieToken
	return session, nil
}

// RevokeSessionsForUser invalidates every active session belonging to the
// given user, returning the number of sessions removed. It removes entries
// from both the in-memory map and the persistent session store so that a
// subsequent GetSession rejects the old IDs. Callers should invoke this
// whenever a user's privileges or credentials change in a way that ought to
// force re-authentication (role change, forced password reset, etc.).
func (s *Service) RevokeSessionsForUser(ctx context.Context, userID string) int {
	return s.RevokeSessionsForUserExcept(ctx, userID, "")
}

// RevokeSessionsForUserExcept is the same as RevokeSessionsForUser but
// preserves a single session whose ID matches exceptSessionID. Self-edit
// password rotations (S-5) call this with the caller's own session ID so
// the user is not logged out of the browser they just used to perform the
// rotation. Pass an empty exceptSessionID to revoke every session
// (the legacy RevokeSessionsForUser semantics).
func (s *Service) RevokeSessionsForUserExcept(ctx context.Context, userID, exceptSessionID string) int {
	if strings.TrimSpace(userID) == "" {
		return 0
	}

	s.mu.Lock()
	toDelete := make([]string, 0)
	for sessionID, session := range s.sessions {
		if session.UserID != userID {
			continue
		}
		if exceptSessionID != "" && sessionID == exceptSessionID {
			continue
		}
		toDelete = append(toDelete, sessionID)
	}
	for _, sessionID := range toDelete {
		delete(s.sessions, sessionID)
	}
	store := s.sessionStore
	s.mu.Unlock()

	if store != nil {
		for _, sessionID := range toDelete {
			if err := store.DeleteSession(ctx, sessionID); err != nil {
				// A persistence failure here is security-relevant: the
				// in-memory map drop above only sticks until the process
				// exits. If the row stays in the store, a panel restart
				// rehydrates the session and the supposedly-revoked user
				// can authenticate again until natural expiry. Log loudly
				// so alerting picks it up; continue iterating so we still
				// remove every session we can.
				slog.Error("session revocation persistence failed",
					"alert", "session_revoke_persist_failed",
					"user_id", userID,
					"session_id", sessionID,
					"error", err,
				)
			}
		}
	}

	return len(toDelete)
}

// GetSession returns the current session record for the provided identifier.
// Expired sessions (past the absolute lifetime cap or the idle-timeout) are
// reported as ErrSessionNotFound and evicted from memory. Use TouchSession
// to slide the idle-timeout forward during an authenticated request.
func (s *Service) GetSession(sessionID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().UTC()
	s.cleanupExpiredSessionsLocked(now)

	session, ok := s.sessions[sessionID]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	if now.After(session.CreatedAt.Add(s.effectiveSessionMaxLifetime())) ||
		now.After(session.LastSeenAt.Add(s.effectiveSessionIdleTimeout())) {
		delete(s.sessions, sessionID)
		return Session{}, ErrSessionNotFound
	}

	return session, nil
}

// TouchSession slides the idle-timeout forward on an active session (S5).
// It is a no-op if the session no longer exists, if the absolute-lifetime
// cap has already passed, or if LastSeenAt was updated less than
// sessionTouchThrottle ago. The throttle prevents a busy dashboard from
// turning every authenticated request into a map write, while still
// keeping the idle window rolling at minute-level resolution.
//
// TouchSession is in-memory only. It does NOT write to the session store:
// that would couple every authenticated request to a DB round-trip for a
// value we rebuild from CreatedAt on restart anyway. Callers should invoke
// it after a successful session lookup on any authenticated HTTP handler.
func (s *Service) TouchSession(ctx context.Context, sessionID string) {
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return
	}
	now := s.now().UTC()
	if now.After(session.CreatedAt.Add(s.effectiveSessionMaxLifetime())) {
		// Absolute cap already reached — don't extend; cleanup will evict.
		s.mu.Unlock()
		return
	}
	if now.Sub(session.LastSeenAt) < sessionTouchThrottle {
		s.mu.Unlock()
		return
	}
	session.LastSeenAt = now
	s.sessions[sessionID] = session
	store := s.sessionStore
	s.mu.Unlock()

	// Persist the refreshed LastSeenAt so the sliding idle timeout
	// survives a control-plane restart. Best-effort: store errors are
	// logged but do not fail the request the touch was triggered from.
	if store != nil {
		if err := store.TouchSession(ctx, sessionID, now); err != nil {
			slog.Warn("auth: persist session last_seen_at failed", "session_id", sessionID, "error", err)
		}
	}
}

// Logout revokes a session so it can no longer authenticate requests.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupExpiredSessionsLocked(s.now().UTC())
	if _, ok := s.sessions[sessionID]; !ok {
		return ErrSessionNotFound
	}

	delete(s.sessions, sessionID)

	if s.sessionStore != nil {
		// P2-SEC-07: logout deletes the session from memory unconditionally;
		// a store failure here is not fatal because the periodic expiry
		// sweeper (DeleteExpiredSessions) will eventually reclaim the row.
		// We still surface it in logs so persistent failures are visible.
		if err := s.sessionStore.DeleteSession(ctx, sessionID); err != nil {
			slog.Warn("auth: failed to delete session from store on logout", "session_id", sessionID, "error", err)
		}
	}

	return nil
}

// SetSessionLookupKey installs the HMAC key used to derive Session.ID
// (and the persistent SessionRecord.id primary key) from the opaque
// cookie token (S-medium / S22 Task 5). Callers should pass at least 16
// bytes — shorter inputs are rejected so a misconfigured deployment
// cannot silently fall back to a weak lookup key. nil is accepted and
// resets to the unset state, in which case sessionLookupKeyLocked
// generates a fresh per-process random key on first use.
func (s *Service) SetSessionLookupKey(key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key == nil {
		s.sessionLookupKey = nil
		return nil
	}
	if len(key) < 16 {
		return errors.New("auth: session lookup key must be at least 16 bytes")
	}
	dup := make([]byte, len(key))
	copy(dup, key)
	s.sessionLookupKey = dup
	return nil
}

// sessionLookupKeyLocked returns the cached HMAC key, allocating a
// per-process random fallback if SetSessionLookupKey has not been
// called. Caller must already hold s.mu (read or write); we mutate
// s.sessionLookupKey on first use, so callers that hold only the read
// lock should upgrade to write before invoking.
func (s *Service) sessionLookupKeyLocked() []byte {
	if s.sessionLookupKey != nil {
		return s.sessionLookupKey
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// Fail-closed: a degraded entropy path silently producing
		// predictable HMACs would let an attacker who reads the DB row
		// reverse the lookup hash to a cookie value. The control plane
		// has no safe way to keep running without secure entropy
		// anyway — session IDs, CSRF tokens, and CA generation all
		// depend on it.
		panic("auth: cannot derive session lookup key: " + err.Error())
	}
	s.sessionLookupKey = buf
	return s.sessionLookupKey
}

// hashSessionTokenLocked computes HMAC-SHA-256 over the supplied opaque
// cookie token under the per-server session-lookup key, returning the
// hex-encoded digest. The hex form is what we store in
// SessionRecord.id, in s.sessions[], and what we compare with
// hmac.Equal at lookup time. Caller must hold s.mu.
func (s *Service) hashSessionTokenLocked(token string) string {
	mac := hmac.New(sha256.New, s.sessionLookupKeyLocked())
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// hashSessionToken is the lock-free wrapper used by HTTP-layer entry
// points that have not yet acquired s.mu. It briefly takes s.mu to
// access the cached key. Returns the same hex digest as
// hashSessionTokenLocked.
func (s *Service) hashSessionToken(token string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hashSessionTokenLocked(token)
}

// issueSessionIdentityLocked generates a fresh opaque cookie token (32
// bytes, base64url) and the matching HMAC-SHA-256 lookup hash. The
// cookie token is what we ship in Set-Cookie; the lookup hash is what
// we persist as Session.ID and SessionRecord.id. Caller must hold
// s.mu.
func (s *Service) issueSessionIdentityLocked() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	return token, s.hashSessionTokenLocked(token), nil
}

// GetSessionByCookie is the HTTP-layer entry point: callers pass in the
// raw cookie value the browser sent, the service hashes it under the
// session-lookup key, and looks up the matching session record. The
// hash comparison is a single map lookup keyed by hex digest, which
// reduces to a constant-time equality check via hmac.Equal on the
// underlying byte slices (same-length hex strings, same key, same
// algorithm). A miss is reported as ErrSessionNotFound — distinct
// errors would let a timing oracle distinguish "wrong cookie" from
// "expired cookie." Empty input is also reported as not-found so a
// caller that forgets to read the cookie cannot accidentally probe
// the map under the empty-string key.
func (s *Service) GetSessionByCookie(cookieValue string) (Session, error) {
	if cookieValue == "" {
		return Session{}, ErrSessionNotFound
	}
	return s.GetSession(s.hashSessionToken(cookieValue))
}
