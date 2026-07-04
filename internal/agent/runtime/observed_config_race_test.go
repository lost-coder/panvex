package runtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// racingTelemt returns a different managed config on every call so each
// BuildRuntimeSnapshot both reads AND writes observedConfigReporter.lastHash.
type racingTelemt struct {
	fakeTelemtClient
	calls atomic.Uint64
}

func (r *racingTelemt) GetManagedConfig(context.Context) (map[string]any, string, error) {
	n := r.calls.Add(1)
	return map[string]any{"general": map[string]any{"call": n}}, "rev", nil
}

// TestBuildRuntimeSnapshotConcurrentObservedConfig reproduces the real
// concurrency shape: runtime poll worker + telemetry.refresh_diagnostics
// job worker + initial sync all call BuildRuntimeSnapshot on one Agent.
// Run with -race; before the fix this reports a data race on
// observedConfigReporter.lastHash (audit #6).
func TestBuildRuntimeSnapshotConcurrentObservedConfig(t *testing.T) {
	agent := New(Config{AgentID: "agent-1", NodeName: "n1", Version: "test"}, &racingTelemt{})

	var wg sync.WaitGroup
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if _, err := agent.BuildRuntimeSnapshot(context.Background(), time.Now()); err != nil {
					t.Errorf("BuildRuntimeSnapshot: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
