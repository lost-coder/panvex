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

// hashPassword derives an Argon2id hash with the project's hardened
// parameters (4 iters, 96 MiB, 2 threads). Moved from service.go so
// password concerns live in one file.
func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	derived := argon2.IDKey([]byte(password), salt, 4, 96*1024, 2, 32)
	return fmt.Sprintf(
		"argon2id$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	), nil
}

// verifyPassword validates a plaintext password against an Argon2id hash,
// trying current parameters first and falling back to legacy 3/64 MiB so
// pre-bump hashes keep authenticating until the user rotates.
func verifyPassword(hash, password string) error {
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
	derived := argon2.IDKey([]byte(password), salt, 4, 96*1024, 2, uint32(len(expected)))
	if subtle.ConstantTimeCompare(expected, derived) == 1 {
		return nil
	}
	legacy := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, uint32(len(expected)))
	if subtle.ConstantTimeCompare(expected, legacy) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}
