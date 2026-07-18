// Package clientjob defines the JSON wire contract for client.* rollout jobs
// exchanged between the control-plane and the agent inside the
// gatewayrpc.JobCommand.payload_json field.
//
// It is a stdlib-only leaf package imported by BOTH sides — the producer
// (internal/controlplane/clients, which marshals the payload into a job) and
// the consumer (internal/agent/runtime, which unmarshals it and applies it to
// the local Telemt instance). Single-sourcing the struct here is the whole
// point: before this package the two sides carried hand-mirrored copies kept
// "in lockstep" by comments, so a field rename or JSON-tag change on one side
// compiled cleanly on both and only surfaced at runtime as a silent
// json.Unmarshal drift (dropped field, or a job failing with "invalid ...
// payload") once a real agent ran a real job. With a single definition such a
// rename is a compile error on whichever side misses it.
//
// Wire compatibility: the JSON tags below ARE the contract. Renaming a tag is a
// breaking change across the panel↔agent boundary — a mixed fleet mid-rollout
// would have panels emitting the new tag and older agents decoding the old one.
// Add fields (agents ignore unknown keys) rather than renaming existing ones.
package clientjob

// Payload is the envelope for the client.create / client.update /
// client.rotate_secret / client.delete jobs. It carries the full desired state
// of one managed Telemt client so the agent can converge idempotently
// regardless of which action delivered it.
type Payload struct {
	ClientID          string `json:"client_id"`
	Name              string `json:"name"`
	Secret            string `json:"secret"`
	UserADTag         string `json:"user_ad_tag"`
	Enabled           bool   `json:"enabled"`
	MaxTCPConns       int    `json:"max_tcp_conns"`
	MaxUniqueIPs      int    `json:"max_unique_ips"`
	DataQuotaBytes    int64  `json:"data_quota_bytes"`
	ExpirationRFC3339 string `json:"expiration_rfc3339"`

	// NameOwnerByAgent is set on client.delete ONLY: agentID -> the ID of the
	// client that, in panel state, currently owns Name on THAT agent. Absent
	// for every agent where nobody but the client being deleted holds the name
	// — which is the overwhelmingly common case, so the map is normally
	// omitted entirely.
	//
	// It exists because the agent cannot answer "did this name change hands?"
	// on its own after a restart: its clientID->name registry is cold, and the
	// only other local evidence — the secret Telemt holds for the user —
	// mismatches for TWO indistinguishable reasons (someone took the name, OR
	// a rotate for this same client never reached the node and was superseded
	// by this delete). The panel knows which, so it says so.
	//
	// PER-AGENT, not global: one delete job fans out to every agent that still
	// hosts the client with ONE shared payload, and the Telemt user "alice" on
	// agent-2 belongs to whichever client is deployed to agent-2. A name
	// re-used on agent-1 must never suppress the delete on agent-2 — that
	// would strand a live user the panel reports as deleted. Hence a map
	// rather than a bool or a bare owner ID.
	NameOwnerByAgent map[string]string `json:"name_owner_by_agent,omitempty"`
}

// ResetQuotaPayload is the envelope for a client.reset_quota job. Only the
// Telemt username is needed; the panel resolves it from the client's
// centrally-stored Name. It is structurally distinct from Payload so a
// counter-reset never has to (and never gets to) resend the client Secret.
type ResetQuotaPayload struct {
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
}
