package clients

import (
	"errors"
	"strings"
	"testing"
)

func TestIsValidHexSecret(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"lowercase 32 hex", strings.Repeat("a", 32), true},
		{"uppercase 32 hex", strings.Repeat("F", 32), true},
		{"mixed case 32 hex", "AbCdEf01234567890abcdefABCDEF0123", false}, // 33 chars
		{"31 chars", strings.Repeat("a", 31), false},
		{"33 chars", strings.Repeat("a", 33), false},
		{"contains non-hex", strings.Repeat("g", 32), false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsValidHexSecret(tc.in); got != tc.want {
				t.Fatalf("IsValidHexSecret(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestRandomHexString(t *testing.T) {
	t.Parallel()

	a, err := RandomHexString(16)
	if err != nil {
		t.Fatalf("RandomHexString error: %v", err)
	}
	if len(a) != 32 {
		t.Fatalf("RandomHexString(16) length = %d, want 32", len(a))
	}
	if !IsValidHexSecret(a) {
		t.Fatalf("RandomHexString output %q is not valid hex", a)
	}
	b, err := RandomHexString(16)
	if err != nil {
		t.Fatalf("RandomHexString error: %v", err)
	}
	if a == b {
		t.Fatalf("two successive RandomHexString calls returned the same value %q", a)
	}
}

func TestResolveUserADTag(t *testing.T) {
	t.Parallel()

	t.Run("empty with fallback returns fallback", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveUserADTag("", "deadbeef")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "deadbeef" {
			t.Fatalf("got %q, want fallback", got)
		}
	})

	t.Run("empty without fallback generates fresh", func(t *testing.T) {
		t.Parallel()
		got, err := ResolveUserADTag("", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 32 || !IsValidHexSecret(got) {
			t.Fatalf("generated tag %q is not a 32-char hex string", got)
		}
	})

	t.Run("valid uppercase is lowercased", func(t *testing.T) {
		t.Parallel()
		in := strings.Repeat("A", 32)
		got, err := ResolveUserADTag(in, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != strings.ToLower(in) {
			t.Fatalf("got %q, want %q", got, strings.ToLower(in))
		}
	})

	t.Run("bad length rejected", func(t *testing.T) {
		t.Parallel()
		_, err := ResolveUserADTag("abc", "")
		if !errors.Is(err, ErrUserADTag) {
			t.Fatalf("got %v, want ErrUserADTag", err)
		}
	})

	t.Run("non-hex rejected", func(t *testing.T) {
		t.Parallel()
		_, err := ResolveUserADTag(strings.Repeat("g", 32), "")
		if !errors.Is(err, ErrUserADTag) {
			t.Fatalf("got %v, want ErrUserADTag", err)
		}
	})
}

func TestNormalizeExpiration(t *testing.T) {
	t.Parallel()

	t.Run("empty returns empty", func(t *testing.T) {
		t.Parallel()
		got, err := NormalizeExpiration("")
		if err != nil || got != "" {
			t.Fatalf("got (%q, %v), want (\"\", nil)", got, err)
		}
	})

	t.Run("valid RFC3339 returns UTC", func(t *testing.T) {
		t.Parallel()
		got, err := NormalizeExpiration("2026-01-02T03:04:05+01:00")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "2026-01-02T02:04:05Z" {
			t.Fatalf("got %q, want UTC-normalized RFC3339", got)
		}
	})

	t.Run("invalid rejected", func(t *testing.T) {
		t.Parallel()
		_, err := NormalizeExpiration("not a date")
		if !errors.Is(err, ErrExpiration) {
			t.Fatalf("got %v, want ErrExpiration", err)
		}
	})
}

func TestNormalizedIDs(t *testing.T) {
	t.Parallel()

	got := NormalizedIDs([]string{"b", "a", " ", "a", "  c  ", ""})
	// Trimmed, deduped, sorted: [a b c] (note: "  c  " trims to "c").
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
