package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
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
// Two formats are accepted:
//
//	v2: "argon2id$v=2$<b64-salt>$<b64-hash>"                — 4 parts, implicit
//	    params (4 iterations / 96 MiB / 2 threads). Written by every release
//	    before the KDF profile split; verify-only from here on.
//	v3: "argon2id$v=3$t=<n>,m=<KiB>,p=<n>$<b64-salt>$<b64-hash>" — 5 parts,
//	    self-describing. Written with the active kdf profile, so switching
//	    profiles never orphans existing hashes.
//
// verifyPassword invokes Argon2id ONCE per call: the format tag selects the
// branch and the params come from the hash itself, never from trying both.
// Running two parameter sets would let an attacker classify a stored hash by
// response time (C-1).
const (
	hashSchemeArgon2id = "argon2id"
	hashVersionTagV2   = "v=2"
	hashVersionTagV3   = "v=3"

	// Legacy v2 params. Verify-only: no new hash is written with these.
	legacyIterV2   uint32 = 4
	legacyMemV2    uint32 = 96 * 1024
	legacyThreadV2 uint8  = 2

	hashKeyLen  uint32 = 32
	hashSaltLen        = 16
)

// Sanity ceilings on params read out of our OWN database. A row corrupted by
// operator error, a bad restore, or a write we did not expect must not be
// able to make the panel allocate gigabytes or spin for weeks — reject it as
// a bad credential instead. The ceilings are far above both profiles
// (default: t=4, m=96 MiB, p=2) so no legitimate hash is affected.
const (
	maxHashIter   uint32 = 16
	maxHashMemKiB uint32 = 512 * 1024
	maxHashPar    uint8  = 8
)

// hashPassword derives an Argon2id hash with the ACTIVE kdf profile and tags
// the result with the v=3 marker plus the params used, so verifyPassword can
// select parameters without running several variants (C-1) and a later
// profile change can still read it.
func hashPassword(password string) (string, error) {
	salt := make([]byte, hashSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	p := kdf.Active()
	derived := kdf.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, hashKeyLen)
	return formatHashV3(p, salt, derived), nil
}

// formatHashV3 renders the v3 wire format. Single writer so the format string
// cannot drift between hashPassword and the login dummy hash.
func formatHashV3(p kdf.Profile, salt, derived []byte) string {
	return fmt.Sprintf(
		"%s$%s$%s$%s$%s",
		hashSchemeArgon2id,
		hashVersionTagV3,
		formatHashParams(p),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	)
}

func formatHashParams(p kdf.Profile) string {
	return fmt.Sprintf("t=%d,m=%d,p=%d", p.Time, p.MemoryKiB, p.Threads)
}

// verifyPassword validates a plaintext password against an Argon2id hash in
// the v3 (self-describing) or v2 (legacy, fixed-param) format. Anything
// else — including the pre-v2 3-part format, dropped in R5 — is rejected.
// Exactly one Argon2id derivation runs per call (C-1, timing oracle).
//
// Malformed base64 and out-of-range params are mapped to
// ErrInvalidCredentials rather than a descriptive error so the error type
// does not leak hash-shape information to callers / clients.
func verifyPassword(hash, password string) error {
	parts := strings.Split(hash, "$")
	switch {
	case len(parts) == 4 && parts[0] == hashSchemeArgon2id && parts[1] == hashVersionTagV2:
		return verifyArgon2(parts[2], parts[3], password, legacyIterV2, legacyMemV2, legacyThreadV2)
	case len(parts) == 5 && parts[0] == hashSchemeArgon2id && parts[1] == hashVersionTagV3:
		iter, mem, par, err := parseHashParams(parts[2])
		if err != nil {
			return ErrInvalidCredentials
		}
		return verifyArgon2(parts[3], parts[4], password, iter, mem, par)
	default:
		return ErrInvalidCredentials
	}
}

// errBadHashParams reports a params field that is unparsable or outside the
// sanity ceilings. Never surfaced to a caller — mapped to
// ErrInvalidCredentials at the verify boundary.
var errBadHashParams = errors.New("auth: unusable argon2 parameters in stored hash")

// parseHashParams reads "t=<n>,m=<KiB>,p=<n>" and enforces the sanity
// ceilings BEFORE any derivation is attempted.
func parseHashParams(field string) (iter, mem uint32, par uint8, err error) {
	var t32, m32, p32 uint32
	// %d into uint32 rejects negatives; Sscanf also rejects trailing junk
	// only if we anchor it, so compare the re-rendered field below.
	if _, scanErr := fmt.Sscanf(field, "t=%d,m=%d,p=%d", &t32, &m32, &p32); scanErr != nil {
		return 0, 0, 0, errBadHashParams
	}
	if fmt.Sprintf("t=%d,m=%d,p=%d", t32, m32, p32) != field {
		return 0, 0, 0, errBadHashParams
	}
	if t32 == 0 || t32 > maxHashIter ||
		m32 == 0 || m32 > maxHashMemKiB ||
		p32 == 0 || p32 > uint32(maxHashPar) {
		return 0, 0, 0, errBadHashParams
	}
	return t32, m32, uint8(p32), nil
}

// needsRehash reports whether a stored hash should be rewritten with the
// active profile: true for the legacy v2 format, for a v3 hash carrying
// different params, and for anything unreadable.
func needsRehash(hash string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 5 || parts[0] != hashSchemeArgon2id || parts[1] != hashVersionTagV3 {
		return true
	}
	return parts[2] != formatHashParams(kdf.Active())
}

// verifyArgon2 decodes the salt + expected hash, runs a single Argon2id
// derivation with the supplied params, and constant-time compares. Any
// failure short of a clean match maps to ErrInvalidCredentials.
func verifyArgon2(saltB64, expectedB64, password string, iterations, memory uint32, parallelism uint8) error {
	salt, err := base64.RawStdEncoding.DecodeString(saltB64)
	if err != nil {
		return ErrInvalidCredentials
	}
	expected, err := base64.RawStdEncoding.DecodeString(expectedB64)
	if err != nil {
		return ErrInvalidCredentials
	}
	derived := kdf.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expected)))
	if subtle.ConstantTimeCompare(expected, derived) != 1 {
		return ErrInvalidCredentials
	}
	return nil
}
