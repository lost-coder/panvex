package auth

import (
	"errors"
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
	// ErrPasswordCommonlyBreached reports a password that appears on the
	// embedded common-breached denylist. Returned on password set / change
	// paths only; existing logins still authenticate so legitimate users
	// can rotate their credential without being locked out (S-medium).
	ErrPasswordCommonlyBreached = errors.New("password matches a commonly breached password and cannot be used")
	// ErrCurrentPasswordRequired reports a self-edit password change that
	// did not include the caller's current password. Self-edits must
	// re-prove possession of the current credential before rotating it
	// (S-5): otherwise a hijacked session can rotate the password without
	// challenge, locking the legitimate user out.
	ErrCurrentPasswordRequired = errors.New("current password is required to change own password")
	// ErrCurrentPasswordIncorrect reports a self-edit password change in
	// which the supplied current password did not match the stored hash.
	// Distinct from ErrInvalidCredentials so the caller can return a
	// specific 401 without confusing it with a login failure.
	ErrCurrentPasswordIncorrect = errors.New("current password is incorrect")
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
	users            map[string]User
	usersByID        map[string]User
	sessions         map[string]Session
	pendingTotpSetup map[string]pendingTotpSetup
	consumedTotp     map[totpUseKey]time.Time
	userStore        storage.UserStore
	sessionStore     storage.SessionStore
	consumedTotpStore storage.ConsumedTotpStore
	vault            *secretvault.Vault
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

// HashPassword delegates to the package-level pure function. Retained as
// a method for API compatibility with existing call-sites.
func (s *Service) HashPassword(password string) (string, error) {
	return hashPassword(password)
}

// VerifyPassword delegates to the package-level pure function.
func (s *Service) VerifyPassword(hash, password string) error {
	return verifyPassword(hash, password)
}
