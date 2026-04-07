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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/panvex/panvex/internal/controlplane/storage"
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
	// ErrPasswordTooWeak reports a password that does not meet minimum requirements.
	ErrPasswordTooWeak = errors.New("password must be at least 12 characters with uppercase, lowercase, and a digit")
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
	minPasswordLength = 12
)

// validatePasswordComplexity requires at least 12 characters with a mix of
// uppercase, lowercase, and digits.
func validatePasswordComplexity(password string) error {
	if len(password) < minPasswordLength {
		return ErrPasswordTooWeak
	}
	var hasUpper, hasLower, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
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
	now              func() time.Time
}

// NewService constructs an in-memory local-auth service.
func NewService() *Service {
	return &Service{
		users:            make(map[string]User),
		sessions:         make(map[string]Session),
		pendingTotpSetup: make(map[string]pendingTotpSetup),
		consumedTotp:     make(map[totpUseKey]time.Time),
		now:              time.Now,
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
}

type pendingTotpSetup struct {
	Secret    string
	CreatedAt time.Time
}

// BootstrapUser creates a local user with TOTP disabled by default.
func (s *Service) BootstrapUser(input BootstrapInput, now time.Time) (User, string, error) {
	if err := validatePasswordComplexity(input.Password); err != nil {
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

// Authenticate validates credentials and enforces TOTP only for users who enabled it.
func (s *Service) Authenticate(input LoginInput, now time.Time) (Session, error) {
	user, err := s.loadUserByUsername(input.Username)
	if err != nil {
		return Session{}, err
	}

	if err := s.VerifyPassword(user.PasswordHash, input.Password); err != nil {
		return Session{}, ErrInvalidCredentials
	}

	if user.TotpEnabled {
		if strings.TrimSpace(input.TotpCode) == "" {
			return Session{}, ErrTotpRequired
		}

		if !s.verifyTotpCode(user.TotpSecret, input.TotpCode, now) {
			return Session{}, ErrInvalidTotpCode
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Reject replayed TOTP codes within the acceptance window.
	if user.TotpEnabled {
		key := totpUseKey{UserID: user.ID, Code: strings.TrimSpace(input.TotpCode)}
		if _, used := s.consumedTotp[key]; used {
			return Session{}, ErrInvalidTotpCode
		}
		s.consumedTotp[key] = now.UTC()
	}

	s.cleanupExpiredSessionsLocked(now)
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
	s.sessions[session.ID] = session

	return session, nil
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

	s.mu.Lock()
	s.cleanupPendingTotpSetupLocked(now)
	setup, ok := s.pendingTotpSetup[userID]
	s.mu.Unlock()
	if !ok {
		return User{}, ErrTotpSetupNotFound
	}

	if !s.verifyTotpCode(setup.Secret, totpCode, now) {
		return User{}, ErrInvalidTotpCode
	}

	user.TotpEnabled = true
	user.TotpSecret = setup.Secret
	if err := s.storeUser(user); err != nil {
		return User{}, err
	}

	s.mu.Lock()
	delete(s.pendingTotpSetup, userID)
	s.mu.Unlock()

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

	if strings.TrimSpace(totpCode) == "" {
		return User{}, ErrTotpRequired
	}

	if !s.verifyTotpCode(user.TotpSecret, totpCode, now) {
		return User{}, ErrInvalidTotpCode
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
	for _, candidateTime := range []time.Time{now.Add(-30 * time.Second), now, now.Add(30 * time.Second)} {
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
	return nil
}
