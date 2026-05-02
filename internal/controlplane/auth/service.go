package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
	// ErrPasswordTooWeak reports a password that is shorter than the
	// minimum length, empty, or exceeds the length cap.
	ErrPasswordTooWeak = errors.New("password does not meet the minimum length policy")
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

// Service provides local-account hashing, TOTP, and session issuance.
type Service struct {
	mu       sync.RWMutex
	sequence uint64
	// users is keyed by username for the login lookup path. usersByID is
	// the matching reverse index keyed by user.ID so GetUserByID stays
	// O(1) in the no-store fallback (M-16). Mutators use the
	// putUserLocked / deleteUserLocked helpers to keep both maps in
	// sync — never write to either map directly.
	users      map[string]User
	usersByID  map[string]User
	sessions   map[string]Session
	pendingTotpSetup   map[string]pendingTotpSetup
	consumedTotp       map[totpUseKey]time.Time
	userStore          storage.UserStore
	sessionStore       storage.SessionStore
	consumedTotpStore  storage.ConsumedTotpStore
	vault              *secretvault.Vault
	// passwordPolicy is the operator-configured minimum length, mirrored
	// from panel_settings. Zero means "use the compiled-in default".
	// Guarded by s.mu like other mutable Service state.
	passwordPolicy int32
	now            func() time.Time
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

// SetPasswordPolicy mirrors the operator policy from panel_settings into
// the service. Called at startup (after PanelSettings load) and on every
// admin-driven update of the policy. (S-01)
func (s *Service) SetPasswordPolicy(minLength int32) {
	s.mu.Lock()
	s.passwordPolicy = minLength
	s.mu.Unlock()
}

// passwordMinLength returns the operator-configured minimum length, or
// zero if no policy has been loaded yet. Callers feed this into
// effectivePolicy, which maps zero to the compiled-in default.
func (s *Service) passwordMinLength() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int(s.passwordPolicy)
}

// putUserLocked installs (or updates) a user record in both indices.
// Caller must hold s.mu for write. previousUsername (when non-empty)
// removes the stale name->record entry left over from a username
// rename so the username index does not double-up.
func (s *Service) putUserLocked(user User, previousUsername string) {
	if previousUsername != "" && previousUsername != user.Username {
		delete(s.users, previousUsername)
	}
	s.users[user.Username] = user
	if user.ID != "" {
		s.usersByID[user.ID] = user
	}
}

// deleteUserLocked removes a user from both indices. Caller must hold
// s.mu for write.
func (s *Service) deleteUserLocked(user User) {
	delete(s.users, user.Username)
	if user.ID != "" {
		delete(s.usersByID, user.ID)
	}
}

// NewService constructs an in-memory local-auth service.
func NewService() *Service {
	return &Service{
		users:            make(map[string]User),
		usersByID:        make(map[string]User),
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
		usersByID:        make(map[string]User),
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
	if err := validatePassword(input.Password, s.passwordMinLength()); err != nil {
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
		s.putUserLocked(user, "")

		return user, "", nil
	}

	users, err := s.userStore.ListUsers(ctx)
	if err != nil {
		return User{}, "", err
	}

	// M-17: do not hold s.mu across the userStore.PutUser round-trip.
	// The previous form blocked every other auth-flow caller for a full
	// DB RTT. Now we (a) reserve the next sequence/ID under the lock,
	// (b) drop the lock, (c) hit the store, (d) re-take the lock only
	// to install the row. Failure paths re-take the lock briefly to
	// roll the sequence back so concurrent bootstraps never re-use an
	// allocated ID.
	id, err := randomUserID()
	if err != nil {
		return User{}, "", err
	}

	s.mu.Lock()
	s.sequence = maxSequence(s.sequence, maxUserSequence(users))
	s.sequence++
	s.mu.Unlock()

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
	encrypted, encErr := s.encryptTotp(bootstrapRecord.TotpSecret)
	if encErr != nil {
		s.mu.Lock()
		s.sequence--
		s.mu.Unlock()
		return User{}, "", encErr
	}
	bootstrapRecord.TotpSecret = encrypted

	if err := s.userStore.PutUser(ctx, bootstrapRecord); err != nil {
		s.mu.Lock()
		s.sequence--
		s.mu.Unlock()
		return User{}, "", err
	}

	s.mu.Lock()
	s.putUserLocked(user, "")
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

// HashPassword delegates to the package-level pure function. Retained as
// a method for API compatibility with existing call-sites.
func (s *Service) HashPassword(password string) (string, error) {
	return hashPassword(password)
}

// VerifyPassword delegates to the package-level pure function.
func (s *Service) VerifyPassword(hash, password string) error {
	return verifyPassword(hash, password)
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
	s.usersByID = make(map[string]User, len(users))
	for _, user := range users {
		s.putUserLocked(user, "")
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

	if user, ok := s.usersByID[userID]; ok {
		return user, nil
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
	s.putUserLocked(user, "")
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

func maxSequence(left, right uint64) uint64 {
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
