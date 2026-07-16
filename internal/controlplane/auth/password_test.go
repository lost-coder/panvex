package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
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
// (argon2id$salt$hash) with 3/64 MiB params. The format is no longer
// accepted; the helper exists so we can assert it is rejected.
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

func TestHashPasswordProducesV3Format(t *testing.T) {
	t.Parallel()
	hash, err := hashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 5 {
		t.Fatalf("expected 5 parts (argon2id$v=3$params$salt$hash), got %d: %s", len(parts), hash)
	}
	if parts[0] != "argon2id" || parts[1] != "v=3" {
		t.Fatalf("wrong header: %q / %q", parts[0], parts[1])
	}
	if parts[2] != "t=4,m=98304,p=2" {
		t.Fatalf("default profile params: got %q", parts[2])
	}
}

// TestHashPasswordEmbedsActiveProfileParams pins that new hashes follow the
// active profile and stay self-describing, so a profile switch does not
// orphan previously written hashes.
func TestHashPasswordEmbedsActiveProfileParams(t *testing.T) {
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })
	if err := kdf.SetActiveProfile("low-memory"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	hash, err := hashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "argon2id$v=3$t=2,m=19456,p=1$") {
		t.Fatalf("v3 hash must embed params, got %s", hash)
	}
	if err := verifyPassword(hash, "correct horse battery"); err != nil {
		t.Fatalf("roundtrip: %v", err)
	}
	if err := verifyPassword(hash, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
}

