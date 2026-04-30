package bootstrap

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"
)

const tokenSize = 32

// TokenIssued is what the issuer hands the operator (Raw, once) and persists
// (Hash + ExpiresAt) for later verification. Raw is intentionally never
// stored on the panel side — only the SHA-256 hash is.
type TokenIssued struct {
	Raw       string    // base64url; shown to operator exactly once
	Hash      [32]byte  // SHA-256 of the raw token bytes; stored in DB
	ExpiresAt time.Time
}

// Errors returned by VerifyToken. Callers can errors.Is them to distinguish
// expiry vs. malformed vs. wrong-token, e.g. for metrics or rate-limit
// decisions.
var (
	ErrTokenExpired      = errors.New("bootstrap: token expired")
	ErrTokenInvalidShape = errors.New("bootstrap: token encoding invalid")
	ErrTokenMismatch     = errors.New("bootstrap: token hash mismatch")
)

func generateToken() ([]byte, error) {
	buf := make([]byte, tokenSize)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func hashToken(raw []byte) [32]byte {
	return sha256.Sum256(raw)
}

// IssueToken generates a fresh random token, returning the raw form for the
// operator and the hash + expiry for storage. Caller chooses ttl.
func IssueToken(now time.Time, ttl time.Duration) (TokenIssued, error) {
	raw, err := generateToken()
	if err != nil {
		return TokenIssued{}, err
	}
	return TokenIssued{
		Raw:       base64.URLEncoding.EncodeToString(raw),
		Hash:      hashToken(raw),
		ExpiresAt: now.Add(ttl),
	}, nil
}

// VerifyToken returns nil iff raw decodes to the bytes whose SHA-256 equals
// expectedHash AND now is not after expiresAt. The error categories above
// are exposed so callers can tell apart "wrong token" from "your token has
// expired" without parsing strings.
func VerifyToken(raw string, expectedHash [32]byte, expiresAt time.Time, now time.Time) error {
	if now.After(expiresAt) {
		return ErrTokenExpired
	}
	decoded, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		return ErrTokenInvalidShape
	}
	// Constant-time compare to avoid timing leaks that would let an attacker
	// brute-force the hash byte-by-byte from observed reject latency.
	got := hashToken(decoded)
	if subtle.ConstantTimeCompare(got[:], expectedHash[:]) != 1 {
		return ErrTokenMismatch
	}
	return nil
}
