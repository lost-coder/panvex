// Package seqid decodes the control-plane's prefixed monotonic IDs
// ("client-0000042", "job-000000000042", "audit-17"). Several domains mint
// such IDs and every one of them has to recover its counter from persisted
// rows on restart, so the decode lives here once.
package seqid

import (
	"strconv"
	"strings"
)

// MaxPrefixed returns max(current, N) where N is the trailing integer of
// value when it has the form "<prefix>-<digits>". A value that does not match
// that layout (a random-hex ID, an empty string, an overflowing number)
// leaves current unchanged.
//
// Callers use it to seed an in-memory counter from the highest ID already in
// the store, so IDs minted after a restart never collide with old ones.
func MaxPrefixed(current uint64, prefix, value string) uint64 {
	digits, ok := strings.CutPrefix(value, prefix+"-")
	if !ok {
		return current
	}
	parsed, err := strconv.ParseUint(digits, 10, 64)
	if err != nil || parsed <= current {
		return current
	}
	return parsed
}
