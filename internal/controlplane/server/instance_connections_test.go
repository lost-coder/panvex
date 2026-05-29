package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// TestInstanceConnectionsJSONKey guards IN-M3: the operator-facing instance
// DTO must expose the telemt connection counter under the "connections" JSON
// key (renamed from the misleading "connected_users") and the value must
// round-trip through the storage-record mappers unchanged.
func TestInstanceConnectionsJSONKey(t *testing.T) {
	dto := Instance{
		ID:          "instance-1",
		AgentID:     "agent-1",
		Name:        "telemt-a",
		Version:     "2026.03",
		Connections: 42,
		UpdatedAt:   time.Date(2026, time.March, 15, 8, 25, 0, 0, time.UTC),
	}

	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `"connections":42`) {
		t.Fatalf("instance JSON missing \"connections\":42, got %s", body)
	}
	if strings.Contains(body, "connected_users") {
		t.Fatalf("instance JSON still carries legacy connected_users key: %s", body)
	}

	// DTO -> storage record -> DTO must preserve the counter.
	record := instanceToRecord(dto)
	if record.Connections != 42 {
		t.Fatalf("instanceToRecord().Connections = %d, want 42", record.Connections)
	}
	roundTrip := instanceFromRecord(storage.InstanceRecord{
		ID:          record.ID,
		AgentID:     record.AgentID,
		Connections: record.Connections,
		UpdatedAt:   record.UpdatedAt,
	})
	if roundTrip.Connections != 42 {
		t.Fatalf("instanceFromRecord().Connections = %d, want 42", roundTrip.Connections)
	}
}
