package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TransportSnapshot captures the transport-relevant fields as they were
// immediately before a switch_transport_mode change, so the agent can roll
// back if the panel never establishes a session within the probation window
// (audit A2: one-way trip to a dead listen mode).
type TransportSnapshot struct {
	Mode           string `json:"mode"`
	ListenAddr     string `json:"listen_addr,omitempty"`
	GRPCEndpoint   string `json:"grpc_endpoint,omitempty"`
	GRPCServerName string `json:"grpc_server_name,omitempty"`
}

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
	// PrevTransport + TransportSwitchedAtUnix implement transport-switch
	// probation (A2). Set by UpdateTransport on a mode change; cleared on
	// the first established panel session; consumed by
	// maybeRevertTransportSwitch when the window expires with no session.
	PrevTransport           *TransportSnapshot `json:"prev_transport,omitempty"`
	TransportSwitchedAtUnix int64              `json:"transport_switched_at_unix,omitempty"`
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

// mu serialises every access to the state file within the agent
// process. All writers (usage-seq ticks, cert renewals, transport
// switches, probation transitions) funnel through Save/Update under
// this lock, so a renewal can no longer be clobbered by a concurrent
// read-modify-write that loaded the bundle before the renewal saved it
// (audit 2026-07-02 #7).
var mu sync.Mutex

// Load reads persisted agent credentials from disk.
func Load(path string) (Credentials, error) {
	mu.Lock()
	defer mu.Unlock()
	return loadLocked(path)
}

func loadLocked(path string) (Credentials, error) {
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
//
// Callers that modify an EXISTING state file must use Update instead:
// a bare Save persists the caller's in-memory copy verbatim and would
// silently drop fields another writer changed since that copy was loaded.
func Save(path string, credentials Credentials) error {
	mu.Lock()
	defer mu.Unlock()
	return saveLocked(path, credentials)
}

func saveLocked(path string, credentials Credentials) error {
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

// Update loads the state file, applies mutate to the freshly loaded
// bundle, and persists the result — atomically under the package write
// lock. It returns the persisted credentials so callers can refresh
// their in-memory copy from the on-disk truth (which may carry
// concurrent changes to unrelated fields).
func Update(path string, mutate func(*Credentials)) (Credentials, error) {
	mu.Lock()
	defer mu.Unlock()
	current, err := loadLocked(path)
	if err != nil {
		return Credentials{}, err
	}
	mutate(&current)
	if err := saveLocked(path, current); err != nil {
		return Credentials{}, err
	}
	return current, nil
}

// SaveUsageSeq rewrites the state file with a new UsageSeq value while
// preserving all other persisted credential fields. Used on the hot path
// after every client-usage snapshot. NOTE: the whole usage-seq protocol
// is scheduled for removal in remediation plan P4 (cumulative counters);
// until then this must stay race-free with concurrent renewals.
// See P2-LOG-06 / L-07 and audit 2026-07-02 #7.
func SaveUsageSeq(path string, seq uint64) error {
	_, err := Update(path, func(c *Credentials) { c.UsageSeq = seq })
	return err
}
