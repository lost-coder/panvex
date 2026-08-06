package updates

import "github.com/lost-coder/panvex/internal/controlplane/jobs"

// JobSupersedeKey is the updates-domain supersede rule for telemt.update
// jobs, injected into jobs.Service alongside clients.JobSupersedeKey (see
// the composition at server/lifecycle.go): a newer operator-dispatched
// Telemt update for an agent must replace a still-pending older one — the
// payload freezes the full desired update (version + strategy) at enqueue
// time, so redelivering a stale target after a newer click landed would
// install the wrong version (same D4 reasoning as client.* jobs).
//
// Unlike clients.JobSupersedeKey, this does not key off anything parsed from
// payloadJSON: telemtupdate.Payload deliberately carries no agent/client
// identifier (Task 5's exact-keys contract — version, release_base_url,
// allow_downgrade, restart_spec, binary_path, asset_flavor only), so there is
// nothing to extract. That is not a problem: telemt.update jobs are always
// single-target (one agent per job — see telemtUpdateJobInput), and
// jobs.Service's supersede pass already restricts its effect to targets the
// NEW job actually covers (per-agent intersection in
// supersedePendingJobsLocked/expireCoveredTargets). A key that is constant
// PER ACTION therefore behaves identically to a per-agent key here: an older
// telemt.update job for a DIFFERENT agent never shares a target with the new
// job and is left untouched regardless of key granularity.
func JobSupersedeKey(action jobs.Action, _ string) string {
	if action != jobs.ActionTelemtUpdate {
		return ""
	}
	return "telemt.update"
}
