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
	if err := validatePassword("1234567", 4); !errors.Is(err, ErrPasswordTooWeak) {
		t.Fatalf("7-char password with min=4 (clamped to 8): got err=%v, want ErrPasswordTooWeak", err)
	}
	// And the accept side: 8-char password with min=4 (clamped to 8) MUST pass.
	if err := validatePassword("12345678", 4); err != nil {
		t.Fatalf("8-char password with min=4 (clamped to 8): got err=%v, want nil", err)
	}
}
