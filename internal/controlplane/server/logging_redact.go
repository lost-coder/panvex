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
		// crypto/rand failure is essentially impossible on a healthy
		// system; fall back to a fixed dev-only key so logging keeps
		// working — the alert is on the failure to read entropy.
		buf = []byte("panvex-log-username-fallback-key")[:32]
	}
	s.usernameHashKeyBytes = buf
	return s.usernameHashKeyBytes
}

