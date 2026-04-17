package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Credentials stores the persisted agent identity and mTLS bundle.
type Credentials struct {
	AgentID        string    `json:"agent_id"`
	CertificatePEM string    `json:"certificate_pem"`
	PrivateKeyPEM  string    `json:"private_key_pem"`
	CAPEM          string    `json:"ca_pem"`
	PanelURL       string    `json:"panel_url,omitempty"`
	GRPCEndpoint   string    `json:"grpc_endpoint"`
	GRPCServerName string    `json:"grpc_server_name"`
	ExpiresAt      time.Time `json:"expires_at"`
	// UsageSeq is the last client-usage snapshot sequence number emitted by
	// the agent. Persisted across restarts so the control-plane can dedup
	// replayed deltas and detect true agent restarts (seq resets to 1).
	// See P2-LOG-06 / L-07.
	UsageSeq uint64 `json:"usage_seq,omitempty"`
}

// Load reads persisted agent credentials from disk.
func Load(path string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, err
	}

	var credentials Credentials
	if err := json.Unmarshal(data, &credentials); err != nil {
		return Credentials{}, err
	}

	return credentials, nil
}

// Save writes persisted agent credentials to disk with restricted permissions.
func Save(path string, credentials Credentials) error {
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// SaveUsageSeq rewrites the state file with a new UsageSeq value while
// preserving all other persisted credential fields. Used on the hot path after
// every client-usage snapshot — callers should therefore only invoke it when
// the sequence actually advances, not on every snapshot attempt.
//
// The read-modify-write is intentional: callers don't hold the full Credentials
// struct on the usage-snapshot path, and we must not clobber the mTLS bundle.
// See P2-LOG-06 / L-07.
func SaveUsageSeq(path string, seq uint64) error {
	existing, err := Load(path)
	if err != nil {
		return err
	}
	if existing.UsageSeq == seq {
		return nil
	}
	existing.UsageSeq = seq
	return Save(path, existing)
}
