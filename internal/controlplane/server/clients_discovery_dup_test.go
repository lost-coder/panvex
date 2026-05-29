package server

import (
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
)

// TestCollectDuplicateDiscoveredIDsRequiresNameAndSecret guards IN-H7: a
// discovered record sharing the secret but NOT the name must not be flipped
// to adopted (it represents a genuinely different, still-unmanaged user).
func TestCollectDuplicateDiscoveredIDsRequiresNameAndSecret(t *testing.T) {
	const secret = "00000000000000000000000000000000"
	all := []discovered.DiscoveredClient{
		{ID: "primary", ClientName: "alice", Secret: secret, Status: discovered.StatusPending, AgentID: "a1"},
		{ID: "sibling-same", ClientName: "alice", Secret: secret, Status: discovered.StatusPending, AgentID: "a2"},
		{ID: "secret-reuse-other-name", ClientName: "bob", Secret: secret, Status: discovered.StatusPending, AgentID: "a3"},
		{ID: "already-adopted", ClientName: "alice", Secret: secret, Status: discovered.StatusAdopted, AgentID: "a4"},
		{ID: "other-secret", ClientName: "alice", Secret: "ffffffffffffffffffffffffffffffff", Status: discovered.StatusPending, AgentID: "a5"},
	}

	got := collectDuplicateDiscoveredIDs(all, "primary", "alice", secret)

	if len(got) != 1 || got[0] != "sibling-same" {
		t.Fatalf("collectDuplicateDiscoveredIDs = %v, want exactly [sibling-same] "+
			"(secret-reuse-other-name must NOT be flipped — different name)", got)
	}
}
