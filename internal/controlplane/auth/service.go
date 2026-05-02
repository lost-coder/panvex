package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
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
	// ErrPasswordTooWeak reports a password that is shorter than the
	// minimum length, empty, or exceeds the length cap.
	ErrPasswordTooWeak = errors.New("password does not meet the minimum length policy")
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

// HashPassword delegates to the package-level pure function. Retained as
// a method for API compatibility with existing call-sites.
func (s *Service) HashPassword(password string) (string, error) {
	return hashPassword(password)
}

// VerifyPassword delegates to the package-level pure function.
func (s *Service) VerifyPassword(hash, password string) error {
	return verifyPassword(hash, password)
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

