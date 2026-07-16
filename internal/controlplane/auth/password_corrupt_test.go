package auth

import (
	"errors"
	"strings"
	"testing"
)

// TestVerifyPasswordRejectsCorruptedHashLengths guards the same threat model as
// the parameter sanity ceilings: a row that our own hashPassword could never
// have written (operator error, a bad restore, a planted value) must be
// rejected as a bad credential, never acted on.
//
// The derived-key length was previously taken from the stored field itself, so
// a hash whose derived part is empty asked Argon2id for a zero-length key —
// which panics inside blake2b and takes the login goroutine down. A hash with
// an over-long derived part would likewise make the panel derive a key size it
// never issues.
func TestVerifyPasswordRejectsCorruptedHashLengths(t *testing.T) {
	// A well-formed 16-byte salt; only the derived part varies below.
	const salt = "MDEyMzQ1Njc4OWFiY2RlZg"

	cases := []struct {
		name string
		hash string
	}{
		{"v2 empty derived part", "argon2id$v=2$" + salt + "$"},
		{"v3 empty derived part", "argon2id$v=3$t=4,m=98304,p=2$" + salt + "$"},
		// 64 bytes of base64 — a length hashPassword never writes (it is
		// always hashKeyLen = 32).
		{"v3 oversized derived part", "argon2id$v=3$t=2,m=19456,p=1$" + salt + "$" + strings.Repeat("A", 86)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyPassword(tc.hash, "any-password-at-all")
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("corrupted hash must be rejected as a bad credential, got %v", err)
			}
		})
	}
}
