package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// backupConfigFile copies path to "<path>.panvex.bak" and returns the backup
// path. Used before a config patch so a failed restart can be rolled back.
func backupConfigFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read config for backup: %w", err)
	}
	backup := path + ".panvex.bak"
	if err := writeFileAtomic(backup, data, 0o600); err != nil {
		return "", fmt.Errorf("write config backup: %w", err)
	}
	return backup, nil
}

// restoreConfigFile atomically copies backup back over path.
func restoreConfigFile(backup, path string) error {
	data, err := os.ReadFile(backup)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	return writeFileAtomic(path, data, 0o600)
}

// writeFileAtomic writes data to a temp file in the same directory, fsyncs it,
// then renames it over path (crash-safe). Mirrors internal/agent/state/credentials.go.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".panvex-cfg-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// configApplyPayload is the JSON body of a config.apply job.
type configApplyPayload struct {
	ExpectedRevision string         `json:"expected_revision,omitempty"`
	Patch            map[string]any `json:"patch"`
	HealthTimeoutSec int            `json:"health_timeout_s,omitempty"`
}

// telemtConfigPort is the subset of the telemt client the orchestrator needs.
type telemtConfigPort interface {
	PatchConfig(ctx context.Context, patch map[string]any, expectedRevision string) (telemt.PatchConfigResult, error)
	HealthReady(ctx context.Context) (bool, string, error)
}

type configApplyDeps struct {
	telemt         telemtConfigPort
	restarter      configRestarter // may be nil
	configPath     string
	healthAttempts int
	healthInterval time.Duration
}

type configApplyResult struct {
	success  bool
	message  string
	revision string
}

// runConfigApply applies a config patch with backup + health-gated rollback.
// Hot changes apply live (Telemt's file watcher); restart-required changes are
// restarted by the agent, health-checked, and rolled back on failure.
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

	backup, err := backupConfigFile(d.configPath)
	if err != nil {
		return configApplyResult{message: fmt.Sprintf("config.apply: backup failed: %v", err)}
	}

	res, err := d.telemt.PatchConfig(ctx, p.Patch, p.ExpectedRevision)
	if err != nil {
		return configApplyResult{message: fmt.Sprintf("config.apply: patch failed: %v", err)}
	}

	if !res.RestartRequired {
		return configApplyResult{success: true, revision: res.Revision, message: "config applied (hot reload, no restart)"}
	}

	if d.restarter == nil {
		_ = restoreConfigFile(backup, d.configPath)
		return configApplyResult{message: "config.apply: change requires restart but no restart strategy is configured; reverted"}
	}

	if err := d.restarter.Restart(ctx); err != nil {
		_ = restoreConfigFile(backup, d.configPath)
		return configApplyResult{message: fmt.Sprintf("config.apply: restart failed: %v; reverted", err)}
	}

	if waitHealthy(ctx, d) {
		return configApplyResult{success: true, revision: res.Revision, message: "config applied with restart"}
	}

	if err := restoreConfigFile(backup, d.configPath); err != nil {
		return configApplyResult{message: fmt.Sprintf("config.apply: unhealthy after restart AND rollback write failed: %v", err)}
	}
	if err := d.restarter.Restart(ctx); err != nil {
		return configApplyResult{message: fmt.Sprintf("config.apply: unhealthy after restart; rollback restart failed: %v", err)}
	}
	return configApplyResult{message: "config.apply: unhealthy after restart; rolled back to previous config"}
}

// waitHealthy polls HealthReady until ready or attempts are exhausted.
func waitHealthy(ctx context.Context, d configApplyDeps) bool {
	for i := 0; i < d.healthAttempts; i++ {
		if ready, _, err := d.telemt.HealthReady(ctx); err == nil && ready {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d.healthInterval):
		}
	}
	return false
}

// handleConfigApplyJob runs a config.apply job against the local Telemt instance.
func (a *Agent) handleConfigApplyJob(ctx context.Context, job *gatewayrpc.JobCommand, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	payload, err := parseConfigApplyPayload(job.GetPayloadJson())
	if err != nil {
		result.Message = fmt.Sprintf("config.apply: invalid payload: %v", err)
		return result
	}
	out := runConfigApply(ctx, configApplyDeps{
		telemt:     a.telemt,
		restarter:  a.restarter,
		configPath: a.resolveTelemtConfigPath(ctx),
	}, payload)
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
