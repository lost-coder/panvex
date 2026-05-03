package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Task 5 (S-medium): the session cookie issued at login must be an
// *opaque* random token — distinct from the internal Session.ID and
// from the persistent SessionRecord.id, both of which are HMACs over
// that token. A test that finds session.Cookie == session.ID would
// indicate the cookie is being reused as the DB primary key, which
// is exactly the coupling we are removing.
func TestAuthenticateIssuesOpaqueCookieDistinctFromSessionID(t *testing.T) {
	now := time.Date(2026, time.May, 3, 8, 0, 0, 0, time.UTC)
	service := NewService()
	if err := service.SetSessionLookupKey([]byte("test-session-lookup-key-bytes!!!")); err != nil {
		t.Fatalf("SetSessionLookupKey() error = %v", err)
	}
	service.SetNow(func() time.Time { return now })

	if _, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
	}, now)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if session.Cookie == "" {
		t.Fatal("Authenticate() returned empty Cookie; the HTTP layer cannot Set-Cookie on the client")
	}
	if session.ID == "" {
		t.Fatal("Authenticate() returned empty ID; the in-memory map and DB cannot key on it")
	}
	if session.Cookie == session.ID {
		t.Fatalf("Authenticate() returned Cookie == ID = %q; want opaque cookie distinct from the lookup hash", session.Cookie)
	}

	// The hash of the cookie under the service's lookup key must
	// equal the stored Session.ID — that is the round-trip the HTTP
	// layer relies on every time it resolves a cookie back to a
	// session.
	if got := service.hashSessionToken(session.Cookie); got != session.ID {
		t.Fatalf("hashSessionToken(cookie) = %q, want session.ID = %q", got, session.ID)
	}
}

// A request carrying the opaque cookie value must resolve to the same
// in-memory session record as one carrying the lookup hash directly.
// This is the contract that lets HTTP handlers (cookie path) and
// internal callers (hash path) share the same Session struct without
// double-hashing or double-bookkeeping.
func TestGetSessionByCookieResolvesToSameRecord(t *testing.T) {
	now := time.Date(2026, time.May, 3, 8, 0, 0, 0, time.UTC)
	service := NewService()
	if err := service.SetSessionLookupKey([]byte("test-session-lookup-key-bytes!!!")); err != nil {
		t.Fatalf("SetSessionLookupKey() error = %v", err)
	}
	service.SetNow(func() time.Time { return now })

	if _, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
	}, now)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	byCookie, err := service.GetSessionByCookie(session.Cookie)
	if err != nil {
		t.Fatalf("GetSessionByCookie() error = %v", err)
	}
	byID, err := service.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if byCookie.ID != byID.ID {
		t.Fatalf("GetSessionByCookie().ID = %q, GetSession().ID = %q; want equal",
			byCookie.ID, byID.ID)
	}
	if byCookie.UserID != byID.UserID {
		t.Fatalf("UserID mismatch: cookie=%q id=%q", byCookie.UserID, byID.UserID)
	}
	if byCookie.Cookie != "" {
		t.Fatalf("GetSessionByCookie() leaked Cookie = %q; the in-memory record must not retain the opaque token", byCookie.Cookie)
	}
}

