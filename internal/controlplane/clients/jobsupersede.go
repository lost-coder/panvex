package clients

import (
	"encoding/json"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

// JobSupersedeKey is the clients-domain supersede rule for orchestration
// jobs, injected into jobs.Service via SetSupersedeKeyFunc (P8.1, audit
// #24 — jobs.Service no longer knows client actions or payload shapes).
//
// A client.* payload is frozen at enqueue time and carries the FULL desired
// state of one Telemt client (the agent applies it as an upsert, D4), so two
// jobs sharing a client_id compete: the newer one supersedes still-pending
// targets of the older. client.reset_quota is deliberately excluded — it is
// a counter reset, not a desired-state upsert; replaying an older reset
// after a newer one is harmless.
//
// Returns "" (no supersession) for non-client actions, empty payloads, and
// payloads that do not parse or carry no client_id — same tolerance as the
// pre-P8.1 jobs-internal rule.
func JobSupersedeKey(action jobs.Action, payloadJSON string) string {
	switch action {
	case jobs.ActionClientCreate, jobs.ActionClientUpdate, jobs.ActionClientDelete, jobs.ActionClientRotateSecret:
	default:
		return ""
	}
	if payloadJSON == "" {
		return ""
	}
	var probe struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &probe); err != nil {
		return ""
	}
	return probe.ClientID
}
