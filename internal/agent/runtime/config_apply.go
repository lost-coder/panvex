package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lost-coder/panvex/internal/agent/atomicfile"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// backupConfigFile copies path to "<path>.panvex.bak" and returns the backup
// path. Used before a config patch so a failed reload can be rolled back.
func backupConfigFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: agent-configured Telemt config path, not untrusted input
	if err != nil {
		return "", fmt.Errorf("read config for backup: %w", err)
	}
	backup := path + ".panvex.bak"
	if err := atomicfile.Write(backup, data, 0o600, ".panvex-cfg-*.tmp"); err != nil {
		return "", fmt.Errorf("write config backup: %w", err)
	}
	return backup, nil
}

// configApplyRollbackTimeout bounds the detached rollback path (both the hot
// file-watcher rollback and the Maestro reload rollback). Generous on
// purpose — rollback is the last line of defence and must not be starved by
// the job deadline.
const configApplyRollbackTimeout = 60 * time.Second

// hotRollbackConfig restores the backup over the live config and waits for
// Telemt's file watcher to pick it up and report healthy again. Used for the
// hot-change paths (no runtime reload involved): the reload-not-required
// branch and the old-Telemt hot branch. H1: a hot patch that leaves Telemt
// unhealthy must be reverted, not reported as a false success.
func hotRollbackConfig(ctx context.Context, d configApplyDeps, backup string) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), configApplyRollbackTimeout)
	defer cancel()
	if err := restoreConfigFile(backup, d.configPath); err != nil {
		return fmt.Errorf("rollback write failed: %w", err)
	}
	if ok, reason := waitHealthy(rollbackCtx, d); !ok {
		return fmt.Errorf("still unhealthy after restoring backup: %s", reason)
	}
	return nil
}

// restoreConfigFile atomically copies backup back over path.
func restoreConfigFile(backup, path string) error {
	data, err := os.ReadFile(backup) //nolint:gosec // G304: backup of the agent-configured Telemt config, not untrusted input
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	return atomicfile.Write(path, data, 0o600, ".panvex-cfg-*.tmp")
}

// configApplyPayload is the JSON body of a config.apply job.
type configApplyPayload struct {
	ExpectedRevision string         `json:"expected_revision,omitempty"`
	Patch            map[string]any `json:"patch"`
	HealthTimeoutSec int            `json:"health_timeout_s,omitempty"`
	// ReloadMode is "instant" or "drain"; empty means "drain" with
	// DefaultReloadDrainSecs. Only consulted when Telemt reports
	// runtime_reload_required=true.
	ReloadMode string `json:"reload_mode,omitempty"`
	// ReloadTimeoutSecs is the drain timeout in seconds; only meaningful for
	// ReloadMode == "drain".
	ReloadTimeoutSecs int `json:"reload_timeout_secs,omitempty"`
}

// telemtConfigPort is the subset of the telemt client the orchestrator needs.
type telemtConfigPort interface {
	PatchConfig(ctx context.Context, patch map[string]any, expectedRevision string) (telemt.PatchConfigResult, error)
	GetManagedConfig(ctx context.Context) (map[string]any, string, error)
	HealthReady(ctx context.Context) (bool, string, error)
	SubmitReload(ctx context.Context, mode string, timeoutSecs int, failurePolicy, ifMatchRevision string) (telemt.ReloadAccepted, error)
	GetReloadStatus(ctx context.Context, reloadID uint64) (telemt.ReloadStatus, error)
}

const (
	// DefaultReloadDrainSecs is the drain timeout used when the caller asks
	// for a reload but does not name a mode (defaults to "drain").
	DefaultReloadDrainSecs = 30
	// reloadPollInterval is the default spacing between GetReloadStatus polls.
	reloadPollInterval = 500 * time.Millisecond
	// reloadPollDeadline bounds how long we poll for a reload to reach a
	// draining/succeeded/failed/rolled_back state before giving up. NOTE:
	// this is a POLLING deadline, not the reload's own drain timeout (which
	// can be up to 3600s server-side) — we stop watching, we don't cancel
	// the reload.
	reloadPollDeadline = 60 * time.Second
	// reloadBusyRetries caps how many times a 409 reload_in_progress is
	// retried with backoff before giving up.
	reloadBusyRetries = 3
)

// reloadBusyBackoff is the default backoff schedule for reloadBusyRetries.
var reloadBusyBackoff = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}

