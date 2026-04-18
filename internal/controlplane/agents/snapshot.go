package agents

import (
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// EnrollmentRequest is the input of the agent enrollment flow.
// Agents present a short-lived token plus their identity; the control-plane
// returns an mTLS identity wrapped in an EnrollmentResponse.
type EnrollmentRequest struct {
	Token    string
	NodeName string
	Version  string
}

// EnrollmentResponse is the mTLS identity issued to a newly enrolled agent.
type EnrollmentResponse struct {
	AgentID        string
	CertificatePEM string
	PrivateKeyPEM  string
	CAPEM          string
	ExpiresAt      time.Time
}

// InstanceSnapshot describes one Telemt instance that an agent reports on.
type InstanceSnapshot struct {
	ID                string
	Name              string
	Version           string
	ConfigFingerprint string
	ConnectedUsers    int
	ReadOnly          bool
}

// Snapshot is the full per-tick payload sent by an agent over the
// gRPC gateway. See controlplane/server/agent_flow.go for the reducer
// that applies it to in-memory and persisted state.
type Snapshot struct {
	AgentID                  string
	NodeName                 string
	FleetGroupID             string
	Version                  string
	ReadOnly                 bool
	Instances                []InstanceSnapshot
	Clients                  []ClientUsageSnapshot
	HasClients               bool
	ClientIPs                []ClientIPSnapshot
	HasClientIPs             bool
	Runtime                  *gatewayrpc.RuntimeSnapshot
	HasRuntime               bool
	RuntimeDiagnostics       *gatewayrpc.RuntimeDiagnosticsSnapshot
	RuntimeSecurityInventory *gatewayrpc.RuntimeSecurityInventorySnapshot
	Metrics                  map[string]uint64
	ObservedAt               time.Time
}

// ClientUsageSnapshot is the per-client Telemt usage delta published by
// an agent. Seq is the monotonic per-agent snapshot sequence number;
// seq == 0 denotes "unknown" (legacy agent or synthetic entry) and the
// dedup path accumulates unconditionally in that case.
type ClientUsageSnapshot struct {
	ClientID         string
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
	ActiveUniqueIPs  int
	ObservedAt       time.Time
	Seq              uint64
}

// ClientIPSnapshot publishes the set of IPs currently seen on each client.
type ClientIPSnapshot struct {
	ClientID  string
	ActiveIPs []string
}

// RuntimeLifecycleState derives the coarse-grained lifecycle label used
// by the UI from the raw gateway runtime snapshot. Pure helper.
func RuntimeLifecycleState(snapshot *gatewayrpc.RuntimeSnapshot) string {
	switch {
	case snapshot == nil:
		return "unknown"
	case snapshot.Degraded:
		return "degraded"
	case snapshot.InitializationStatus != "" && snapshot.InitializationStatus != "ready":
		return snapshot.InitializationStatus
	case snapshot.StartupStatus != "" && snapshot.StartupStatus != "ready":
		return snapshot.StartupStatus
	case !snapshot.AcceptingNewConnections || !snapshot.MeRuntimeReady:
		return "starting"
	default:
		return "ready"
	}
}
