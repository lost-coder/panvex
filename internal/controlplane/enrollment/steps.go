// Package enrollment records and exposes the per-attempt timeline of every
// agent enrollment, both inbound (agent dials panel) and outbound (panel
// dials agent), so operators can see why a connection succeeded or failed.
package enrollment

// Mode identifies which direction of the connection an attempt represents.
type Mode string

const (
	ModeInbound  Mode = "inbound"
	ModeOutbound Mode = "outbound"
)

// Level is the severity of a single event inside an attempt.
type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Status is the lifecycle state of an attempt as stored in the database.
type Status string

const (
	StatusInProgress Status = "in_progress"
	StatusSuccess    Status = "success"
	StatusFailed     Status = "failed"
)

// Step is the stable identifier of one stage in an enrollment timeline.
// New values may be appended; existing values must never be renamed because
// the UI maps them to localized copy.
type Step string

const (
	StepBootstrapRequestReceived Step = "bootstrap_request_received"
	StepTokenValidated           Step = "token_validated"
	StepCSRReceived              Step = "csr_received"
	StepCSRValidated             Step = "csr_validated"
	StepCertSigned               Step = "cert_signed"
	StepCertReturned             Step = "cert_returned"
	StepAgentPersistedCert       Step = "agent_persisted_cert"
	StepGatewayDialed            Step = "gateway_dialed"
	StepTLSHandshakeOK           Step = "tls_handshake_ok"
	StepFirstSyncOK              Step = "first_sync_ok"

	StepInstallCommandIssued    Step = "install_command_issued"
	StepOutboundListenerStarted Step = "outbound_listener_started"
	StepPanelDialAttempted      Step = "panel_dial_attempted"
)
