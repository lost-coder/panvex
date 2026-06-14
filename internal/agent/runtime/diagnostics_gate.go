package runtime

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"sync"
)

// contentHashGate delta-gates a payload that is otherwise re-sent verbatim on
// every snapshot: callers feed the canonical field tuple and get back the
// hash plus a sendBody flag that is true only when the tuple changed since
// the previous call (D5). Mirrors observedConfigReporter (observed_config.go)
// but is mutex-guarded because BuildRuntimeSnapshot is reachable from both
// the runtime poll worker and the telemetry.refresh_diagnostics job worker.
type contentHashGate struct {
	mu       sync.Mutex
	lastHash string
}

// next hashes the field tuple and reports whether the body must be sent.
func (g *contentHashGate) next(fields ...string) (hash string, sendBody bool) {
	digest := sha256.New()
	var lenBuf [8]byte
	for _, field := range fields {
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(field)))
		digest.Write(lenBuf[:])
		digest.Write([]byte(field))
	}
	hash = hex.EncodeToString(digest.Sum(nil))

	g.mu.Lock()
	defer g.mu.Unlock()
	if hash == g.lastHash {
		return hash, false
	}
	g.lastHash = hash
	return hash, true
}

// reset forgets the last hash so the next call re-sends the full body.
// Called when the agent emits an unreachable snapshot: the panel blanks its
// stored row on that path, so the first post-recovery snapshot must carry
// the body again even if the diagnostics did not change across the outage.
func (g *contentHashGate) reset() {
	g.mu.Lock()
	g.lastHash = ""
	g.mu.Unlock()
}
