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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
	"golang.org/x/crypto/argon2"
)

var (
	// ErrInvalidCredentials reports a username or password mismatch.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrSessionNotFound reports a missing or revoked session identifier.
	ErrSessionNotFound = errors.New("session not found")
	// ErrTotpRequired reports a missing second factor for write-capable roles.
	ErrTotpRequired = errors.New("totp code required")
	// ErrInvalidTotpCode reports a second factor mismatch.
	ErrInvalidTotpCode = errors.New("invalid totp code")
)

// Role identifies the RBAC role assigned to a local operator account.
type Role string

const (
	// RoleViewer can inspect fleet state but cannot mutate it.
	RoleViewer Role = "viewer"
	// RoleOperator can execute control-plane commands and requires TOTP.
	RoleOperator Role = "operator"
	// RoleAdmin can manage security-sensitive settings and requires TOTP.
	RoleAdmin Role = "admin"
)

// BootstrapInput describes the initial user record to create.
type BootstrapInput struct {
	Username string
	Password string
	Role     Role
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
	TotpSecret   string
	CreatedAt    time.Time
}

// Session stores the authenticated session record returned after login.
type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
}

// Service provides local-account hashing, TOTP, and session issuance.
type Service struct {
	mu       sync.Mutex
	sequence uint64
	users    map[string]User
	sessions map[string]Session
	userStore storage.UserStore
}

// NewService constructs an in-memory local-auth service.
func NewService() *Service {
	return &Service{
		users:    make(map[string]User),
		sessions: make(map[string]Session),
	}
}

// NewServiceWithStore constructs an auth service that persists users through the shared store.
func NewServiceWithStore(userStore storage.UserStore) *Service {
	return &Service{
		users:     make(map[string]User),
		sessions:  make(map[string]Session),
		userStore: userStore,
	}
}

// BootstrapUser creates a local operator and seeds a TOTP secret when required by the role.
func (s *Service) BootstrapUser(input BootstrapInput, now time.Time) (User, string, error) {
	hash, err := s.HashPassword(input.Password)
	if err != nil {
		return User{}, "", err
	}

	secret := ""
	if requiresTotp(input.Role) {
		secret, err = randomBase32(20)
		if err != nil {
			return User{}, "", err
		}
	}

	if s.userStore == nil {
		s.mu.Lock()
		defer s.mu.Unlock()

		s.sequence++
		user := User{
			ID:           fmt.Sprintf("user-%06d", s.sequence),
			Username:     input.Username,
			PasswordHash: hash,
			Role:         input.Role,
			TotpSecret:   secret,
			CreatedAt:    now.UTC(),
		}
		s.users[input.Username] = user

		return user, secret, nil
	}

	users, err := s.userStore.ListUsers(context.Background())
	if err != nil {
		return User{}, "", err
	}

	s.mu.Lock()
	s.sequence = maxSequence(s.sequence, maxUserSequence(users))
	s.sequence++
	user := User{
		ID:           fmt.Sprintf("user-%06d", s.sequence),
		Username:     input.Username,
		PasswordHash: hash,
		Role:         input.Role,
		TotpSecret:   secret,
		CreatedAt:    now.UTC(),
	}
	s.mu.Unlock()

	if err := s.userStore.PutUser(context.Background(), userToRecord(user)); err != nil {
		return User{}, "", err
	}

	s.mu.Lock()
	s.users[input.Username] = user
	s.mu.Unlock()

	return user, secret, nil
}

// Authenticate validates credentials and enforces role-based TOTP requirements.
func (s *Service) Authenticate(input LoginInput, now time.Time) (Session, error) {
	s.mu.Lock()
	userStore := s.userStore
	s.mu.Unlock()

	var user User
	if userStore != nil {
		record, err := userStore.GetUserByUsername(context.Background(), input.Username)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return Session{}, ErrInvalidCredentials
			}
			return Session{}, err
		}
		user = userFromRecord(record)
	} else {
		s.mu.Lock()
		storedUser, ok := s.users[input.Username]
		s.mu.Unlock()
		if !ok {
			return Session{}, ErrInvalidCredentials
		}
		user = storedUser
	}

	if err := s.VerifyPassword(user.PasswordHash, input.Password); err != nil {
		return Session{}, ErrInvalidCredentials
	}

	if requiresTotp(user.Role) {
		if strings.TrimSpace(input.TotpCode) == "" {
			return Session{}, ErrTotpRequired
		}

		expected, err := s.GenerateTotpCode(user.TotpSecret, now)
		if err != nil {
			return Session{}, err
		}

		if subtle.ConstantTimeCompare([]byte(expected), []byte(input.TotpCode)) != 1 {
			return Session{}, ErrInvalidTotpCode
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sequence++
	session := Session{
		ID:        fmt.Sprintf("session-%06d", s.sequence),
		UserID:    user.ID,
		CreatedAt: now.UTC(),
	}
	s.sessions[session.ID] = session

	return session, nil
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

func requiresTotp(role Role) bool {
	return role == RoleOperator || role == RoleAdmin
}

// SnapshotUsers returns a copy of the current local-account state.
func (s *Service) SnapshotUsers() []User {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	s.mu.Lock()
	defer s.mu.Unlock()

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
		TotpSecret:   record.TotpSecret,
		CreatedAt:    record.CreatedAt.UTC(),
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

func maxSequence(left uint64, right uint64) uint64 {
	if right > left {
		return right
	}

	return left
}

// GetSession returns the current session record for the provided identifier.
func (s *Service) GetSession(sessionID string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return Session{}, ErrSessionNotFound
	}

	return session, nil
}

// Logout revokes a session so it can no longer authenticate requests.
func (s *Service) Logout(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[sessionID]; !ok {
		return ErrSessionNotFound
	}

	delete(s.sessions, sessionID)
	return nil
}
