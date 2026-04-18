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
	sessionTTL          = 24 * time.Hour
	maxPasswordLength   = 1024
)

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
type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
}

// totpUseKey identifies a consumed TOTP code for replay prevention.
type totpUseKey struct {
	UserID string
	Code   string
}

// Service provides local-account hashing, TOTP, and session issuance.
type Service struct {
	mu               sync.RWMutex
	sequence         uint64
	users            map[string]User
	sessions         map[string]Session
	pendingTotpSetup map[string]pendingTotpSetup
	consumedTotp     map[totpUseKey]time.Time
	userStore        storage.UserStore
	sessionStore     storage.SessionStore
	now              func() time.Time
	// startedAt records when the service was created. During the first 90
	// seconds after startup the TOTP verifier skips the past (-30s) window
	// to prevent replay of codes that may have been consumed before a restart.
	startedAt time.Time
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
		s.sessions[record.ID] = Session{
			ID:        record.ID,
			UserID:    record.UserID,
			CreatedAt: record.CreatedAt,
		}
	}

	// Clean up expired sessions in the store.
	if err := s.sessionStore.DeleteExpiredSessions(context.Background(), cutoff); err != nil {
		return err
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
func (s *Service) BootstrapUser(input BootstrapInput, now time.Time) (User, string, error) {
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

	users, err := s.userStore.ListUsers(context.Background())
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

	if err := s.userStore.PutUser(context.Background(), userToRecord(user)); err != nil {
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
	if _, err := rand.Read(salt); err != nil {
		// Fall back to a fixed salt — the value is meaningless, we just need
		// a well-formed input for VerifyPassword to derive on.
		salt = []byte("panvex-dummy-salt-bytes")
	}
	derived := argon2.IDKey([]byte("panvex-timing-dummy"), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived))
})

// Authenticate validates credentials and enforces TOTP only for users who enabled it.
func (s *Service) Authenticate(input LoginInput, now time.Time) (Session, error) {
	user, err := s.loadUserByUsername(input.Username)
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
		ID:        sessionID,
		UserID:    user.ID,
		CreatedAt: now.UTC(),
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
			if err := s.sessionStore.DeleteSession(context.Background(), priorSessionID); err != nil {
				slog.Warn("auth: failed to delete prior session from store", "error", err)
			}
		}
		if err := s.sessionStore.PutSession(context.Background(), storage.SessionRecord{
			ID:        session.ID,
			UserID:    session.UserID,
			CreatedAt: session.CreatedAt,
		}); err != nil {
			slog.Error("auth: failed to persist session; rejecting login", "user_id", session.UserID, "error", err)
			return Session{}, fmt.Errorf("%w: %v", ErrSessionStoreUnavailable, err)
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
func (s *Service) RevokeSessionsForUser(userID string) int {
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
			_ = store.DeleteSession(context.Background(), sessionID)
		}
	}

	return len(toDelete)
}

// StartTotpSetup creates a short-lived TOTP setup secret for the provided user.
func (s *Service) StartTotpSetup(userID string, now time.Time) (string, error) {
	if _, err := s.GetUserByID(userID); err != nil {
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
func (s *Service) EnableTotp(userID string, password string, totpCode string, now time.Time) (User, error) {
	user, err := s.GetUserByID(userID)
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
	if err := s.storeUser(user); err != nil {
		return User{}, err
	}

	return user, nil
}

// DisableTotp disables TOTP after validating the current password and active TOTP code.
func (s *Service) DisableTotp(userID string, password string, totpCode string, now time.Time) (User, error) {
	user, err := s.GetUserByID(userID)
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
	if err := s.storeUser(user); err != nil {
		return User{}, err
	}

	return user, nil
}

// ResetTotp clears the active TOTP configuration for the provided user.
// Callers must verify that the authenticated principal is authorized to reset TOTP for the target user.
func (s *Service) ResetTotp(userID string) (User, error) {
	user, err := s.GetUserByID(userID)
	if err != nil {
		return User{}, err
	}

	user.TotpEnabled = false
	user.TotpSecret = ""
	if err := s.storeUser(user); err != nil {
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

	derived := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
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

	derived := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, uint32(len(expected)))
	if subtle.ConstantTimeCompare(expected, derived) != 1 {
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
func (s *Service) GetUserByID(userID string) (User, error) {
	if s.userStore != nil {
		record, err := s.userStore.GetUserByID(context.Background(), userID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return User{}, ErrInvalidCredentials
			}
			return User{}, err
		}
		return userFromRecord(record), nil
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

func (s *Service) loadUserByUsername(username string) (User, error) {
	s.mu.RLock()
	userStore := s.userStore
	s.mu.RUnlock()

	if userStore != nil {
		record, err := userStore.GetUserByUsername(context.Background(), username)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return User{}, ErrInvalidCredentials
			}
			return User{}, err
		}
		return userFromRecord(record), nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[username]
	if !ok {
		return User{}, ErrInvalidCredentials
	}

	return user, nil
}

func (s *Service) storeUser(user User) error {
	if s.userStore != nil {
		if err := s.userStore.PutUser(context.Background(), userToRecord(user)); err != nil {
			return err
		}
	}

	s.mu.Lock()
	s.users[user.Username] = user
	s.mu.Unlock()

	return nil
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
	cutoff := now.UTC().Add(-sessionTTL)
	for sessionID, session := range s.sessions {
		if session.CreatedAt.Before(cutoff) {
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
func (s *Service) GetSession(sessionID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().UTC()
	s.cleanupExpiredSessionsLocked(now)

	session, ok := s.sessions[sessionID]
	if !ok {
		return Session{}, ErrSessionNotFound
	}
	if now.After(session.CreatedAt.Add(sessionTTL)) {
		delete(s.sessions, sessionID)
		return Session{}, ErrSessionNotFound
	}

	return session, nil
}

// Logout revokes a session so it can no longer authenticate requests.
func (s *Service) Logout(sessionID string) error {
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
		if err := s.sessionStore.DeleteSession(context.Background(), sessionID); err != nil {
			slog.Warn("auth: failed to delete session from store on logout", "session_id", sessionID, "error", err)
		}
	}

	return nil
}
