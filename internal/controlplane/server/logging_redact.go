package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

// usernameHashKey returns the cached HMAC key for username log
// hashing. Derivation:
//
//   if EncryptionKey != "":
//     key = SHA-256("panvex-log-username-v1" || EncryptionKey)
//   else:
//     key = 32 random bytes (per-process)
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
	if key := strings.TrimSpace(s.encryptionKey); key != "" {
		sum := sha256.Sum256([]byte("panvex-log-username-v1\x00" + key))
		s.usernameHashKeyBytes = sum[:]
		return s.usernameHashKeyBytes
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// Q3.U-S-22: crypto/rand failures are essentially impossible
		// on a healthy system. The previous fallback to a fixed dev-only
		// key meant a degraded entropy path silently produced predictable
		// HMACs. Fail-closed instead: panic so the operator cannot ship
		// a log-redaction bypass undetected. The control plane has no
		// safe way to keep running without secure entropy anyway —
		// session IDs, CSRF tokens, and CA generation all depend on it.
		panic("control-plane: cannot derive username log-hash key: " + err.Error())
	}
	s.usernameHashKeyBytes = buf
	return s.usernameHashKeyBytes
}