// A tampered cookie — any single-byte change relative to the issued
// value — must NOT collide with the stored hash. HMAC-SHA-256 makes
// this true with overwhelming probability; the test is a regression
// guard against accidentally weakening the lookup (e.g. truncating the
// hash, dropping the key, or comparing prefix only).
func TestTamperedCookieDoesNotCollide(t *testing.T) {
	now := time.Date(2026, time.May, 3, 8, 0, 0, 0, time.UTC)
	service := NewService()
	if err := service.SetSessionLookupKey([]byte("test-session-lookup-key-bytes!!!")); err != nil {
		t.Fatalf("SetSessionLookupKey() error = %v", err)
	}
	service.SetNow(func() time.Time { return now })

	if _, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
	}, now)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	// Flip a single byte at the front of the base64url cookie. The
	// cookie alphabet has 64 symbols; any other ASCII letter outside
	// that set guarantees we changed the value rather than producing
	// the same string. session.Cookie is base64url with no padding
	// and at least 32 chars (32 random bytes), so [0] is always set.
	original := session.Cookie
	if len(original) == 0 {
		t.Fatal("session.Cookie empty; cannot mutate")
	}
	tampered := flipByte(original)
	if tampered == original {
		t.Fatal("tampered cookie equals original; mutation logic broken")
	}
	if _, err := service.GetSessionByCookie(tampered); err == nil {
		t.Fatalf("GetSessionByCookie(tampered=%q) returned nil error; want ErrSessionNotFound", tampered)
	}

	// And: the hash of the tampered cookie must differ from the
	// stored session.ID. Belt-and-braces, the previous assertion
	// already implies this, but a direct comparison documents the
	// HMAC round-trip the lookup depends on.
	if got := service.hashSessionToken(tampered); got == session.ID {
		t.Fatalf("hashSessionToken(tampered) = %q collides with session.ID = %q", got, session.ID)
	}
}

// flipByte flips a single bit in the first non-trivial byte of s and
// returns the result, preserving length. Used by tamper tests to
// produce a value that is guaranteed-distinct from the input without
// caring about specific encoding alphabets.
func flipByte(s string) string {
	if s == "" {
		return s
	}
	b := []byte(s)
	b[0] ^= 0x01
	return string(b)
}

// Audit-log redaction must not leak the internal session lookup hash.
// The session-lookup key (hashes the *cookie* into Session.ID) and the
// log-redaction key (hashes Session.ID into a stable correlatable
// "s-…" tag) are derived from EncryptionKey under different domain
// tags, so the audit-visible output is two layers of HMAC away from a
// live cookie. This test exercises only the auth-side guarantee:
// session.ID (the lookup hash) is not echoed verbatim into anything
// the audit pipeline can read.
func TestSessionLookupHashIsNotTheCookie(t *testing.T) {
	now := time.Date(2026, time.May, 3, 8, 0, 0, 0, time.UTC)
	service := NewService()
	if err := service.SetSessionLookupKey([]byte("test-session-lookup-key-bytes!!!")); err != nil {
		t.Fatalf("SetSessionLookupKey() error = %v", err)
	}
	service.SetNow(func() time.Time { return now })

	if _, _, err := service.BootstrapUser(context.Background(), BootstrapInput{
		Username: "operator",
		Password: "Correct1horse2battery",
		Role:     RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	session, err := service.Authenticate(context.Background(), LoginInput{
		Username: "operator",
		Password: "Correct1horse2battery",
	}, now)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	// session.ID is the lookup hash and a hex-encoded HMAC-SHA-256
	// digest (64 chars). It must never equal the cookie token, and
	// it must never *contain* the cookie as a substring.
	if strings.Contains(session.ID, session.Cookie) {
		t.Fatalf("session.ID = %q contains session.Cookie = %q; the lookup hash must be derived, not concatenated",
			session.ID, session.Cookie)
	}
	if got := len(session.ID); got != 64 {
		t.Fatalf("len(session.ID) = %d, want 64 (hex-encoded SHA-256)", got)
	}
}

// SetSessionLookupKey rejects keys shorter than 16 bytes so a typo or
// misconfigured deployment cannot silently fall back to a weak
// lookup. The HMAC remains technically computable with shorter keys,
// but the security argument for "hash the cookie before storing" only
// holds at full-strength key material.
func TestSetSessionLookupKeyRejectsShortKey(t *testing.T) {
	service := NewService()
	if err := service.SetSessionLookupKey([]byte("tooshort")); err == nil {
		t.Fatal("SetSessionLookupKey(short) returned nil error; want rejection")
	}
}
