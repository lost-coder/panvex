// Package telemtjob defines the JSON wire contract for the telemt.update job
// exchanged between the control-plane and the agent inside the
// gatewayrpc.JobCommand.payload_json field.
//
// It is a stdlib-only leaf package (mirrors internal/clientjob and
// internal/restartspec) imported by BOTH sides — the producer
// (internal/controlplane/server, which marshals the payload into a job) and
// the consumer (internal/agent/telemtupdate, which unmarshals it and drives
// the download/verify/swap/health-gate flow). Single-sourcing the struct
// here keeps a field rename or JSON-tag change on one side a compile error on
// the other, instead of a silent json.Unmarshal drift.
//
// It exists as its own package, separate from internal/agent/telemtupdate,
// because that package pulls in os/exec (telemtrestart), the Telemt HTTP
// client and golang.org/x/sys/unix — none of which belong in the panel
// binary. internal/controlplane/server imports ONLY this package, never
// internal/agent/telemtupdate.
//
// Wire compatibility: the JSON tags below ARE the contract. Renaming a tag is
// a breaking change across the panel<->agent boundary — a mixed fleet
// mid-rollout would have panels emitting the new tag and older agents
// decoding the old one. Add fields (agents ignore unknown keys) rather than
// renaming existing ones.
package telemtjob

// Payload is the JSON payload of a telemt.update job. The panel serializes
// exactly these keys (contract for the panel-side job producer and the
// agent-side job handler). Mirrors internal/agent/updater.Payload's shape:
// the panel supplies the release directory and target version, the agent
// resolves its own architecture/libc via ResolveAsset so the panel can never
// pick the wrong asset.
type Payload struct {
	Version        string `json:"version"`
	ReleaseBaseURL string `json:"release_base_url"`
	// AllowDowngrade lets an operator install an older binary on purpose
	// (e.g. emergency rollback). Without it Execute refuses any version
	// below the running one.
	AllowDowngrade bool `json:"allow_downgrade,omitempty"`
	// RestartSpec is parsed by telemtrestart.New — see internal/restartspec
	// for the accepted strategies (systemd/procd/openrc/runit/command).
	RestartSpec string `json:"restart_spec"`
	// BinaryPath is the path to the currently-running Telemt binary. Execute
	// stages downloads in filepath.Dir(BinaryPath) so the final swap stays
	// on one filesystem (see swapBinary's doc comment).
	BinaryPath string `json:"binary_path"`
	// AssetFlavor selects a CPU-feature-level release variant (currently
	// only "v3" on x86_64); "" means the architecture baseline. Passed
	// through to ResolveAsset verbatim.
	AssetFlavor string `json:"asset_flavor,omitempty"`
}
