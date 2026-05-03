package auth

import (
	"errors"
	"strings"
	"testing"
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
