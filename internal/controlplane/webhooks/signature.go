package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign returns the signature header value for a body+timestamp pair.
// The format follows the GitHub / Stripe convention: "sha256=<hex>".
// Receivers verify with their copy of the secret + the same
// timestamp/body inputs.
//
// The timestamp is included in the signed input so a receiver can
// drop replays without the server having to mint per-request nonces.
// Recommended verification window is ±5 minutes; documented in
// package doc.go and in the receiver-side examples.
func Sign(secret []byte, timestamp, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(timestamp)
	mac.Write([]byte{'\n'})
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify checks an X-Panvex-Signature header value against the
// expected (secret, timestamp, body) tuple. Constant-time compare
// guards against timing oracles. Used in tests and receiver
// example code; not on the panel's hot path.
func Verify(secret []byte, timestamp, body []byte, header string) bool {
	want := Sign(secret, timestamp, body)
	return hmac.Equal([]byte(want), []byte(header))
}
