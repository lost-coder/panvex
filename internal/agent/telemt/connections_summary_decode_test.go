package telemt

import (
	"encoding/json"
	"testing"
)

// Contract-audit F3: Telemt's /v1/runtime/connections/summary top-N rows
// (runtime_edge.rs RuntimeEdgeConnectionUserData) carry
// username / current_connections / total_octets for BOTH lists. The agent
// used to decode `connections` and `throughput_bytes`, which do not exist
// in the payload — every top row arrived with correct usernames and
// all-zero values.
func TestConnectionSummaryTopDecodesTelemtFieldNames(t *testing.T) {
	payload := []byte(`{
		"by_connections": [
			{"username": "alice", "current_connections": 7, "total_octets": 4096}
		],
		"by_throughput": [
			{"username": "bob", "current_connections": 2, "total_octets": 99999}
		]
	}`)

	var top connectionSummaryTop
	if err := json.Unmarshal(payload, &top); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if len(top.ByConnections) != 1 || len(top.ByThroughput) != 1 {
		t.Fatalf("rows = %d/%d, want 1/1", len(top.ByConnections), len(top.ByThroughput))
	}
	if got := top.ByConnections[0].Connections; got != 7 {
		t.Fatalf("ByConnections[0].Connections = %d, want 7 (must decode current_connections)", got)
	}
	if got := top.ByThroughput[0].ThroughputBytes; got != 99999 {
		t.Fatalf("ByThroughput[0].ThroughputBytes = %d, want 99999 (must decode total_octets)", got)
	}
}