type configApplyDeps struct {
	telemt         telemtConfigPort
	configPath     string
	healthAttempts int
	healthInterval time.Duration
	reloadPoll     time.Duration   // 0 => reloadPollInterval
	reloadDeadline time.Duration   // 0 => reloadPollDeadline
	reloadBackoff  []time.Duration // nil => reloadBusyBackoff
}

type configApplyResult struct {
	success    bool
	message    string
	revision   string
	keepBackup bool
}

// runConfigApply applies a config patch with backup + health-gated rollback.
// Telemt tells us, per patch, whether the change was already picked up by
// its file watcher (no reload needed), needs an in-process Maestro reload to
// take effect, or — on a pre-Maestro Telemt — needs a process restart we can
// no longer perform. See the three-branch flow below.
func runConfigApply(ctx context.Context, d configApplyDeps, p configApplyPayload) configApplyResult {
	if len(p.Patch) == 0 {
		return configApplyResult{message: "config.apply: empty patch"}
	}
	if d.healthAttempts == 0 {
		d.healthAttempts = 30
	}
	if d.healthInterval == 0 {
		d.healthInterval = time.Second
	}

	if ready, _, err := d.telemt.HealthReady(ctx); err != nil || !ready {
		return configApplyResult{message: fmt.Sprintf("config.apply: preflight health check failed (ready=%v err=%v)", ready, err)}
	}

	// D3: every apply must be a genuine compare-and-swap against Telemt's
	// current revision, never a blind write. Callers (control-plane job
	// enqueue, UI) are not required to supply expected_revision — if it's
	// absent the agent reads the live revision itself and uses that as the
	// If-Match, so a retried/duplicated config.apply job (at-least-once job
	// delivery) lands on the revision it actually observed rather than
	// bypassing Telemt's 409 guard.
	expectedRevision := p.ExpectedRevision
	if expectedRevision == "" {
		_, currentRevision, err := d.telemt.GetManagedConfig(ctx)
		if err != nil {
			return configApplyResult{message: fmt.Sprintf("config.apply: fetch current revision failed: %v", err)}
		}
		expectedRevision = currentRevision
		slog.DebugContext(ctx, "config.apply: no expected_revision supplied, using agent-observed revision for CAS",
			slog.String("revision", expectedRevision))
	}

	backup, err := backupConfigFile(d.configPath)
	if err != nil {
		return configApplyResult{message: fmt.Sprintf("config.apply: backup failed: %v", err)}
	}
	// .panvex.bak must never leak on a path where the live config ends up in a
	// KNOWN state: success (hot or reload), or a rollback that actually
	// restored it. On a path where a restore/rollback was attempted and
	// FAILED, the live config, the running Telemt, and the backup can all
	// disagree — keepBackup is set to true on every such branch below so the
	// on-disk backup survives as the only recovery artifact, and that
	// branch's message names its path and calls out manual recovery.
	keepBackup := false
	defer func() {
		if !keepBackup {
			_ = os.Remove(backup)
		}
	}()

	res, err := d.telemt.PatchConfig(ctx, p.Patch, expectedRevision)
	if err != nil {
		if errors.Is(err, telemt.ErrConfigRevisionConflict) {
			slog.WarnContext(ctx, "config.apply: revision conflict, config changed underneath",
				slog.String("expected_revision", expectedRevision))
			return configApplyResult{message: "config.apply: revision conflict — config changed underneath since expected_revision was read; re-fetch and retry"}
		}
		return configApplyResult{message: fmt.Sprintf("config.apply: patch failed: %v", err)}
	}

	// Branch 1 — old Telemt (no runtime_reload_required field).
	if res.RuntimeReloadRequired == nil {
		if res.RestartRequired {
			if rErr := restoreConfigFile(backup, d.configPath); rErr != nil {
				keepBackup = true
				return configApplyResult{message: fmt.Sprintf("config.apply: change needs a restart but this Telemt is older than 3.4.25 (no in-process reload); restore failed: %v; manual recovery required, backup kept at %s", rErr, backup), keepBackup: true}
			}
			return configApplyResult{message: "config.apply: change needs a restart but this Telemt is older than 3.4.25 (no in-process reload); reverted — upgrade Telemt to 3.4.25+"}
		}
		// Hot change on an old node: really applied by the file watcher.
		result := afterHotApply(ctx, d, backup, res.Revision, "config applied (hot reload; node on Telemt older than 3.4.25)")
		if result.keepBackup {
			keepBackup = true
		}
		return result
	}

	// Branch 2 — reload not required: file watcher already applied it.
	if !*res.RuntimeReloadRequired {
		result := afterHotApply(ctx, d, backup, res.Revision, "config applied (hot reload, no runtime reload)")
		if result.keepBackup {
			keepBackup = true
		}
		return result
	}

	// Branch 3 — reload required.
	mode := p.ReloadMode
	timeout := p.ReloadTimeoutSecs
	if mode == "" {
		mode = "drain"
		timeout = DefaultReloadDrainSecs
	}
	if ok, reason, submitErr := submitAndAwaitReload(ctx, d, mode, timeout, "rollback", res.Revision); !ok {
		if rErr := restoreConfigFile(backup, d.configPath); rErr != nil {
			keepBackup = true
			return configApplyResult{message: fmt.Sprintf("config.apply: reload failed (%s); restore failed: %v; manual recovery required, backup kept at %s", reason, rErr, backup), keepBackup: true}
		}
		return configApplyResult{message: fmt.Sprintf("config.apply: reload failed (%s); reverted%s", reason, submitErrNote(submitErr))}
	}

	// Reload reached draining/succeeded — the new generation is already
	// active. Confirm the node is actually healthy; do NOT wait for
	// "succeeded" first, since draining can legitimately run for up to the
	// full drain timeout (up to 3600s) after the switch has already happened.
	if ok, healthReason := waitHealthy(ctx, d); !ok {
		if rbErr := rollbackReload(ctx, d, backup); rbErr != nil {
			keepBackup = true
			return configApplyResult{message: fmt.Sprintf("config.apply: unhealthy after reload (%s); rollback-reload failed: %v; manual recovery required, backup kept at %s", healthReason, rbErr, backup), keepBackup: true}
		}
		return configApplyResult{message: fmt.Sprintf("config.apply: unhealthy after reload (%s); rolled back", healthReason)}
	}

	msg := "config applied (in-process reload)"
	if res.ProcessRestartRequired {
		msg += fmt.Sprintf("; note: %v need a process restart and were NOT applied", res.DeferredProcessFields)
	}
	return configApplyResult{success: true, revision: res.Revision, message: msg}
}

