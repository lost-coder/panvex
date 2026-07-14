package clients

import (
	"fmt"
)

// newSequenceID formats a prefixed monotonic ID ("client-0000042").
// Matches the layout used elsewhere in the control-plane.
func newSequenceID(prefix string, value uint64) string {
	return prefix + "-" + fmt.Sprintf("%07d", value)
}
