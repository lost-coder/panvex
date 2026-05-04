package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// logUsername returns a stable, privacy-preserving identifier suitable
// for structured log fields (S9). Raw usernames end up in operator
// log aggregators which in practice are less access-controlled than
// the user table itself — and for operators who use email addresses
// as usernames the raw value is PII.
//
// The output is HMAC-SHA256 prefix hex (u-xxxxxxxxxxxx). Compared
// to a bare SHA-256 (the previous form), HMAC means an attacker who
// only has the log files cannot reverse common usernames via a
// pre-computed rainbow table — the secret materially lifts the
// brute-force cost from "trivial dictionary" to "infeasible without
// also exfiltrating the secret."
//
// The HMAC key is derived once per Server from the EncryptionKey
// option (the same key the operator already provisions for the CA
// private-key cipher). Empty EncryptionKey falls back to a fresh
// per-process random key — hashes stay correlatable within a single
// run but rotate on restart. Production must always set
// EncryptionKey so log fields stay correlatable across deploys.
//
// An empty input yields "u-anon" so "missing username" is still
// distinguishable from an unlogged field.
func (s *Server) logUsername(username string) string {
	u := strings.TrimSpace(username)
	if u == "" {
		return "u-anon"
	}
	mac := hmac.New(sha256.New, s.usernameHashKey())
	mac.Write([]byte(strings.ToLower(u)))
	return "u-" + hex.EncodeToString(mac.Sum(nil)[:6])
}

// logIPHash returns a privacy-preserving identifier for a client IP,
// suitable for structured log fields tied to the IP-keyed lockout
// tracker (Task 6, S-medium). Raw client IPs in logs are PII for
// residential users and operationally noisy; hashing keeps lines
// correlatable within a process / deploy without exposing the value.
//
// Implemented as a top-level function (not a method on *Server) so
// callers in the lockout pre-check path do not depend on Server
// internals — but the HMAC key still rotates only when EncryptionKey
// rotates, via the package-level keying helper described in
// usernameHashKey. We accept a Server-less form by deriving from the
// same per-process key when the caller is not server-bound, falling
// back to a sha256 prefix when no key is available. In practice every
// caller is on the *Server hot path; the bare function is just a thin
// adapter.
func (s *Server) logIPHash(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "ip-anon"
	}
	mac := hmac.New(sha256.New, s.usernameHashKey())
	mac.Write([]byte(ip))
	return "ip-" + hex.EncodeToString(mac.Sum(nil)[:6])
}

// logIPHash is the package-level shim used from contexts that don't
// have a *Server in scope (the lockout pre-check is one). It computes a
// non-keyed sha256 prefix — sufficient for correlation, since the IP
// itself is low-entropy and a cross-deploy rainbow table would be
// trivial regardless. Callers with a *Server should prefer the method.
func logIPHash(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "ip-anon"
	}
	sum := sha256.Sum256([]byte(ip))
	return "ip-" + hex.EncodeToString(sum[:6])
}

// logSessionID returns a stable, privacy-preserving identifier for a
// session ID, suitable for audit-event target IDs and structured log
// fields. The full session.ID doubles as the cookie value, so leaking
// it via audit-trail (visible to Operator/Admin) or SIEM-mirrored
// structured logs is a session-hijack vector. We hash with the same
// per-process HMAC key used for usernames so sessions stay correlatable
// across log lines without exposing the live token.
//
// Empty input yields "" so callers can pass through an unknown ID
// without writing the literal string "s-anon".
func (s *Server) logSessionID(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	mac := hmac.New(sha256.New, s.usernameHashKey())
	mac.Write([]byte(sessionID))
	return "s-" + hex.EncodeToString(mac.Sum(nil)[:8])
}

