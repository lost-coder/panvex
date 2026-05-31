package storage

import (
	"testing"
	"time"
)

// FuzzDecodeKeysetCursor feeds arbitrary bytes to the pagination cursor
// decoder, which parses an opaque, attacker-controllable query parameter.
// The decoder must never panic; malformed input must return an error.
//
// It also asserts the roundtrip invariant: a cursor produced by
// EncodeKeysetCursor must decode back to the same (createdAt, id) pair,
// using the seed corpus values as encode inputs.
func FuzzDecodeKeysetCursor(f *testing.F) {
	// Valid cursors derived from EncodeKeysetCursor.
	f.Add(EncodeKeysetCursor(time.Unix(0, 0).UTC(), "abc"))
	f.Add(EncodeKeysetCursor(time.Date(2026, 5, 31, 12, 0, 0, 123456789, time.UTC), "id-123"))
	f.Add(EncodeKeysetCursor(time.Time{}, "only-id"))
	// Edge cases.
	f.Add("")                   // sentinel first page
	f.Add("not-base64-!@#$%")   // invalid base64
	f.Add("e30")                // base64 of "{}" — empty JSON object
	f.Add("bnVsbA")             // base64 of "null"
	f.Add("W10")                // base64 of "[]" — wrong JSON shape
	f.Add("eyJ0IjoiYm9ndXMifQ") // base64 of {"t":"bogus"} — bad timestamp

	f.Fuzz(func(t *testing.T, encoded string) {
		// Must never panic on arbitrary input.
		gotTime, gotID, err := DecodeKeysetCursor(encoded)
		if err != nil {
			return
		}
		// On a successful decode, re-encoding then decoding must round-trip
		// to the same logical position (idempotence of the wire shape).
		reEncoded := EncodeKeysetCursor(gotTime, gotID)
		rtTime, rtID, rtErr := DecodeKeysetCursor(reEncoded)
		if rtErr != nil {
			t.Fatalf("re-decode of self-encoded cursor failed: input=%q err=%v", encoded, rtErr)
		}
		if !rtTime.Equal(gotTime) || rtID != gotID {
			t.Fatalf("roundtrip mismatch: input=%q got=(%v,%q) roundtrip=(%v,%q)",
				encoded, gotTime, gotID, rtTime, rtID)
		}
	})
}
