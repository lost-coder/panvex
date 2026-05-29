package server

import "testing"

// IN-M6: the per-interval IP snapshot is a monotonic UNION of every IP seen
// during the upload window (IPCollector only resets on Flush), so its length
// overstates "active now". Active-now is owned by the usage tick
// (CurrentIPsUsed → ActiveUniqueIPs); the IP snapshot must only feed
// client_ip_history (via enqueueClientIPHistory) and must NOT clobber the
// active-now gauge.
func TestApplyClientIPSnapshotDoesNotOverrideActiveNow(t *testing.T) {
	server := mustNew(t, Options{LoginTimingFloor: -1})
	t.Cleanup(server.Close)

	const clientID = "client-1"
	const agentID = "agent-1"

	// Usage tick established active-now = 2 (instantaneous, from telemt).
	server.clientUsage[clientID] = map[string]clientUsageSnapshot{
		agentID: {ClientID: clientID, ActiveUniqueIPs: 2},
	}

	// An IP snapshot accumulated 5 distinct IPs over the upload interval.
	server.applyClientIPSnapshot(agentID, []clientIPSnapshot{
		{ClientID: clientID, ActiveIPs: []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4", "5.5.5.5"}},
	})

	got := server.clientUsage[clientID][agentID].ActiveUniqueIPs
	if got != 2 {
		t.Fatalf("ActiveUniqueIPs = %d, want 2 (interval union of 5 must not overwrite active-now)", got)
	}
}
