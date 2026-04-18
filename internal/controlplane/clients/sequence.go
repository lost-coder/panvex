package clients

import (
	"fmt"
	"strconv"
	"strings"
)

// newSequenceID formats a prefixed monotonic ID ("client-0000042").
// Matches the layout used elsewhere in the control-plane.
func newSequenceID(prefix string, value uint64) string {
	return prefix + "-" + fmt.Sprintf("%07d", value)
}

// maxPrefixedSequence returns max(current, decoded(value)) when value
// has the expected "<prefix>-<digits>" layout; otherwise returns
// current unchanged. Used to recover the next-ID counter from
// persisted IDs at startup.
func maxPrefixedSequence(current uint64, prefix string, value string) uint64 {
	if !strings.HasPrefix(value, prefix+"-") {
		return current
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix+"-"), 10, 64)
	if err != nil {
		return current
	}
	if parsed > current {
		return parsed
	}
	return current
}