// TestVerifyPasswordAcceptsLegacyV2 pins backward compatibility: hashes
// written before the profile split (fixed 4 iters / 96 MiB, no embedded
// params) must keep verifying, or every existing install is locked out.
func TestVerifyPasswordAcceptsLegacyV2(t *testing.T) {
	t.Parallel()
	salt := []byte("0123456789abcdef")
	derived := argon2.IDKey([]byte("pw1234567890"), salt, 4, 96*1024, 2, 32)
	hash := fmt.Sprintf("argon2id$v=2$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(derived))
	if err := verifyPassword(hash, "pw1234567890"); err != nil {
		t.Fatalf("legacy v2 must verify: %v", err)
	}
	if err := verifyPassword(hash, "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("legacy v2 wrong password: %v", err)
	}
}

// TestVerifyPasswordRejectsOversizedV3Params proves a corrupted or planted
// row cannot make the panel allocate gigabytes / spin for weeks: the sanity
// ceilings reject before any derivation runs.
func TestVerifyPasswordRejectsOversizedV3Params(t *testing.T) {
	before := kdf.Derivations()
	cases := []string{
		"argon2id$v=3$t=2,m=4194304,p=1$AAAA$AAAA",   // 4 GiB memory
		"argon2id$v=3$t=99999,m=19456,p=1$AAAA$AAAA", // absurd iterations
		"argon2id$v=3$t=2,m=19456,p=99$AAAA$AAAA",    // absurd parallelism
		"argon2id$v=3$t=0,m=19456,p=1$AAAA$AAAA",     // zero iterations
		"argon2id$v=3$t=2,m=0,p=1$AAAA$AAAA",         // zero memory
		"argon2id$v=3$t=2,m=19456,p=0$AAAA$AAAA",     // zero parallelism
		"argon2id$v=3$garbage$AAAA$AAAA",             // unparsable params
	}
	for _, hash := range cases {
		if err := verifyPassword(hash, "pw"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("%s: oversized/invalid params must be rejected, got %v", hash, err)
		}
	}
	if got := kdf.Derivations() - before; got != 0 {
		t.Fatalf("rejected params must not derive at all, ran %d derivations", got)
	}
}

// TestVerifyPasswordRunsExactlyOneDerivation pins C-1: a verify call must
// never try more than one parameter set, or the response time classifies the
// hash and reopens a timing oracle.
func TestVerifyPasswordRunsExactlyOneDerivation(t *testing.T) {
	v3, err := hashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	salt := []byte("0123456789abcdef")
	v2 := fmt.Sprintf("argon2id$v=2$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(argon2.IDKey([]byte("pw1234567890"), salt, 4, 96*1024, 2, 32)))

	cases := []struct{ name, hash, password string }{
		{"v3 correct", v3, "correct-horse-battery"},
		{"v3 wrong", v3, "wrong"},
		{"v2 correct", v2, "pw1234567890"},
		{"v2 wrong", v2, "wrong"},
	}
	for _, tc := range cases {
		before := kdf.Derivations()
		_ = verifyPassword(tc.hash, tc.password)
		if got := kdf.Derivations() - before; got != 1 {
			t.Fatalf("%s: want exactly 1 derivation (C-1), got %d", tc.name, got)
		}
	}
}

// TestNeedsRehash pins the trigger for rehash-on-login: anything that is not
// the active profile in the v3 format needs rewriting.
func TestNeedsRehash(t *testing.T) {
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })
	if err := kdf.SetActiveProfile("low-memory"); err != nil {
		t.Fatalf("SetActiveProfile: %v", err)
	}
	current, err := hashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if needsRehash(current) {
		t.Fatalf("hash written under the active profile must not need a rehash: %s", current)
	}
	stale := []string{
		"argon2id$v=2$AAAA$AAAA",                 // legacy format
		"argon2id$v=3$t=4,m=98304,p=2$AAAA$AAAA", // other profile's params
		"argon2id$v=3$t=2,m=19456,p=2$AAAA$AAAA", // one param off
		"garbage",                                // unreadable
	}
	for _, hash := range stale {
		if !needsRehash(hash) {
			t.Fatalf("%s must need a rehash", hash)
		}
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

// R5: the pre-v2 3-part format is gone. Even the RIGHT password against
// a legacy hash must be rejected — those rows are unreadable by design.
func TestVerifyPasswordRejectsLegacyHash(t *testing.T) {
	t.Parallel()
	password := "correct-horse-battery"
	legacy := legacyHash(t, password)
	if err := verifyPassword(legacy, password); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for legacy hash, got %v", err)
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
// format and params as the current hashPassword so verifyPassword routes it
// through the same Argon2id branch. If it did not, the unknown-user path
// would burn measurably different CPU than the real-user path and the
// enumeration oracle this dummy exists to close would reopen.
//
// Not parallel: it switches the active profile.
func TestDummyPasswordHashMatchesCurrentFormat(t *testing.T) {
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })
	for _, profile := range []string{"default", "low-memory"} {
		if err := kdf.SetActiveProfile(profile); err != nil {
			t.Fatalf("SetActiveProfile(%s): %v", profile, err)
		}
		d := dummyPasswordHash()
		parts := strings.Split(d, "$")
		if len(parts) != 5 {
			t.Fatalf("%s: dummy hash must be v=3 5-part format, got %d parts: %s", profile, len(parts), d)
		}
		if parts[0] != hashSchemeArgon2id || parts[1] != hashVersionTagV3 {
			t.Fatalf("%s: dummy hash wrong header: %q / %q (want %q / %q)",
				profile, parts[0], parts[1], hashSchemeArgon2id, hashVersionTagV3)
		}
		// The whole point: the dummy must cost what a REAL hash of the
		// active profile costs, so the params must be the active ones.
		if needsRehash(d) {
			t.Fatalf("%s: dummy hash params must match the active profile, got %s", profile, d)
		}
		// And it must route through the normal v3 verify branch — one
		// derivation, same as a real user's hash.
		before := kdf.Derivations()
		if err := verifyPassword(d, "whatever-the-attacker-typed"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("%s: dummy verify: %v", profile, err)
		}
		if got := kdf.Derivations() - before; got != 1 {
			t.Fatalf("%s: dummy path must derive exactly once, got %d", profile, got)
		}
	}
}

// TestDummyPasswordHashCostsNoDerivationToBuild pins that the unknown-user
// login path costs EXACTLY the same as a real user's: one derivation, always.
//
// The dummy's stored bytes are never compared for equality — only the Argon2id
// cost of verifying against them matters — so building it must not derive at
// all. Deriving to build it made the FIRST unknown-user login per process cost
// two derivations against a real user's one: a one-shot ~2x latency signal on a
// freshly booted panel, which is the very enumeration oracle the dummy exists
// to close.
//
// Not parallel: it reads the process-global derivation counter.
func TestDummyPasswordHashCostsNoDerivationToBuild(t *testing.T) {
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })
	for _, profile := range []string{"default", "low-memory"} {
		if err := kdf.SetActiveProfile(profile); err != nil {
			t.Fatalf("SetActiveProfile(%s): %v", profile, err)
		}
		before := kdf.Derivations()
		d := dummyPasswordHash()
		if got := kdf.Derivations() - before; got != 0 {
			t.Fatalf("%s: building the dummy ran %d derivation(s), want 0", profile, got)
		}
		// Still a well-formed active-profile hash that verifies (and fails) on
		// the same branch as a real one.
		if needsRehash(d) {
			t.Fatalf("%s: dummy must carry the active profile's params, got %s", profile, d)
		}
		before = kdf.Derivations()
		if err := verifyPassword(d, "whatever-the-attacker-typed"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("%s: dummy verify: %v", profile, err)
		}
		if got := kdf.Derivations() - before; got != 1 {
			t.Fatalf("%s: dummy verify ran %d derivations, want exactly 1", profile, got)
		}
	}
}

// Two consecutive dummies must differ: a fixed dummy would let an attacker who
// somehow learns it recognise the unknown-user path by its stored value.
func TestDummyPasswordHashIsNotConstant(t *testing.T) {
	first := dummyPasswordHash()
	second := dummyPasswordHash()
	if first == second {
		t.Fatalf("dummy hashes must be freshly randomised per call, got %s twice", first)
	}
}
