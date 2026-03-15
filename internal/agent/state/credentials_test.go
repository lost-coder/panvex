package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadCredentialsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-state.json")
	credentials := Credentials{
		AgentID:        "agent-1",
		CertificatePEM: "cert",
		PrivateKeyPEM:  "key",
		CAPEM:          "ca",
		ExpiresAt:      time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC),
	}

	if err := Save(path, credentials); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.AgentID != credentials.AgentID {
		t.Fatalf("loaded.AgentID = %q, want %q", loaded.AgentID, credentials.AgentID)
	}
}
