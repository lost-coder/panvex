package server

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// logUsername returns a stable, privacy-preserving identifier suitable
// for structured log fields (S9). Raw usernames end up in operator
// log aggregators, which in practice are less access-controlled than
// the user table itself — and for operators who use email addresses
// as usernames the raw value is PII.
//
// The output is SHA-256 prefix hex (u-xxxxxxxxxxxx). It is:
//   - deterministic, so two log lines for the same account join on this
//     field and an incident responder can correlate by it;
//   - one-way, so an attacker with log access cannot recover the
//     username itself (they still need the user table to resolve it);
//   - short, to keep log lines legible.
//
// An empty input yields "u-anon" so "missing username" is still
// distinguishable from an unlogged field.
func logUsername(username string) string {
	u := strings.TrimSpace(username)
	if u == "" {
		return "u-anon"
	}
	sum := sha256.Sum256([]byte(strings.ToLower(u)))
	return "u-" + hex.EncodeToString(sum[:6])
}
