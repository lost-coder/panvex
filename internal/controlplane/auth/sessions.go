package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	maxCutoff := now.UTC().Add(-sessionMaxLifetime)
	idleCutoff := now.UTC().Add(-sessionIdleTimeout)
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
//
// Note: preferAuthenticateWithContext from request handlers so the
// underlying user-store and session-store calls inherit request cancellation.
func (s *Service) Authenticate(input LoginInput, now time.Time) (Session, error) {
	return s.AuthenticateWithContext(context.Background(), input, now)
}

// AuthenticateWithContext is the ctx-aware variant of Authenticate.
func (s *Service) AuthenticateWithContext(ctx context.Context, input LoginInput, now time.Time) (Session, error) {
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
	priorSessionID := strings.TrimSpace(input.PriorSessionID)
	if priorSessionID != "" {
		delete(s.sessions, priorSessionID)
	}

	s.sequence++
	sessionID, err := randomSessionID()
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
	if err := s.persistAuthenticatedSession(ctx, session, priorSessionID); err != nil {
		return Session{}, err
	}

	s.sessions[session.ID] = session

	return session, nil
}

// RevokeSessionsForUser invalidates every active session belonging to the
// given user, returning the number of sessions removed. It removes entries
// from both the in-memory map and the persistent session store so that a
// subsequent GetSession rejects the old IDs. Callers should invoke this
// whenever a user's privileges or credentials change in a way that ought to
// force re-authentication (role change, forced password reset, etc.).
//
// Note: preferRevokeSessionsForUserWithContext to thread request ctx.
func (s *Service) RevokeSessionsForUser(userID string) int {
	return s.RevokeSessionsForUserWithContext(context.Background(), userID)
}

// RevokeSessionsForUserWithContext is the ctx-aware variant of
// RevokeSessionsForUser.
func (s *Service) RevokeSessionsForUserWithContext(ctx context.Context, userID string) int {
	if strings.TrimSpace(userID) == "" {
		return 0
	}

	s.mu.Lock()
	toDelete := make([]string, 0)
	for sessionID, session := range s.sessions {
		if session.UserID == userID {
			toDelete = append(toDelete, sessionID)
		}
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
	if now.After(session.CreatedAt.Add(sessionMaxLifetime)) ||
		now.After(session.LastSeenAt.Add(sessionIdleTimeout)) {
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
//
// TouchSession is the legacy ctx-less entrypoint. New callers should use
// TouchSessionWithContext so the persistence side-effect inherits the
// request's cancellation budget.
func (s *Service) TouchSession(sessionID string) {
	s.TouchSessionWithContext(context.Background(), sessionID)
}

// TouchSessionWithContext is the ctx-aware variant of TouchSession.
func (s *Service) TouchSessionWithContext(ctx context.Context, sessionID string) {
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
	if now.After(session.CreatedAt.Add(sessionMaxLifetime)) {
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
//
// Note: preferLogoutWithContext from request handlers.
func (s *Service) Logout(sessionID string) error {
	return s.LogoutWithContext(context.Background(), sessionID)
}

// LogoutWithContext is the ctx-aware variant of Logout.
func (s *Service) LogoutWithContext(ctx context.Context, sessionID string) error {
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

func randomSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
