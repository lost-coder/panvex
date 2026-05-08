// internal/controlplane/discovered/types_test.go
package discovered

import "testing"

func TestStatus_KnownValues(t *testing.T) {
	for _, s := range []Status{StatusPending, StatusAdopted, StatusIgnored} {
		if string(s) == "" {
			t.Fatalf("Status %q is empty", s)
		}
	}
}
