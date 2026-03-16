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
	GRPCEndpoint   string    `json:"grpc_endpoint"`
	GRPCServerName string    `json:"grpc_server_name"`
	ExpiresAt      time.Time `json:"expires_at"`
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

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}
