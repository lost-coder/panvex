package runtime

import (
	"sync"

	"github.com/lost-coder/panvex/internal/configcanon"
)

// observedConfigReporter delta-gates the observed managed-config report: it always
// returns the current canonical hash, and returns the full canonical JSON only
// when the hash changed since the last call. Mutex-guarded, mirroring the
// neighbouring contentHashGate (diagnostics_gate.go): BuildRuntimeSnapshot is
// reachable concurrently from the runtime poll worker, the
// telemetry.refresh_diagnostics job worker, and the initial sync (audit #6).
type observedConfigReporter struct {
	mu       sync.Mutex
	lastHash string
}

// next returns (hash, jsonOrEmpty) for the given current editable sections.
func (o *observedConfigReporter) next(sections map[string]any) (string, string) {
	hash := configcanon.Hash(sections)

	o.mu.Lock()
	defer o.mu.Unlock()
	if hash == o.lastHash {
		return hash, ""
	}
	o.lastHash = hash
	if sections == nil {
		sections = map[string]any{}
	}
	return hash, string(configcanon.CanonicalBytes(sections))
}
