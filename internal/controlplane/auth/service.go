package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"golang.org/x/crypto/argon2"
)

var (
	// ErrInvalidCredentials reports a username or password mismatch.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrUserNotFound reports a missing local user record.
	ErrUserNotFound = errors.New("user not found")
	// ErrUserAlreadyExists reports a duplicate local username.
	ErrUserAlreadyExists = errors.New("user already exists")
	// ErrLastAdminRequired reports an operation that would remove the last admin.
	ErrLastAdminRequired = errors.New("last admin must remain an admin")
	// ErrSessionNotFound reports a missing or revoked session identifier.
	ErrSessionNotFound = errors.New("session not found")
	// ErrTotpRequired reports a missing second factor for a TOTP-enabled account.
	ErrTotpRequired = errors.New("totp code required")
	// ErrInvalidTotpCode reports a second factor mismatch.
	ErrInvalidTotpCode = errors.New("invalid totp code")
	// ErrTotpSetupNotFound reports a missing or expired pending TOTP setup.
	ErrTotpSetupNotFound = errors.New("totp setup not found")
	// ErrPasswordTooWeak reports a password that is empty or exceeds the length cap.
	ErrPasswordTooWeak = errors.New("password must be between 1 and 1024 characters")
	// ErrSessionStoreUnavailable reports that the persistent session store
	// rejected a write during login. P2-SEC-07: the in-memory session alone
	// is not acceptable — it would silently disappear on the next control-
	// plane restart. The handler surfaces this as 503 so the caller retries.
	ErrSessionStoreUnavailable = errors.New("session store unavailable")
)

// Role identifies the RBAC role assigned to a local operator account.
type Role string

const (
	// RoleViewer can inspect fleet state but cannot mutate it.
	RoleViewer Role = "viewer"
	// RoleOperator can execute control-plane commands.
	RoleOperator Role = "operator"
	// RoleAdmin can manage security-sensitive settings.
	RoleAdmin Role = "admin"
)

const (
	pendingTotpSetupTTL = 10 * time.Minute
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
	maxPasswordLength    = 1024
)

// sessionTTL is retained as the public compatibility alias for
// sessionMaxLifetime. Existing call-sites (RestoreSessions cutoff,
// cleanupExpiredSessionsLocked, tests) continue to read the same value;
// the new idle-timeout is enforced in addition, not instead.
const sessionTTL = sessionMaxLifetime

// validatePassword only enforces a sanity cap so that pathological inputs
// cannot stall the password hasher. No complexity rules — operators pick
// whatever password they feel comfortable with. An empty password is still
// refused because the row must carry a hash.
func validatePassword(password string) error {
	if password == "" || len(password) > maxPasswordLength {
		return ErrPasswordTooWeak
	}
	return nil
}

// BootstrapInput describes the initial user record to create.
type BootstrapInput struct {
	Username string
	Password string
	Role     Role
}

// UpdateUserInput describes the mutable fields for one existing local user.
type UpdateUserInput struct {
	UserID      string
	Username    string
	Role        Role
	NewPassword string
}

// LoginInput describes the operator credentials submitted during login.
type LoginInput struct {
	Username string
	Password string
	TotpCode string
	// PriorSessionID, if non-empty, identifies a pre-authentication session
	// cookie that the browser carried into the login request. On successful
	// authentication the service invalidates this ID in both the in-memory map
	// and the persistent session store before issuing a fresh session. This
	// defeats session fixation (an attacker who planted a cookie pre-login
	// cannot continue to use that ID after the victim authenticates).
	PriorSessionID string
}

// User stores the local operator identity.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	Role         Role
	TotpEnabled  bool
	TotpSecret   string
	CreatedAt    time.Time
}