// deriveSessionLookupKey derives the per-server session-lookup HMAC
// key (S22 Task 5, S-medium) from the operator-provided encryption
// key. Domain tag "panvex-session-lookup-v1" keeps this key
// independent of the username/audit log-hash key (also derived from
// EncryptionKey) and the CA private-key cipher: leaking one
// derivative does not weaken the others.
//
// Empty EncryptionKey returns nil, signalling "do not configure" — the
// auth service then falls back to a per-process random key on first
// use (sessions stay correlatable within a single run but rotate on
// restart, which means restored cookies stop verifying after a
// fail-over). Production deployments must always set EncryptionKey.
func deriveSessionLookupKey(encryptionKey string) []byte {
	key := strings.TrimSpace(encryptionKey)
	if key == "" {
		return nil
	}
	sum := sha256.Sum256([]byte("panvex-session-lookup-v1\x00" + key))
	return sum[:]
}

// usernameHashKey returns the cached HMAC key for username log
// hashing. The bytes are pre-populated by initUsernameHashKey from the
// constructor (Plan 3 Task 4 / Q-7), so the hot-path log redactors
// observe a non-nil key without doing entropy work or risking a panic.
// A zero-value Server (e.g. test fixtures that build Server{} directly
// without going through New) lazily falls back to deriving the key on
// first call so existing tests keep working.
//
// Derivation, performed once in initUsernameHashKey:
//
//	if EncryptionKey != "":
//	  key = SHA-256("panvex-log-username-v1" || EncryptionKey)
//	else:
//	  key = 32 random bytes (per-process)
//
// The "panvex-log-username-v1" tag domain-separates this key from
// any other use of EncryptionKey (CA cipher, future signing slots),
// so leaking a log-key derivative cannot be replayed against
// EncryptionKey itself.
func (s *Server) usernameHashKey() []byte {
	s.usernameHashMu.Lock()
	defer s.usernameHashMu.Unlock()
	if s.usernameHashKeyBytes != nil {
		return s.usernameHashKeyBytes
	}
	// Lazy fallback for tests that build Server{} directly. Production
	// boot path always primes the key via initUsernameHashKey, so this
	// branch is unreachable in real deployments. Mirrors the pre-fix
	// fallback so the only behavioural change is that the boot path is
	// fail-closed via initUsernameHashKey instead of panicking here.
	if key := strings.TrimSpace(s.encryptionKey); key != "" {
		sum := sha256.Sum256([]byte("panvex-log-username-v1\x00" + key))
		s.usernameHashKeyBytes = sum[:]
		return s.usernameHashKeyBytes
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// Last-resort fallback for the lazy-init path. Production boot
		// runs initUsernameHashKey first and surfaces this as an error
		// from New (Plan 3 Task 4 / Q-7). If we somehow land here on a
		// healthy system the request that triggered hashing will see a
		// degraded key — but the boot-time guarantee is the load-bearing
		// one for fail-closed log redaction.
		buf = make([]byte, 32)
		copy(buf, []byte("panvex-log-degraded-fallback-key"))
	}
	s.usernameHashKeyBytes = buf
	return s.usernameHashKeyBytes
}

// initUsernameHashKey derives and caches the HMAC log-redaction key
// once at Server construction. Returning an error instead of panicking
// (Plan 3 Task 4 / Q-7) lets New surface entropy failures to the
// caller; embedders and tests can recover instead of being killed by a
// library-level panic.
//
// Idempotent: a non-nil cached key short-circuits, so callers that
// constructed a Server via the New path do not re-derive on hot
// request paths.
func (s *Server) initUsernameHashKey() error {
	s.usernameHashMu.Lock()
	defer s.usernameHashMu.Unlock()
	if s.usernameHashKeyBytes != nil {
		return nil
	}
	if key := strings.TrimSpace(s.encryptionKey); key != "" {
		sum := sha256.Sum256([]byte("panvex-log-username-v1\x00" + key))
		s.usernameHashKeyBytes = sum[:]
		return nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// Q3.U-S-22: crypto/rand failures are essentially impossible
		// on a healthy system. Fail-closed: an operator cannot ship a
		// log-redaction bypass undetected because New refuses to
		// return a Server. The control plane has no safe way to keep
		// running without secure entropy anyway — session IDs, CSRF
		// tokens, and CA generation all depend on it.
		return fmt.Errorf("derive username log-hash key: %w", err)
	}
	s.usernameHashKeyBytes = buf
	return nil
}
