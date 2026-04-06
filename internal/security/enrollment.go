package security

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

var (
	// ErrEnrollmentTokenConsumed reports a replay attempt against a single-use token.
	ErrEnrollmentTokenConsumed = errors.New("enrollment token already consumed")
	// ErrEnrollmentTokenExpired reports a token that exceeded its validity window.
	ErrEnrollmentTokenExpired = errors.New("enrollment token expired")
	// ErrEnrollmentTokenInvalid reports an unknown or malformed enrollment token.
	ErrEnrollmentTokenInvalid = errors.New("enrollment token invalid")
)

// EnrollmentScope defines where a newly enrolled agent is allowed to attach.
type EnrollmentScope struct {
	FleetGroupID  string
	TTL           time.Duration
}

// EnrollmentToken stores the minted token value and the scope bound to it.
type EnrollmentToken struct {
	Value         string
	FleetGroupID  string
	IssuedAt      time.Time
	ExpiresAt     time.Time
}

type storedEnrollmentToken struct {
	token    EnrollmentToken
	consumed bool
}

// EnrollmentService issues and consumes single-use enrollment tokens.
type EnrollmentService struct {
	mu     sync.Mutex
	tokens map[string]storedEnrollmentToken
}

// NewEnrollmentService constructs an in-memory enrollment token service.
func NewEnrollmentService() *EnrollmentService {
	return &EnrollmentService{
		tokens: make(map[string]storedEnrollmentToken),
	}
}

// IssueToken mints a token for the provided scope and expiration window.
func (s *EnrollmentService) IssueToken(scope EnrollmentScope, issuedAt time.Time) (EnrollmentToken, error) {
	value, err := randomToken(32)
	if err != nil {
		return EnrollmentToken{}, err
	}

	token := EnrollmentToken{
		Value:         value,
		FleetGroupID:  scope.FleetGroupID,
		IssuedAt:      issuedAt.UTC(),
		ExpiresAt:     issuedAt.UTC().Add(scope.TTL),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cleanupExpiredLocked(issuedAt)
	s.tokens[token.Value] = storedEnrollmentToken{
		token: token,
	}

	return token, nil
}

// ConsumeToken validates the token, marks it consumed, and returns the bound scope.
func (s *EnrollmentService) ConsumeToken(value string, now time.Time) (EnrollmentToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, ok := s.tokens[value]
	if !ok {
		s.cleanupExpiredLocked(now)
		return EnrollmentToken{}, ErrEnrollmentTokenInvalid
	}

	if stored.consumed {
		s.cleanupExpiredLocked(now)
		return EnrollmentToken{}, ErrEnrollmentTokenConsumed
	}

	if now.UTC().After(stored.token.ExpiresAt) {
		delete(s.tokens, value)
		s.cleanupExpiredLocked(now)
		return EnrollmentToken{}, ErrEnrollmentTokenExpired
	}

	stored.consumed = true
	s.tokens[value] = stored
	return stored.token, nil
}

func (s *EnrollmentService) cleanupExpiredLocked(now time.Time) {
	for value, stored := range s.tokens {
		if now.UTC().After(stored.token.ExpiresAt) {
			delete(s.tokens, value)
		}
		_ = stored // consumed tokens are also cleaned once expired
	}
}

func randomToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
