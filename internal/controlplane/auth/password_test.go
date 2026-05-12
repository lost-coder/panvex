package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestValidatePassword_DefaultMinIs10(t *testing.T) {
	t.Parallel()
	if err := validatePassword("Tr0ub4dor", 0); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("9-char password under default policy: got err=%v, want ErrPasswordTooWeak", err)
	}
	if err := validatePassword("Tr0ub4dor3", 0); err != nil {
		t.Fatalf("10-char password under default policy: got err=%v, want nil", err)
	}
}

func TestValidatePassword_HonoursOperatorPolicy(t *testing.T) {
	t.Parallel()
	if err := validatePassword("password14char", 12); err != nil {
		t.Fatalf("14-char password under min=12: got err=%v, want nil", err)
	}
	if err := validatePassword("short8ch", 12); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("8-char password under min=12: got err=%v, want ErrPasswordTooWeak", err)
	}
}

func TestValidatePassword_HardCeiling(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("a", maxPasswordLength+1)
	if err := validatePassword(huge, 0); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("over-cap password: got err=%v, want ErrPasswordTooWeak", err)
	}
}

func TestValidatePassword_FloorClampedAt8(t *testing.T) {
	t.Parallel()
	// Operator passes a min below 8 — clamp to 8 (DB CHECK enforces this,
	// but the in-memory layer must not trust callers blindly).
	if err := validatePassword("abcdefg", 4); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("7-char password with min=4 (clamped to 8): got err=%v, want ErrPasswordTooWeak", err)
	}
	// And the accept side: 8-char password with min=4 (clamped to 8) MUST pass.
	// Use a non-breached string — denylist is checked AFTER length policy.
	if err := validatePassword("kx8q-mz9", 4); err != nil {
		t.Fatalf("8-char password with min=4 (clamped to 8): got err=%v, want nil", err)
	}
}

func TestValidatePassword_RejectsCommonBreached(t *testing.T) {
	t.Parallel()
	// "password" is too short under default policy → returns
	// ErrPasswordTooWeak first; pass a high-min so we exercise the
	// denylist branch directly.
	if err := validatePassword("password", 4); !errors.Is(err, ErrPasswordCommonlyBreached) {
		t.Fatalf("'password' with min=4: got err=%v, want ErrPasswordCommonlyBreached", err)
	}
	// 12345678 is exactly 8 chars (passes floor) but on the list.
	if err := validatePassword("12345678", 4); !errors.Is(err, ErrPasswordCommonlyBreached) {
		t.Fatalf("'12345678': got err=%v, want ErrPasswordCommonlyBreached", err)
	}
	// password123 is 11 chars (passes default min=10).
	if err := validatePassword("password123", 0); !errors.Is(err, ErrPasswordCommonlyBreached) {
		t.Fatalf("'password123': got err=%v, want ErrPasswordCommonlyBreached", err)
	}
}

func TestValidatePassword_DenylistCaseInsensitive(t *testing.T) {
	t.Parallel()
	// Mixed case must still be rejected — entries are stored lowercase
	// and lookup folds the input.
	// Use min=4 so the denylist (not length) is what rejects these.
	cases := []string{"PASSWORD", "Password", "PaSsWoRd", "Welcome123", "QWERTY123"}
	for _, c := range cases {
		if err := validatePassword(c, 4); !errors.Is(err, ErrPasswordCommonlyBreached) {
			t.Fatalf("%q: got err=%v, want ErrPasswordCommonlyBreached", c, err)
		}
	}
}

func TestValidatePassword_StrongPasswordAccepted(t *testing.T) {
	t.Parallel()
	// Strong, non-breached, length-compliant password must pass.
	if err := validatePassword("xK7$qPm-rT9wVzQ2", 0); err != nil {
		t.Fatalf("strong password: got err=%v, want nil", err)
	}
}

// legacyHash builds an Argon2id hash in the pre-v2 3-part format
// (argon2id$salt$hash) with 3/64 MiB params, so we can exercise the
// legacy verification path without depending on a version-pinned helper.
func legacyHash(t *testing.T, password string) string {
	t.Helper()
	salt := make([]byte, 16)
	for i := range salt {
		salt[i] = byte(i + 1) // deterministic
	}
	derived := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("argon2id$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived),
	)
}

func TestHashPasswordProducesV2Format(t *testing.T) {
	t.Parallel()
	hash, err := hashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 4 {
		t.Fatalf("expected 4 parts (argon2id$v=2$salt$hash), got %d: %s", len(parts), hash)
	}
	if parts[0] != "argon2id" || parts[1] != "v=2" {
		t.Fatalf("wrong header: %q / %q", parts[0], parts[1])
	}
}

func TestVerifyPasswordAcceptsCurrentV2Hash(t *testing.T) {
	t.Parallel()
	password := "correct-horse-battery"
	hash, err := hashPassword(password)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if err := verifyPassword(hash, password); err != nil {
		t.Fatalf("verify current hash: %v", err)
	}
}

func TestVerifyPasswordAcceptsLegacyHash(t *testing.T) {
	t.Parallel()
	password := "correct-horse-battery"
	legacy := legacyHash(t, password)
	if err := verifyPassword(legacy, password); err != nil {
		t.Fatalf("verify legacy hash: %v", err)
	}
}

func TestVerifyPasswordRejectsWrongPasswordOnCurrent(t *testing.T) {
	t.Parallel()
	hash, err := hashPassword("right")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if err := verifyPassword(hash, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestVerifyPasswordRejectsWrongPasswordOnLegacy(t *testing.T) {
	t.Parallel()
	legacy := legacyHash(t, "right")
	if err := verifyPassword(legacy, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	t.Parallel()
	// Wrong scheme.
	if err := verifyPassword("bcrypt$abc$def", "anything"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong scheme: expected ErrInvalidCredentials, got %v", err)
	}
	// Wrong number of parts (2).
	if err := verifyPassword("argon2id$onlyone", "anything"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("two parts: expected ErrInvalidCredentials, got %v", err)
	}
	// 4-part but missing v=2 tag.
	if err := verifyPassword("argon2id$v=99$YWJj$ZGVm", "anything"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown version: expected ErrInvalidCredentials, got %v", err)
	}
	// 4-part v=2 with malformed base64 salt.
	if err := verifyPassword("argon2id$v=2$@@@notb64@@@$ZGVm", "anything"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("bad salt b64: expected ErrInvalidCredentials, got %v", err)
	}
}

// TestDummyPasswordHashMatchesCurrentFormat guards the user-enumeration
// timing-oracle fix (C-1 follow-up): dummyPasswordHash MUST emit the same
// 4-part v=2 format as the current hashPassword so verifyPassword routes
// it through the same Argon2id branch (4 iters / 96 MiB). If a future
// param bump on hashPassword forgets to update dummyPasswordHash, this
// test fails before the regression ships.
func TestDummyPasswordHashMatchesCurrentFormat(t *testing.T) {
	t.Parallel()
	d := dummyPasswordHash()
	parts := strings.Split(d, "$")
	if len(parts) != 4 {
		t.Fatalf("dummy hash must be v=2 4-part format, got %d parts: %s", len(parts), d)
	}
	if parts[0] != hashSchemeArgon2id || parts[1] != hashVersionTagV2 {
		t.Fatalf("dummy hash wrong header: %q / %q (want %q / %q)",
			parts[0], parts[1], hashSchemeArgon2id, hashVersionTagV2)
	}
}
