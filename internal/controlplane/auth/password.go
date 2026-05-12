package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// DefaultPasswordMinLength is the in-binary fallback when the operator
	// has not configured a policy. Mirrors NIST SP 800-63B "memorized secret"
	// floor for admin-tier accounts (S-01).
	DefaultPasswordMinLength = 10
	// PasswordMinLengthFloor is the absolute minimum we ever accept,
	// regardless of operator configuration. Defends the in-memory layer
	// against a misconfigured caller bypassing the DB CHECK constraint.
	PasswordMinLengthFloor = 8
	// maxPasswordLength caps input so pathological values cannot stall
	// the Argon2id hasher.
	maxPasswordLength = 1024
)

// effectivePolicy returns the enforced minimum length for an operator-
// configured value. Zero (or negative) is treated as "no operator opinion"
// and returns DefaultPasswordMinLength. Positive values below
// PasswordMinLengthFloor are clamped to PasswordMinLengthFloor. Values
// above maxPasswordLength are clamped to maxPasswordLength.
//
// Task 3 storage layer note: panel_settings rows created by migration 0032
// have DEFAULT 10 — those round-trip as 10 (== DefaultPasswordMinLength)
// here. In-memory zero (before settings load) maps to the same default.
// Both states behave identically to "no operator override".
func effectivePolicy(operatorMin int) int {
	if operatorMin <= 0 {
		return DefaultPasswordMinLength
	}
	if operatorMin < PasswordMinLengthFloor {
		return PasswordMinLengthFloor
	}
	if operatorMin > maxPasswordLength {
		return maxPasswordLength
	}
	return operatorMin
}

// validatePassword enforces the configured length policy and the embedded
// common-breached denylist. There are no character-class checks — Argon2id
// + per-account lockout cover online brute force, while NIST SP 800-63B
// explicitly recommends against composition rules. See AUDIT_2026-05-01 §S-01.
//
// The denylist applies to ALL users on set / change paths (Task 7,
// S-medium) because any breached password is unsafe regardless of role.
// Existing logins are not affected — verify paths do not call this.
func validatePassword(password string, operatorMin int) error {
	minLen := effectivePolicy(operatorMin)
	if len(password) < minLen || len(password) > maxPasswordLength {
		return ErrPasswordTooWeak
	}
	if err := validatePasswordAgainstDenylist(password); err != nil {
		return err
	}
	return nil
}

// Hash format constants.
//
// Legacy (pre-C-1 fix): "argon2id$<b64-salt>$<b64-hash>" — 3 parts,
// Argon2id with 3 iterations and 64 MiB memory.
//
// Current (v2): "argon2id$v=2$<b64-salt>$<b64-hash>" — 4 parts,
// Argon2id with 4 iterations and 96 MiB memory.
//
// verifyPassword selects the parameter set from the hash structure and
// invokes Argon2id ONCE per call — never falls through to a second
// parameter set. This eliminates the timing oracle where a correct
// password against a legacy hash took ~2x the CPU of a correct password
// against a current hash (which let an attacker classify hash age and
// focus brute-force attempts).
const (
	hashSchemeArgon2id = "argon2id"
	hashVersionTagV2   = "v=2"

	hashIterLegacy uint32 = 3
	hashMemLegacy  uint32 = 64 * 1024
	hashIterV2     uint32 = 4
	hashMemV2      uint32 = 96 * 1024

	hashParallelism uint8  = 2
	hashKeyLen      uint32 = 32
	hashSaltLen            = 16
)

// hashPassword derives an Argon2id hash with the project's hardened
// parameters (4 iters, 96 MiB, 2 threads) and tags the result with the
// v=2 format marker so verifyPassword can select parameters without
// running both variants (C-1).
func hashPassword(password string) (string, error) {
	salt := make([]byte, hashSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := argon2.IDKey([]byte(password), salt, hashIterV2, hashMemV2, hashParallelism, hashKeyLen)
	return fmt.Sprintf(
		"%s$%s$%s$%s",
		hashSchemeArgon2id,
		hashVersionTagV2,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	), nil
}

// verifyPassword validates a plaintext password against an Argon2id
// hash. Parameter selection is driven by the stored hash's structure,
// not by trial: a 3-part hash is verified against legacy params, a
// 4-part v=2 hash against current params, anything else is rejected.
// Exactly one Argon2id derivation runs per call (C-1, timing oracle).
//
// Malformed base64 is mapped to ErrInvalidCredentials rather than the
// raw decoding error so the error type does not leak hash-shape
// information to callers / clients.
func verifyPassword(hash, password string) error {
	parts := strings.Split(hash, "$")
	switch {
	case len(parts) == 3 && parts[0] == hashSchemeArgon2id:
		// Legacy: argon2id$salt$hash — 3 iters, 64 MiB.
		return verifyArgon2(parts[1], parts[2], password, hashIterLegacy, hashMemLegacy)
	case len(parts) == 4 && parts[0] == hashSchemeArgon2id && parts[1] == hashVersionTagV2:
		// Current: argon2id$v=2$salt$hash — 4 iters, 96 MiB.
		return verifyArgon2(parts[2], parts[3], password, hashIterV2, hashMemV2)
	default:
		return ErrInvalidCredentials
	}
}

// verifyArgon2 decodes the salt + expected hash, runs a single Argon2id
// derivation with the supplied params, and constant-time compares. Any
// failure short of a clean match maps to ErrInvalidCredentials.
func verifyArgon2(saltB64, expectedB64, password string, iterations, memory uint32) error {
	salt, err := base64.RawStdEncoding.DecodeString(saltB64)
	if err != nil {
		return ErrInvalidCredentials
	}
	expected, err := base64.RawStdEncoding.DecodeString(expectedB64)
	if err != nil {
		return ErrInvalidCredentials
	}
	derived := argon2.IDKey([]byte(password), salt, iterations, memory, hashParallelism, uint32(len(expected)))
	if subtle.ConstantTimeCompare(expected, derived) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}
