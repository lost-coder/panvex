package storage

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// keysetCursor is the wire shape of an opaque cursor passed to operator-facing
// list endpoints. Keeping the JSON unversioned keeps the cursor short; if the
// shape ever needs to evolve we add a Version field and accept missing == 1.
//
// CreatedAt is encoded in RFC3339Nano so a server returning a cursor over
// SQLite (microsecond resolution) and another reading it on PostgreSQL
// (microsecond) round-trip the same key. ID disambiguates rows that share a
// timestamp, which both backends allow.
type keysetCursor struct {
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"i"`
}

// EncodeKeysetCursor returns the base64-url JSON encoding of the (createdAt,
// id) keyset position. Empty id == sentinel "first page" — callers should
// pass "" for both arguments to mean that.
func EncodeKeysetCursor(createdAt time.Time, id string) string {
	if id == "" && createdAt.IsZero() {
		return ""
	}
	c := keysetCursor{CreatedAt: createdAt.UTC(), ID: id}
	raw, err := json.Marshal(c)
	if err != nil {
		// json.Marshal of a (time, string) pair never fails; we still
		// fall back to "" rather than panic so a buggy clock can't crash
		// the server.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// DecodeKeysetCursor parses an opaque cursor produced by EncodeKeysetCursor.
// An empty input returns the zero cursor (first page). Malformed input returns
// an error so the HTTP layer can respond 400 — silently treating garbage as
// "first page" would let a stale-but-valid-looking cursor produce wrong
// results without the client noticing.
func DecodeKeysetCursor(encoded string) (time.Time, string, error) {
	if encoded == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("decode cursor: %w", err)
	}
	var c keysetCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return time.Time{}, "", fmt.Errorf("decode cursor: %w", err)
	}
	return c.CreatedAt.UTC(), c.ID, nil
}
