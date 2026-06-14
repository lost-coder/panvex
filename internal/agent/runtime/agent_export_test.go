package runtime

// UsageSeq exposes the last emitted client-usage sequence number to
// tests. Production code has no callers (audit 2026-06-09, B7).
func (a *Agent) UsageSeq() uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.usageSeq
}