// Session stores the authenticated session record returned after login.
//
// LastSeenAt is an in-memory sliding-refresh timestamp (S5). It is not
// persisted to storage: on control-plane restart we reload the session
// from SessionRecord and seed LastSeenAt = CreatedAt. This is a small
// privacy/correctness trade-off: the idle-timeout resets once across a
// restart rather than leaking a precise activity timestamp into the
// audit-visible SessionRecord table, and still meets the S5 goal of
// shrinking a stolen cookie's window.
type Session struct {
	ID         string
	UserID     string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// totpUseKey identifies a consumed TOTP code for replay prevention.
type totpUseKey struct {
	UserID string
	Code   string
}

// Service provides local-account hashing, TOTP, and session issuance.
type Service struct {
	mu                 sync.RWMutex
	sequence           uint64
	users              map[string]User
	sessions           map[string]Session
	pendingTotpSetup   map[string]pendingTotpSetup
	consumedTotp       map[totpUseKey]time.Time
	userStore          storage.UserStore
	sessionStore       storage.SessionStore
	consumedTotpStore  storage.ConsumedTotpStore
	vault              *secretvault.Vault
	now                func() time.Time
	// startedAt records when the service was created. During the first 90
	// seconds after startup the TOTP verifier skips the past (-30s) window
	// to prevent replay of codes that may have been consumed before a restart.
	startedAt time.Time
}

// SetVault wires the at-rest encryption vault. Called by Server during
// construction so TOTP secrets are encrypted before being persisted.
// A nil or disabled vault keeps legacy plaintext behaviour.
func (s *Service) SetVault(vault *secretvault.Vault) {
	s.mu.Lock()
	s.vault = vault
	s.mu.Unlock()
}

// SetConsumedTotpStore wires the persistent replay-prevention store
// (Q2.U-S-17). Once set, every consumed TOTP code is mirrored to the
// store and the in-memory map is rebuilt from it on RestoreSessions.
func (s *Service) SetConsumedTotpStore(store storage.ConsumedTotpStore) {
	s.mu.Lock()
	s.consumedTotpStore = store
	s.mu.Unlock()
}

// persistConsumedTotpAsync mirrors a freshly consumed (user_id, code)
// pair to the configured store. Best-effort: a store error is logged
// but does not fail the auth flow that triggered the consume.
func (s *Service) persistConsumedTotp(ctx context.Context, userID, code string, usedAt time.Time) {
	store := s.consumedTotpStore
	if store == nil {
		return
	}
	if err := store.UpsertConsumedTotp(ctx, storage.ConsumedTotpRecord{
		UserID: userID,
		Code:   code,
		UsedAt: usedAt.UTC(),
	}); err != nil {
		slog.Warn("auth: persist consumed TOTP failed", "user_id", userID, "error", err)
	}
}

// encryptTotp returns the storage form of the TOTP secret, applying
// vault encryption when configured. Empty values pass through.
func (s *Service) encryptTotp(value string) (string, error) {
	if value == "" {
		return value, nil
	}
	if s.vault == nil || !s.vault.Enabled() {
		return value, nil
	}
	if secretvault.IsEncrypted(value) {
		return value, nil
	}
	return s.vault.Encrypt(secretvault.DomainTOTP, value)
}

// decryptTotp reverses encryptTotp. Plaintext rows from before the
// vault was enabled are returned unchanged so existing users keep
// working until they next rotate their TOTP secret.
func (s *Service) decryptTotp(value string) (string, error) {
	if value == "" || !secretvault.IsEncrypted(value) {
		return value, nil
	}
	if s.vault == nil || !s.vault.Enabled() {
		return "", errors.New("auth: encrypted TOTP secret present but vault is disabled")
	}
	return s.vault.Decrypt(secretvault.DomainTOTP, value)
}

// NewService constructs an in-memory local-auth service.
func NewService() *Service {
	return &Service{
		users:            make(map[string]User),
		sessions:         make(map[string]Session),
		pendingTotpSetup: make(map[string]pendingTotpSetup),
		consumedTotp:     make(map[totpUseKey]time.Time),
		now:              time.Now,
		startedAt:        time.Now(),
	}
}

// NewServiceWithStore constructs an auth service that persists users through the shared store.
func NewServiceWithStore(userStore storage.UserStore) *Service {
	return &Service{
		users:            make(map[string]User),
		sessions:         make(map[string]Session),
		pendingTotpSetup: make(map[string]pendingTotpSetup),
		consumedTotp:     make(map[totpUseKey]time.Time),
		userStore:        userStore,
		now:              time.Now,
		startedAt:        time.Now(),
	}
}

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

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range records {
		if record.CreatedAt.Before(cutoff) {
			continue
		}
		// Q2.U-S-12: LastSeenAt is now persisted via TouchSession; if a
		// pre-Q2 row carries a zero LastSeenAt we still seed from
		// CreatedAt so the idle-timeout has a sane reference point.
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

	// Clean up expired sessions in the store.
	if err := s.sessionStore.DeleteExpiredSessions(context.Background(), cutoff); err != nil {
		return err
	}

	// Q2.U-S-17: rebuild the in-memory consumed-TOTP map from the
	// persistent store and prune anything past the verifier acceptance
	// window so an attacker cannot resurrect old codes via restart.
	if s.consumedTotpStore != nil {
		totpCutoff := time.Now().UTC().Add(-90 * time.Second)
		if err := s.consumedTotpStore.DeleteExpiredConsumedTotp(context.Background(), totpCutoff); err != nil {
			slog.Warn("auth: prune expired consumed TOTP failed", "error", err)
		}
		records, err := s.consumedTotpStore.ListConsumedTotp(context.Background())
		if err != nil {
			slog.Warn("auth: list consumed TOTP failed", "error", err)
		} else {
			for _, rec := range records {
				if rec.UsedAt.Before(totpCutoff) {
					continue
				}
				s.consumedTotp[totpUseKey{UserID: rec.UserID, Code: rec.Code}] = rec.UsedAt
			}
		}
	}

	return nil
}

// SetNow overrides the clock used for time-sensitive auth checks.
func (s *Service) SetNow(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now == nil {
		s.now = time.Now
		return
	}
	s.now = now
	// Reset the startup timestamp so the TOTP grace period is relative to
	// the injected clock, not to wall-clock time. Without this, tests using
	// a synthetic clock would hit the post-restart replay guard incorrectly.
	s.startedAt = now()
}

type pendingTotpSetup struct {
	Secret    string
	CreatedAt time.Time
}

// BootstrapUser creates a local user with TOTP disabled by default.
//
// Note: callers that have a request context should use
// BootstrapUserWithContext to propagate cancellation through the user store.
// This wrapper stays for CLI bootstrap and tests where no request ctx exists.
func (s *Service) BootstrapUser(input BootstrapInput, now time.Time) (User, string, error) {
	return s.BootstrapUserWithContext(context.Background(), input, now)
}

// BootstrapUserWithContext is the ctx-aware variant of BootstrapUser.
func (s *Service) BootstrapUserWithContext(ctx context.Context, input BootstrapInput, now time.Time) (User, string, error) {
	if err := validatePassword(input.Password); err != nil {
		return User{}, "", err
	}

	hash, err := s.HashPassword(input.Password)
	if err != nil {
		return User{}, "", err
	}

	if s.userStore == nil {
		s.mu.Lock()
		defer s.mu.Unlock()

		id, err := randomUserID()
		if err != nil {
			return User{}, "", err
		}
		s.sequence++
		user := User{
			ID:           id,
			Username:     input.Username,
			PasswordHash: hash,
			Role:         input.Role,
			TotpEnabled:  false,
			TotpSecret:   "",
			CreatedAt:    now.UTC(),
		}
		s.users[input.Username] = user

		return user, "", nil
	}

	users, err := s.userStore.ListUsers(ctx)
	if err != nil {
		return User{}, "", err
	}

	s.mu.Lock()
	s.sequence = maxSequence(s.sequence, maxUserSequence(users))
	s.sequence++
	id, err := randomUserID()
	if err != nil {
		s.mu.Unlock()
		return User{}, "", err
	}
	user := User{
		ID:           id,
		Username:     input.Username,
		PasswordHash: hash,
		Role:         input.Role,
		TotpEnabled:  false,
		TotpSecret:   "",
		CreatedAt:    now.UTC(),
	}

	bootstrapRecord := userToRecord(user)
	if encrypted, encErr := s.encryptTotp(bootstrapRecord.TotpSecret); encErr != nil {
		s.sequence--
		s.mu.Unlock()
		return User{}, "", encErr
	} else {
		bootstrapRecord.TotpSecret = encrypted
	}
	if err := s.userStore.PutUser(ctx, bootstrapRecord); err != nil {
		s.sequence--
		s.mu.Unlock()
		return User{}, "", err
	}
	s.users[input.Username] = user
	s.mu.Unlock()

	return user, "", nil
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

	if user.TotpEnabled {
		if strings.TrimSpace(input.TotpCode) == "" {
			return Session{}, ErrTotpRequired
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// TOTP verification and replay check must both happen under the lock to
	// prevent a TOCTOU race where two concurrent requests with the same code
	// both pass verification before either records consumption.
	if user.TotpEnabled {
		key := totpUseKey{UserID: user.ID, Code: strings.TrimSpace(input.TotpCode)}
		if _, used := s.consumedTotp[key]; used {
			return Session{}, ErrInvalidTotpCode
		}
		if !s.verifyTotpCode(user.TotpSecret, input.TotpCode, now) {
			return Session{}, ErrInvalidTotpCode
		}
		s.consumedTotp[key] = now.UTC()
		// Mirror to the persistent store outside the lock so a CP restart
		// cannot let the same code be re-used inside the verifier
		// acceptance window. Detach the request context so an early
		// client disconnect does not abort the persist write.
		bgCtx := context.WithoutCancel(ctx)
		go s.persistConsumedTotp(bgCtx, key.UserID, key.Code, now.UTC())
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
	if s.sessionStore != nil {
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
			return Session{}, fmt.Errorf("%w: %w", ErrSessionStoreUnavailable, err)
		}
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

// StartTotpSetup creates a short-lived TOTP setup secret for the provided user.
//
// Note: preferStartTotpSetupWithContext from request handlers.
func (s *Service) StartTotpSetup(userID string, now time.Time) (string, error) {
	return s.StartTotpSetupWithContext(context.Background(), userID, now)
}

// StartTotpSetupWithContext is the ctx-aware variant of StartTotpSetup.
func (s *Service) StartTotpSetupWithContext(ctx context.Context, userID string, now time.Time) (string, error) {
	if _, err := s.GetUserByIDWithContext(ctx, userID); err != nil {
		return "", err
	}

	secret, err := randomBase32(20)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupPendingTotpSetupLocked(now)
	s.pendingTotpSetup[userID] = pendingTotpSetup{
		Secret:    secret,
		CreatedAt: now.UTC(),
	}

	return secret, nil
}

// EnableTotp validates a pending setup and persists it as the active user TOTP secret.
//
// Note: preferEnableTotpWithContext from request handlers.
func (s *Service) EnableTotp(userID string, password string, totpCode string, now time.Time) (User, error) {
	return s.EnableTotpWithContext(context.Background(), userID, password, totpCode, now)
}

// EnableTotpWithContext is the ctx-aware variant of EnableTotp.
func (s *Service) EnableTotpWithContext(ctx context.Context, userID string, password string, totpCode string, now time.Time) (User, error) {
	user, err := s.GetUserByIDWithContext(ctx, userID)
	if err != nil {
		return User{}, err
	}

	if err := s.VerifyPassword(user.PasswordHash, password); err != nil {
		return User{}, ErrInvalidCredentials
	}

	if strings.TrimSpace(totpCode) == "" {
		return User{}, ErrTotpRequired
	}

	// Hold the lock through setup lookup and TOTP verification to prevent a
	// concurrent StartTotpSetup from replacing the pending secret between the
	// lookup and the code check (TOCTOU).
	s.mu.Lock()
	s.cleanupPendingTotpSetupLocked(now)
	setup, ok := s.pendingTotpSetup[userID]
	if !ok {
		s.mu.Unlock()
		return User{}, ErrTotpSetupNotFound
	}
	if !s.verifyTotpCode(setup.Secret, totpCode, now) {
		s.mu.Unlock()
		return User{}, ErrInvalidTotpCode
	}
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

	user.TotpEnabled = true
	user.TotpSecret = setup.Secret
	if err := s.storeUserWithContext(ctx, user); err != nil {
		return User{}, err
	}

	return user, nil
}

// DisableTotp disables TOTP after validating the current password and active TOTP code.
//
// Note: preferDisableTotpWithContext from request handlers.
func (s *Service) DisableTotp(userID string, password string, totpCode string, now time.Time) (User, error) {
	return s.DisableTotpWithContext(context.Background(), userID, password, totpCode, now)
}

// DisableTotpWithContext is the ctx-aware variant of DisableTotp.
func (s *Service) DisableTotpWithContext(ctx context.Context, userID string, password string, totpCode string, now time.Time) (User, error) {
	user, err := s.GetUserByIDWithContext(ctx, userID)
	if err != nil {
		return User{}, err
	}

	if err := s.VerifyPassword(user.PasswordHash, password); err != nil {
		return User{}, ErrInvalidCredentials
	}

	trimmedCode := strings.TrimSpace(totpCode)
	if trimmedCode == "" {
		return User{}, ErrTotpRequired
	}

	// P2-SEC-08: hold the lock across the replay check, TOTP verification,
	// and the consumed-code record so two concurrent DisableTotp calls with
	// the same valid code cannot both pass. Without this, an attacker who
	// acquires one valid code can race the legitimate operator to disable
	// their second factor. Mirrors the pattern used in Authenticate.
	s.mu.Lock()
	s.cleanupExpiredSessionsLocked(now)
	key := totpUseKey{UserID: user.ID, Code: trimmedCode}
	if _, used := s.consumedTotp[key]; used {
		s.mu.Unlock()
		return User{}, ErrInvalidTotpCode
	}
	if !s.verifyTotpCode(user.TotpSecret, trimmedCode, now) {
		s.mu.Unlock()
		return User{}, ErrInvalidTotpCode
	}
	s.consumedTotp[key] = now.UTC()
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

	user.TotpEnabled = false
	user.TotpSecret = ""
	if err := s.storeUserWithContext(ctx, user); err != nil {
		return User{}, err
	}

	return user, nil
}

// ResetTotp clears the active TOTP configuration for the provided user.
// Callers must verify that the authenticated principal is authorized to reset TOTP for the target user.
//
// Note: preferResetTotpWithContext from request handlers.
func (s *Service) ResetTotp(userID string) (User, error) {
	return s.ResetTotpWithContext(context.Background(), userID)
}

// ResetTotpWithContext is the ctx-aware variant of ResetTotp.
func (s *Service) ResetTotpWithContext(ctx context.Context, userID string) (User, error) {
	user, err := s.GetUserByIDWithContext(ctx, userID)
	if err != nil {
		return User{}, err
	}

	user.TotpEnabled = false
	user.TotpSecret = ""
	if err := s.storeUserWithContext(ctx, user); err != nil {
		return User{}, err
	}

	s.mu.Lock()
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

	return user, nil
}

// HashPassword derives an Argon2id hash suitable for local credential storage.
func (s *Service) HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	// Q3.U-S-16: lift Argon2id parameters above the OWASP minimum.
	// 4 iters / 96 MiB / 2 threads is the recommended cost for
	// high-trust password hashing in 2026. VerifyPassword infers the
	// caller's parameters from the stored hash, so existing 3/64 MiB
	// hashes keep working until the user next rotates the password.
	derived := argon2.IDKey([]byte(password), salt, 4, 96*1024, 2, 32)
	return fmt.Sprintf(
		"argon2id$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	), nil
}

// VerifyPassword validates a plaintext password against an Argon2id hash.
func (s *Service) VerifyPassword(hash string, password string) error {
	parts := strings.Split(hash, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return ErrInvalidCredentials
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return err
	}

	expected, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return err
	}

	// Q3.U-S-16: try the current parameters first, then the legacy
	// 3/64 MiB tuple so any pre-bump hashes still authenticate. New
	// rotations land on the strong parameters via HashPassword.
	derived := argon2.IDKey([]byte(password), salt, 4, 96*1024, 2, uint32(len(expected)))
	if subtle.ConstantTimeCompare(expected, derived) == 1 {
		return nil
	}
	legacy := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, uint32(len(expected)))
	if subtle.ConstantTimeCompare(expected, legacy) != 1 {
		return ErrInvalidCredentials
	}

	return nil
}

// GenerateTotpCode derives a standard 30-second TOTP code from the stored secret.
func (s *Service) GenerateTotpCode(secret string, at time.Time) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(secret))
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		return "", err
	}

	counter := uint64(at.UTC().Unix() / 30)
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)

	mac := hmac.New(sha1.New, decoded)
	if _, err := mac.Write(msg[:]); err != nil {
		return "", err
	}

	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0F
	value := (int(sum[offset])&0x7F)<<24 |
		int(sum[offset+1])<<16 |
		int(sum[offset+2])<<8 |
		int(sum[offset+3])
	code := value % 1_000_000

	return fmt.Sprintf("%06d", code), nil
}

