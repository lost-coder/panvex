package server

import "testing"

// FuzzRequestID feeds arbitrary strings to validRequestID, which gates the
// attacker-controllable inbound X-Request-Id header before it is echoed into
// response headers and structured logs. It must never panic, and any value it
// accepts must still validate on a second pass (idempotence of the predicate).
//
// It also exercises newRequestID's output through the validator: a freshly
// minted UUID must always be accepted, otherwise the middleware would loop on
// itself logically (every request would be treated as unvalidated).
func FuzzRequestID(f *testing.F) {
	f.Add("0190f3a1-7c2e-7b4a-9f1e-0123456789ab") // UUIDv7-shaped
	f.Add("")                                     // empty
	f.Add("simple-correlation-id")
	f.Add(" leading-space")
	f.Add("with\ttab")
	f.Add("with\x00null")
	f.Add("ünicode")
	// A 200-byte string exceeds the 128-byte cap.
	f.Add("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	f.Fuzz(func(t *testing.T, s string) {
		ok := validRequestID(s)
		if ok {
			// Accepted values must remain accepted on re-validation.
			if !validRequestID(s) {
				t.Fatalf("validRequestID not idempotent for %q", s)
			}
		}
		// A freshly minted ID must always pass validation.
		if id := newRequestID(); !validRequestID(id) {
			t.Fatalf("newRequestID produced an invalid id: %q", id)
		}
	})
}
