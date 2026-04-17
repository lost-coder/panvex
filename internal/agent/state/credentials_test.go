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
		GRPCEndpoint:   "panel.example.com:8443",
		GRPCServerName: "panel.example.com",
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
	if loaded.GRPCEndpoint != credentials.GRPCEndpoint {
		t.Fatalf("loaded.GRPCEndpoint = %q, want %q", loaded.GRPCEndpoint, credentials.GRPCEndpoint)
	}
	if loaded.GRPCServerName != credentials.GRPCServerName {
		t.Fatalf("loaded.GRPCServerName = %q, want %q", loaded.GRPCServerName, credentials.GRPCServerName)
	}
}

// TestSaveUsageSeqPreservesOtherFields guards P2-LOG-06 / L-07: updating the
// usage sequence from the hot snapshot path must not clobber the mTLS bundle
// or any other persisted credential fields.
func TestSaveUsageSeqPreservesOtherFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-state.json")
	original := Credentials{
		AgentID:        "agent-1",
		CertificatePEM: "cert",
		PrivateKeyPEM:  "key",
		CAPEM:          "ca",
		GRPCEndpoint:   "panel.example.com:8443",
		GRPCServerName: "panel.example.com",
		ExpiresAt:      time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC),
		UsageSeq:       3,
	}
	if err := Save(path, original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := SaveUsageSeq(path, 42); err != nil {
		t.Fatalf("SaveUsageSeq() error = %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.UsageSeq != 42 {
		t.Fatalf("loaded.UsageSeq = %d, want %d", loaded.UsageSeq, 42)
	}
	if loaded.CertificatePEM != original.CertificatePEM {
		t.Fatalf("loaded.CertificatePEM = %q, want %q", loaded.CertificatePEM, original.CertificatePEM)
	}
	if loaded.PrivateKeyPEM != original.PrivateKeyPEM {
		t.Fatalf("loaded.PrivateKeyPEM = %q, want %q", loaded.PrivateKeyPEM, original.PrivateKeyPEM)
	}
	if loaded.GRPCEndpoint != original.GRPCEndpoint {
		t.Fatalf("loaded.GRPCEndpoint = %q, want %q", loaded.GRPCEndpoint, original.GRPCEndpoint)
	}
	if !loaded.ExpiresAt.Equal(original.ExpiresAt) {
		t.Fatalf("loaded.ExpiresAt = %v, want %v", loaded.ExpiresAt, original.ExpiresAt)
	}
}
