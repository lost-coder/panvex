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
	// TransportMode controls direction of the gRPC stream:
	//   "" / "dial" — agent dials the panel (default; legacy state files have no value)
	//   "listen"    — agent serves gRPC; the panel dials in
	TransportMode string `json:"transport_mode,omitempty"`
	// ListenAddr is the agent-side bind address used when TransportMode == "listen".
	// Ignored in dial mode.
	ListenAddr string `json:"listen_addr,omitempty"`
	// UsageSeq is the last client-usage snapshot sequence number emitted by
	// the agent. Persisted across restarts so the control-plane can dedup
	// replayed deltas and detect true agent restarts (seq resets to 1).
	// See P2-LOG-06 / L-07.
	UsageSeq uint64 `json:"usage_seq,omitempty"`
	// InsecureTransport records that this agent bootstrapped against a
	// plain-HTTP panel URL behind a trusted private link (e.g. VPN-only
	// deployment). Persisted so certificate recovery on later runs honors
	// the same transport relaxation without needing a CLI re-flag.
	InsecureTransport bool `json:"insecure_transport,omitempty"`
	// EnrollmentAttemptID identifies the panel-side enrollment attempt this
	// agent was minted under. The agent uses it to ship local steps
	// (cert persisted, gateway dialed, tls handshake ok) via the
	// ReportEnrollmentSteps RPC once the first sync is up. Empty for
	// agents that bootstrapped against a panel that pre-dates the
	// enrollment-logging timeline (Phase 1) — in that case the agent
	// silently skips reporting.
	EnrollmentAttemptID string `json:"enrollment_attempt_id,omitempty"`
	// AgentPersistedCertAt is the wall-clock time at which the bootstrap
	// command first wrote the credential bundle to disk. Captured here so
	// the runtime can stamp the agent_persisted_cert timeline event with
	// the original moment rather than the runtime start time. Zero value
	// for state files pre-dating Phase 1.
	AgentPersistedCertAt time.Time `json:"agent_persisted_cert_at,omitempty"`
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

// Save writes persisted agent credentials to disk with restricted
// permissions. The write is atomic: data lands in a temp file in the same
// directory, is fsynced, then renamed over the target. A crash, power loss,
// or full disk mid-write therefore leaves the previous credentials intact
// instead of a truncated bundle the agent cannot load on its next start
// (which, for an outbound agent whose bootstrap token was already cleared,
// would otherwise wedge enrollment).
func Save(path string, credentials Credentials) error {
	data, err := json.MarshalIndent(credentials, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".credentials-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// Best-effort cleanup; a no-op once the rename below succeeds.
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
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