func randomBase32(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

func randomSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// SnapshotUsers returns a copy of the current local-account state.
func (s *Service) SnapshotUsers() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]User, 0, len(s.users))
	for _, user := range s.users {
		result = append(result, user)
	}

	return result
}

// LoadUsers replaces the current local-account state with the provided users.
func (s *Service) LoadUsers(users []User) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.users = make(map[string]User, len(users))
	for _, user := range users {
		s.users[user.Username] = user
	}
}

// GetUserByID returns the user record that owns the provided identifier.
//
// Note: preferGetUserByIDWithContext from request handlers.
func (s *Service) GetUserByID(userID string) (User, error) {
	return s.GetUserByIDWithContext(context.Background(), userID)
}

// GetUserByIDWithContext is the ctx-aware variant of GetUserByID.
func (s *Service) GetUserByIDWithContext(ctx context.Context, userID string) (User, error) {
	if s.userStore != nil {
		record, err := s.userStore.GetUserByID(ctx, userID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return User{}, ErrInvalidCredentials
			}
			return User{}, err
		}
		return s.userFromStoredRecord(record)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, user := range s.users {
		if user.ID == userID {
			return user, nil
		}
	}

	return User{}, ErrInvalidCredentials
}

func userToRecord(user User) storage.UserRecord {
	return storage.UserRecord{
		ID:           user.ID,
		Username:     user.Username,
		PasswordHash: user.PasswordHash,
		Role:         string(user.Role),
		TotpEnabled:  user.TotpEnabled,
		TotpSecret:   user.TotpSecret,
		CreatedAt:    user.CreatedAt.UTC(),
	}
}