// afterHotApply runs the post-hot health check and hot-rollback, shared by
// the reload-not-required and old-Telemt-hot branches.
func afterHotApply(ctx context.Context, d configApplyDeps, backup, revision, successMsg string) configApplyResult {
	if ok, reason := waitHealthy(ctx, d); !ok {
		if rbErr := hotRollbackConfig(ctx, d, backup); rbErr != nil {
			return configApplyResult{message: fmt.Sprintf("config.apply: unhealthy after hot reload (%s); rollback failed: %v; manual recovery required, backup kept at %s", reason, rbErr, backup), keepBackup: true}
		}
		return configApplyResult{message: fmt.Sprintf("config.apply: unhealthy after hot reload (%s); rolled back to previous config", reason)}
	}
	return configApplyResult{success: true, revision: revision, message: successMsg}
}

// submitAndAwaitReload submits a reload (retrying 409 reload_in_progress with
// backoff) and polls to a terminal-or-draining state. Returns ok=true once the
// new generation is active (draining or succeeded) — it deliberately does NOT
// wait for succeeded, since draining can legitimately last up to the reload's
// own drain timeout (up to 3600s) after the generation switch already
// happened. failurePolicy is "rollback" for a forward apply and "keep_new"
// for the rollback-reload.
func submitAndAwaitReload(ctx context.Context, d configApplyDeps, mode string, timeoutSecs int, failurePolicy, revision string) (bool, string, error) {
	backoff := d.reloadBackoff
	if backoff == nil {
		backoff = reloadBusyBackoff
	}
	maxRetries := reloadBusyRetries
	if len(backoff) < maxRetries {
		maxRetries = len(backoff)
	}

	var acc telemt.ReloadAccepted
	var err error
	for attempt := 0; ; attempt++ {
		acc, err = d.telemt.SubmitReload(ctx, mode, timeoutSecs, failurePolicy, revision)
		if err == nil {
			break
		}
		if errors.Is(err, telemt.ErrReloadInProgress) && attempt < maxRetries {
			select {
			case <-ctx.Done():
				return false, "reload submit cancelled", err
			case <-time.After(backoff[attempt]):
			}
			continue
		}
		return false, fmt.Sprintf("reload submit: %v", err), err
	}

	poll := d.reloadPoll
	if poll == 0 {
		poll = reloadPollInterval
	}
	deadline := d.reloadDeadline
	if deadline == 0 {
		deadline = reloadPollDeadline
	}
	pollCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	for {
		st, sErr := d.telemt.GetReloadStatus(pollCtx, acc.ReloadID)
		if sErr == nil {
			switch st.State {
			case "draining", "succeeded":
				return true, "", nil
			case "failed", "rolled_back":
				return false, fmt.Sprintf("reload %s: %s", st.State, st.Error), nil
			}
		}
		select {
		case <-pollCtx.Done():
			// The Telemt-side state is UNKNOWN at this point — the reload may
			// still finish (successfully or not) after we stop watching. The
			// caller must NOT restore the on-disk file here: doing so could
			// contradict a reload that completes after the deadline.
			return false, "reload status poll deadline exceeded (state unknown)", pollCtx.Err()
		case <-time.After(poll):
		}
	}
}

