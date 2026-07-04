package runtime

// BootID exposes the process boot identifier to tests (P4).
func (a *Agent) BootID() string { return a.bootID }

// UsageTotalForTest returns the accumulated cumulative total for one
// tracking key (client id, or "name:<client_name>" fallback).
func (a *Agent) UsageTotalForTest(trackingKey string) uint64 {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.usageTotals[trackingKey]
}