func userFromRecord(record storage.UserRecord) User {
	return User{
		ID:           record.ID,
		Username:     record.Username,
		PasswordHash: record.PasswordHash,
		Role:         Role(record.Role),
		TotpEnabled:  record.TotpEnabled,
		TotpSecret:   record.TotpSecret,
		CreatedAt:    record.CreatedAt.UTC(),
	}
}

func (s *Service) loadUserByUsernameCtx(ctx context.Context, username string) (User, error) {
	s.mu.RLock()
	userStore := s.userStore
	s.mu.RUnlock()

	if userStore != nil {
		record, err := userStore.GetUserByUsername(ctx, username)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return User{}, ErrInvalidCredentials
			}
			return User{}, err
		}
		return s.userFromStoredRecord(record)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[username]
	if !ok {
		return User{}, ErrInvalidCredentials
	}

	return user, nil
}

func (s *Service) storeUserWithContext(ctx context.Context, user User) error {
	if s.userStore != nil {
		record := userToRecord(user)
		encrypted, err := s.encryptTotp(record.TotpSecret)
		if err != nil {
			return err
		}
		record.TotpSecret = encrypted
		if err := s.userStore.PutUser(ctx, record); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.users[user.Username] = user
	s.mu.Unlock()

	return nil
}

// userFromStoredRecord wraps userFromRecord with TOTP decryption. Used
// by all paths that load a user from the userStore.
func (s *Service) userFromStoredRecord(record storage.UserRecord) (User, error) {
	plaintext, err := s.decryptTotp(record.TotpSecret)
	if err != nil {
		return User{}, err
	}
	record.TotpSecret = plaintext
	return userFromRecord(record), nil
}

func (s *Service) cleanupPendingTotpSetupLocked(now time.Time) {
	cutoff := now.UTC().Add(-pendingTotpSetupTTL)
	for userID, setup := range s.pendingTotpSetup {
		if setup.CreatedAt.Before(cutoff) {
			delete(s.pendingTotpSetup, userID)
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

func (s *Service) verifyTotpCode(secret string, code string, now time.Time) bool {
	trimmedCode := strings.TrimSpace(code)
	// During the first 90 seconds after startup, skip the past (-30s) window
	// to prevent replay of TOTP codes consumed before a restart. The consumed
	// code map is in-memory only, so after a restart an attacker could replay
	// a code that was used just before shutdown.
	elapsed := now.Sub(s.startedAt)
	skipPastWindow := elapsed >= 0 && elapsed < 90*time.Second
	for _, candidateTime := range []time.Time{now.Add(-30 * time.Second), now, now.Add(30 * time.Second)} {
		if skipPastWindow && candidateTime.Before(s.startedAt) {
			continue
		}
		expected, err := s.GenerateTotpCode(secret, candidateTime)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(trimmedCode)) == 1 {
			return true
		}
	}
	return false
}

func maxUserSequence(users []storage.UserRecord) uint64 {
	var maxValue uint64
	for _, user := range users {
		if !strings.HasPrefix(user.ID, "user-") {
			continue
		}
		value, err := strconv.ParseUint(strings.TrimPrefix(user.ID, "user-"), 10, 64)
		if err != nil {
			continue
		}
		if value > maxValue {
			maxValue = value
		}
	}

	return maxValue
}

func maxSequence(left uint64, right uint64) uint64 {
	if right > left {
		return right
	}

	return left
}

func randomUserID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "user-" + hex.EncodeToString(buf), nil
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