// rollbackReload restores the backup file and submits an instant keep_new
// reload so the runtime returns to the previous config. Detached from the job
// ctx (mirrors the old rollbackConfig's V4 rationale). keep_new is correct
// here: we are reloading TO the known-good previous config, so there is
// nothing to roll forward from. The re-read revision is the restored file's,
// sent as If-Match.
func rollbackReload(ctx context.Context, d configApplyDeps, backup string) error {
	rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), reloadPollDeadline+configApplyRollbackTimeout)
	defer cancel()
	if err := restoreConfigFile(backup, d.configPath); err != nil {
		return fmt.Errorf("rollback restore failed: %w", err)
	}
	_, rev, err := d.telemt.GetManagedConfig(rbCtx)
	if err != nil {
		return fmt.Errorf("rollback re-read revision: %w", err)
	}
	if ok, reason, _ := submitAndAwaitReload(rbCtx, d, "instant", 0, "keep_new", rev); !ok {
		return fmt.Errorf("rollback reload did not activate: %s", reason)
	}
	return nil
}

func submitErrNote(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf(" (%v)", err)
}

// waitHealthy polls HealthReady until ready or attempts are exhausted. It
// returns the last observed reason/error string alongside the bool so
// callers can fold it into a failure message instead of discarding it (D1).
func waitHealthy(ctx context.Context, d configApplyDeps) (bool, string) {
	var lastReason string
	for i := 0; i < d.healthAttempts; i++ {
		ready, reason, err := d.telemt.HealthReady(ctx)
		if err == nil && ready {
			return true, ""
		}
		if err != nil {
			lastReason = err.Error()
		} else {
			lastReason = reason
		}
		select {
		case <-ctx.Done():
			return false, lastReason
		case <-time.After(d.healthInterval):
		}
	}
	return false, lastReason
}

// handleConfigApplyJob runs a config.apply job against the local Telemt instance.
func (a *Agent) handleConfigApplyJob(ctx context.Context, job *gatewayrpc.JobCommand, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	payload, err := parseConfigApplyPayload(job.GetPayloadJson())
	if err != nil {
		result.Message = fmt.Sprintf("config.apply: invalid payload: %v", err)
		return result
	}
	deps := configApplyDeps{
		telemt:     a.telemt,
		configPath: a.resolveTelemtConfigPath(ctx),
	}
	if payload.HealthTimeoutSec > 0 {
		deps.healthInterval = time.Second
		deps.healthAttempts = payload.HealthTimeoutSec
	}
	out := runConfigApply(ctx, deps, payload)
	result.Success = out.success
	result.Message = out.message
	if out.revision != "" {
		result.ResultJson = marshalConfigApplyResult(out.revision)
	}
	return result
}

func marshalConfigApplyResult(revision string) string {
	b, err := json.Marshal(struct {
		Revision string `json:"revision"`
	}{Revision: revision})
	if err != nil {
		return ""
	}
	return string(b)
}

// parseConfigApplyPayload unmarshals a config.apply job payload.
func parseConfigApplyPayload(payloadJSON string) (configApplyPayload, error) {
	var p configApplyPayload
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		return configApplyPayload{}, err
	}
	return p, nil
}
