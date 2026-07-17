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

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// restoreConsumedTotp rebuilds the in-memory consumed-TOTP map from
// the persistent store and prunes anything past the verifier
// acceptance window so an attacker cannot resurrect old codes via
// restart (Q2.U-S-17). A nil store is a documented no-op.
// ctx is the lifecycle context of the caller (serverCtx / Background in tests).
func (s *Service) restoreConsumedTotp(ctx context.Context) {
	if s.consumedTotpStore == nil {
		return
	}
	// Use the service clock (s.now), not the wall clock: the verifier's 90s
	// acceptance window is computed against s.startedAt = now(), so the restore
	// cutoff must agree with it. A wall-clock cutoff diverges whenever the
	// clock is injected (tests, replays) and would drop just-consumed records
	// as "expired", reopening the replay window the persist was meant to close.
	totpCutoff := s.now().UTC().Add(-90 * time.Second)
	if err := s.consumedTotpStore.DeleteExpiredConsumedTotp(ctx, totpCutoff); err != nil {
		slog.Warn("auth: prune expired consumed TOTP failed", "error", err)
	}
	records, err := s.consumedTotpStore.ListConsumedTotp(ctx)
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

// dummyPasswordHash returns a throwaway hash used to equalise login latency
// when the supplied username does not exist, so timing does not leak a
// user-enumeration signal. Its stored bytes are never compared for equality —
// only the Argon2id cost of VERIFYING against them matters — so it is filled
// with random bytes rather than derived. Building it therefore costs no
// derivation, and the unknown-user path costs exactly what a real user's failed
// login costs: one.
//
// CRITICAL: this MUST emit the SAME format and SAME Argon2id params as the
// current hashPassword in password.go, i.e. the active profile's. Otherwise
// verifyPassword routes the dummy through a different branch (a cheaper
// parameter set, or a rejected format that derives nothing at all) and the
// unknown-user path burns measurably less CPU than the real-user path —
// reopening the user-enumeration timing oracle this dummy exists to close
// (C-1 follow-up). Pinned by TestDummyPasswordHashMatchesCurrentFormat and
// TestDummyPasswordHashCostsNoDerivationToBuild.
func dummyPasswordHash() string {
	p := kdf.Active()
	salt := make([]byte, hashSaltLen)
	derived := make([]byte, hashKeyLen)
	if _, err := rand.Read(salt); err != nil {
		// The values are meaningless — we only need a well-formed input for
		// VerifyPassword to derive against — so a dead entropy source must not
		// take the login path down with it.
		for i := range salt {
			salt[i] = byte(i * 17)
		}
	}
	if _, err := rand.Read(derived); err != nil {
		copy(derived, salt)
	}
	return formatHashV3(p, salt, derived)
}

// rehashPasswordIfStale rewrites a verified credential with the active KDF
// profile when the stored hash does not already use it.
//
// Best-effort by design: the caller has already proved possession of the
// password, so a failure to derive or persist the new hash must not fail the
// login — the old hash stays valid and the next login retries. Callers MUST
// only reach this after a successful VerifyPassword.
//
// The write goes through storeUserWithContext (whole-record PutUser), the
// same last-writer-wins path UpdateUser uses; there is no optimistic lock on
// user rows. A rehash therefore races an admin edit of the same user landing
// in the same instant. The window is one login, it only fires while a user's
// hash is stale (i.e. once per user after a profile change), and both writers
// are already racy today — so this does not add a new class of problem.
func (s *Service) rehashPasswordIfStale(ctx context.Context, user User, password string) {
	if !needsRehash(user.PasswordHash) {
		return
	}
	newHash, err := hashPassword(password)
	if err != nil {
		slog.WarnContext(ctx, "auth: password rehash failed", "user_id", user.ID, "error", err)
		return
	}
	user.PasswordHash = newHash
	if err := s.storeUserWithContext(ctx, user); err != nil {
		slog.WarnContext(ctx, "auth: password rehash persist failed", "user_id", user.ID, "error", err)
	}
}

// SetSessionStore attaches a persistent session store to the auth service.
// When set, sessions are persisted on creation and loaded on restart.
func (s *Service) SetSessionStore(sessionStore SessionStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionStore = sessionStore
}

// RestoreSessions loads persisted sessions into the in-memory map, discarding
// any that have exceeded the configured session max lifetime. This should be
// called during startup.
// ctx is the lifecycle context of the caller (serverCtx / Background in tests);
// a cancelled ctx aborts the restore so a Close() during boot does not hang.
func (s *Service) RestoreSessions(ctx context.Context) error {
	if s.sessionStore == nil {
		return nil
	}

	records, err := s.sessionStore.ListSessions(ctx)
	if err != nil {
		return err
	}

	now := s.now().UTC()
	// R1.3 (audit 2026-07-07 §1.4): honour the operator-configured max
	// lifetime; the compiled-in constant is only the fallback inside
	// effectiveSessionMaxLifetime when no fn is wired.
	cutoff := now.Add(-s.effectiveSessionMaxLifetime())

	s.installRestoredSessions(records, cutoff)

	if err := s.sessionStore.DeleteExpiredSessions(ctx, cutoff); err != nil {
		return err
	}

	s.restoreConsumedTotp(ctx)
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

// StartSessionCleanupWorker sweeps expired sessions and consumed-TOTP codes on
// a ticker until ctx is cancelled. The sweep is O(sessions + consumed codes)
// under the write lock, which is why it does NOT run on the request path any
// more: it used to fire on every GetSession — i.e. on every authenticated HTTP
// request — serialising the whole fleet's requests behind a full map scan.
//
// Expiry itself is not deferred to this worker. GetSession still rejects and
// evicts an expired session the moment it is asked for one; the worker only
// reclaims the memory of sessions nobody asks about any more.
//
// The caller owns wg.Add(1); the worker Done()s on exit.
func (s *Service) StartSessionCleanupWorker(ctx context.Context, interval time.Duration, wg *sync.WaitGroup) {
	if interval <= 0 {
		interval = time.Minute
	}
	go func() {
		if wg != nil {
			defer wg.Done()
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.mu.Lock()
				s.cleanupExpiredSessionsLocked(s.now().UTC())
				s.mu.Unlock()
			}
		}
	}()
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

	// Migrate the credential to the active KDF profile now that we hold the
	// plaintext and know it is correct. Without this, hashes written before
	// the profile split (or under a previous profile) keep their old
	// parameters forever and a `low-memory` panel still pays 96 MiB on every
	// login — the one derivation that repeats. Only successful logins reach
	// here, so a guesser cannot force the extra work.
	s.rehashPasswordIfStale(ctx, user, input.Password)

	if user.TotpEnabled && strings.TrimSpace(input.TotpCode) == "" {
		return Session{}, ErrTotpRequired
	}

	s.mu.Lock()

	// TOTP verification and replay check must both happen under the lock to
	// prevent a TOCTOU race where two concurrent requests with the same code
	// both pass verification before either records consumption.
	if user.TotpEnabled {
		if err := s.verifyTotpAndConsumeLocked(ctx, user, input.TotpCode, now); err != nil {
			s.mu.Unlock()
			return Session{}, err
		}
	}

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

	cookieToken, sessionID, err := s.issueSessionIdentityLocked()
	if err != nil {
		s.mu.Unlock()
		return Session{}, err
	}
	session := Session{
		ID:         sessionID,
		UserID:     user.ID,
		CreatedAt:  now.UTC(),
		LastSeenAt: now.UTC(),
	}
	s.mu.Unlock()

	// The store write happens OUTSIDE s.mu. It is a DB round-trip, and holding
	// the lock across it stalled every concurrent GetSession — that is, every
	// authenticated request in flight — for the duration of a login's disk I/O.
	//
	// Dropping the lock here is safe because nothing can reach the new session
	// yet: its cookie has not left this function. The prior session, if any, is
	// already gone from the map (deleted above, under the lock), so a failed
	// persist leaves exactly the state it left before — no prior session, no
	// new one — and the login is rejected.
	//
	// P2-SEC-07: persist BEFORE publishing the session in memory. If the store
	// rejects the write we must NOT create the session at all — an
	// in-memory-only session would silently disappear on the next control-plane
	// restart, leaving the operator logged in but unable to recover cleanly.
	if err := s.persistAuthenticatedSession(ctx, session, priorLookupID); err != nil {
		return Session{}, err
	}

	s.mu.Lock()
	s.sessions[session.ID] = session
	s.mu.Unlock()

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
// given user, returning the number of sessions actually removed and an
// aggregated error if any store deletion failed. It removes entries from
// both the in-memory map and the persistent session store so that a
// subsequent GetSession rejects the old IDs. Callers should invoke this
// whenever a user's privileges or credentials change in a way that ought to
// force re-authentication (role change, forced password reset, etc.), and
// MUST NOT report success to their own caller while discarding a non-nil
// error here (PVX-002): a store-delete failure means a session survives in
// the persistent store and will resurrect on the next restore, so callers on
// a fail-closed path (credential rotation, TOTP reset, account deletion)
// must propagate it.
func (s *Service) RevokeSessionsForUser(ctx context.Context, userID string) (int, error) {
	return s.RevokeSessionsForUserExcept(ctx, userID, "")
}

// RevokeSessionsForUserExcept is the same as RevokeSessionsForUser but
// preserves a single session whose ID matches exceptSessionID. Self-edit
// password rotations (S-5) call this with the caller's own session ID so
// the user is not logged out of the browser they just used to perform the
// rotation. Pass an empty exceptSessionID to revoke every session
// (the legacy RevokeSessionsForUser semantics).
//
// Fail-closed consistency (PVX-002 / D1): a session is dropped from the
// in-memory map ONLY after its persistent-store row has been deleted
// successfully. A session whose store delete failed is deliberately LEFT in
// memory — dropping it anyway would make the in-memory map (the only
// remaining index of "which sessions belong to this user") forget about the
// orphan row, so a retry of this same call would find nothing to delete and
// the row would be unreachable forever. Keeping it means memory and store
// stay in agreement, the failure is reported, and a retry can genuinely
// re-attempt the delete. The returned int counts only successful deletions;
// errors from every failed deletion are aggregated with errors.Join so a
// caller inspecting the error with errors.Is/errors.As sees all of them.
func (s *Service) RevokeSessionsForUserExcept(ctx context.Context, userID, exceptSessionID string) (int, error) {
	if strings.TrimSpace(userID) == "" {
		return 0, nil
	}

	s.mu.Lock()
	candidates := make([]string, 0)
	for sessionID, session := range s.sessions {
		if session.UserID != userID {
			continue
		}
		if exceptSessionID != "" && sessionID == exceptSessionID {
			continue
		}
		candidates = append(candidates, sessionID)
	}
	store := s.sessionStore
	s.mu.Unlock()

	if len(candidates) == 0 {
		return 0, nil
	}

	// Store I/O happens OUTSIDE s.mu (D5): only the candidate list is built
	// under the lock. Deleted IDs are collected here and removed from the
	// in-memory map in a single follow-up locked pass below.
	deleted := make([]string, 0, len(candidates))
	var errs []error
	for _, sessionID := range candidates {
		if store != nil {
			if err := store.DeleteSession(ctx, sessionID); err != nil && !errors.Is(err, storage.ErrNotFound) {
				// A persistence failure here is security-relevant: see the
				// fail-closed rationale on the function doc comment. Log
				// loudly so alerting picks it up; continue iterating so we
				// still remove every session we genuinely can, and leave
				// this one in memory rather than dropping it.
				//
				// storage.ErrNotFound is deliberately NOT treated as a
				// failure here: it means the row is already gone (a
				// concurrent Logout, the expiry sweeper, or — the common
				// case — the "sessions.user_id ... ON DELETE CASCADE" FK in
				// the schema already removing every session row when
				// DeleteUser deletes the user row first). The invariant
				// this function exists to guarantee is "the row does not
				// survive in the store", and ErrNotFound already proves
				// that, so treating it as an error would fail DeleteUser
				// (and any other caller racing a legitimate concurrent
				// deletion) for a condition that is actually success.
				slog.ErrorContext(ctx, "session revocation persistence failed",
					"alert", "session_revoke_persist_failed",
					"user_id", userID,
					"session_id", sessionID,
					"error", err,
				)
				errs = append(errs, fmt.Errorf("delete session %s: %w", sessionID, err))
				continue
			}
		}
		deleted = append(deleted, sessionID)
	}

	if len(deleted) > 0 {
		s.mu.Lock()
		for _, sessionID := range deleted {
			delete(s.sessions, sessionID)
		}
		s.mu.Unlock()
	}

	return len(deleted), errors.Join(errs...)
}

// GetSession returns the current session record for the provided identifier.
// Expired sessions (past the absolute lifetime cap or the idle-timeout) are
// reported as ErrSessionNotFound and evicted from memory. Use TouchSession
// to slide the idle-timeout forward during an authenticated request.
func (s *Service) GetSession(sessionID string) (Session, error) {
	// Hot path: every authenticated HTTP request lands here. It takes the READ
	// lock and touches exactly one map entry — no full-map sweep (that moved to
	// StartSessionCleanupWorker) and no write lock unless this particular
	// session turns out to be expired.
	s.mu.RLock()
	now := s.now().UTC()
	session, ok := s.sessions[sessionID]
	expired := ok && (now.After(session.CreatedAt.Add(s.effectiveSessionMaxLifetime())) ||
		now.After(session.LastSeenAt.Add(s.effectiveSessionIdleTimeout())))
	s.mu.RUnlock()

	if !ok {
		return Session{}, ErrSessionNotFound
	}
	if expired {
		// Evict immediately — an expired session must not be usable while it
		// waits for the sweeper.
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()
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
//
// Fail-closed consistency (PVX-002 / D1), same shape as
// RevokeSessionsForUserExcept: the session is dropped from the in-memory map
// ONLY after the persistent-store delete is confirmed (or the store reports
// storage.ErrNotFound, which is positive proof — a DELETE by primary key
// with zero rows affected — that the row is already gone, not a race). A
// genuine store failure leaves the session in memory and returns the error,
// so a caller can retry and actually find it again, instead of the previous
// unconditional-drop behavior making a retry a no-op.
//
// P2-SEC-07 (rewritten): the old comment here claimed a failed store delete
// was safe because "the periodic expiry sweeper will eventually reclaim the
// row." That does not hold: DeleteExpiredSessions is `DELETE FROM sessions
// WHERE created_at_unix < ?` — it only reclaims rows past the ABSOLUTE
// session lifetime, not "sometime soon." So a row that survives a failed
// logout delete stays resurrectable for up to the full session lifetime
// (sessionMaxLifetime), which is exactly the exposure window this fix
// closes. Composed with the rest of PVX-002: if this function still dropped
// the session from memory unconditionally, a later password rotation on the
// same user would see zero in-memory candidates for that session,
// RevokeSessionsForUserExcept would report success trivially, and the
// surviving row would authenticate again after a control-plane restart
// despite the rotation — the same class of bug this whole plan exists to
// close, just reached through Logout instead of RevokeSessionsForUserExcept
// directly.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	// D5: only the membership check happens under the lock; store I/O runs
	// outside it, and the map entry is dropped in a second, separate locked
	// pass — mirrors RevokeSessionsForUserExcept's lock discipline instead
	// of holding s.mu across the DeleteSession round-trip as the previous
	// implementation (a single `defer s.mu.Unlock()` spanning the store
	// call) did.
	s.mu.RLock()
	_, ok := s.sessions[sessionID]
	store := s.sessionStore
	s.mu.RUnlock()

	if !ok {
		return ErrSessionNotFound
	}

	if store != nil {
		if err := store.DeleteSession(ctx, sessionID); err != nil && !errors.Is(err, storage.ErrNotFound) {
			slog.ErrorContext(ctx, "session revocation persistence failed",
				"alert", "session_revoke_persist_failed",
				"session_id", sessionID,
				"error", err,
			)
			return fmt.Errorf("delete session %s: %w", sessionID, err)
		}
	}

	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()

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
