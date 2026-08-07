package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/agent/telemtrestart"
	"github.com/lost-coder/panvex/internal/agent/telemtupdate"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestHandleTelemtUpdateJobInvalidPayload guards that a malformed payload
// produces a failed result instead of panicking the job worker.
func TestHandleTelemtUpdateJobInvalidPayload(t *testing.T) {
	a := New(Config{AgentID: "agent-1"}, &fakeTelemtClient{})

	job := &gatewayrpc.JobCommand{
		Id:          "job-bad-payload",
		Action:      "telemt.update",
		PayloadJson: `{not-json`,
	}
	res := a.HandleJob(context.Background(), job, time.Now())
	if res.Success {
		t.Fatal("expected failure for invalid payload")
	}
	if res.Message == "" {
		t.Fatal("expected a non-empty failure message")
	}
}

// TestHandleTelemtUpdateJobSystemInfoError guards that when the agent
// cannot read the currently-running Telemt version, the update is refused
// rather than started blind (the downgrade gate would be unable to compare
// against the running version).
func TestHandleTelemtUpdateJobSystemInfoError(t *testing.T) {
	client := &fakeTelemtClient{systemInfoErr: errors.New("connection refused")}
	a := New(Config{AgentID: "agent-1"}, client)

	job := &gatewayrpc.JobCommand{
		Id:          "job-sysinfo-err",
		Action:      "telemt.update",
		PayloadJson: `{"version":"2026.03","release_base_url":"https://releases.invalid/telemt/2026.03","restart_spec":"systemd:telemt","binary_path":"/usr/local/bin/telemt"}`,
	}
	res := a.HandleJob(context.Background(), job, time.Now())
	if res.Success {
		t.Fatal("expected failure when current telemt version cannot be read")
	}
	if !strings.Contains(res.Message, "cannot read current telemt version") {
		t.Fatalf("message = %q, want it to mention cannot read current telemt version", res.Message)
	}
	if !strings.Contains(res.Message, "connection refused") {
		t.Fatalf("message = %q, want it to wrap the underlying error", res.Message)
	}
}

// TestHandleTelemtUpdateJobSuccess guards the happy path: Execute succeeds,
// the result reports success with Execute's own Outcome.Message, and the
// slow-data cache is invalidated so the next snapshot cycle picks up the
// new version sooner.
func TestHandleTelemtUpdateJobSuccess(t *testing.T) {
	client := &fakeTelemtClient{systemInfo: telemt.SystemInfo{Version: "2026.02"}}
	a := New(Config{AgentID: "agent-1"}, client)

	orig := telemtUpdateExecute
	telemtUpdateExecute = func(_ context.Context, p telemtupdate.Payload, currentVersion string, _ telemtupdate.TelemtInfo, _ telemtrestart.CommandRunner, _ *slog.Logger) (telemtupdate.Outcome, error) {
		if currentVersion != "2026.02" {
			t.Fatalf("currentVersion = %q, want 2026.02", currentVersion)
		}
		if p.Version != "2026.03" {
			t.Fatalf("payload version = %q, want 2026.03", p.Version)
		}
		return telemtupdate.Outcome{Updated: true, Message: "updated telemt 2026.02 -> 2026.03"}, nil
	}
	t.Cleanup(func() { telemtUpdateExecute = orig })

	// Prime the diagnostics delta-gate so it remembers a hash: the second
	// next() with the same fields returns sendBody=false. A successful update
	// must reset it (ResetDeltaGates) so the next snapshot re-sends the full
	// diagnostics body — carrying the new Telemt version to the panel even
	// though the restart's unreachable snapshots left the gate holding a stale
	// hash. Without the reset the "update available" badge sticks on the old
	// version after a successful upgrade.
	if _, send := a.diagnosticsGate.next("diag-body"); !send {
		t.Fatal("priming: first next() should send body")
	}
	if _, send := a.diagnosticsGate.next("diag-body"); send {
		t.Fatal("priming: second identical next() should NOT send body")
	}

	job := &gatewayrpc.JobCommand{
		Id:          "job-ok",
		Action:      "telemt.update",
		PayloadJson: `{"version":"2026.03","release_base_url":"https://releases.invalid/telemt/2026.03","restart_spec":"systemd:telemt","binary_path":"/usr/local/bin/telemt"}`,
	}
	res := a.HandleJob(context.Background(), job, time.Now())
	if !res.Success {
		t.Fatalf("expected success, got failure: %q", res.Message)
	}
	if res.Message != "updated telemt 2026.02 -> 2026.03" {
		t.Fatalf("message = %q, want Outcome.Message verbatim", res.Message)
	}
	if client.invalidateSlowDataCalls != 1 {
		t.Fatalf("expected InvalidateSlowDataCache to be called once, got %d", client.invalidateSlowDataCalls)
	}
	if _, send := a.diagnosticsGate.next("diag-body"); !send {
		t.Fatal("expected ResetDeltaGates on success so diagnostics body re-sends; gate still suppressed")
	}
}

// TestHandleTelemtUpdateJobExecuteError guards the failure path: Execute's
// error becomes a failed result carrying Outcome.Message (self-contained,
// no extra wrapping needed since Execute already folds KeepBackup etc. into
// the text).
func TestHandleTelemtUpdateJobExecuteError(t *testing.T) {
	client := &fakeTelemtClient{systemInfo: telemt.SystemInfo{Version: "2026.02"}}
	a := New(Config{AgentID: "agent-1"}, client)

	orig := telemtUpdateExecute
	telemtUpdateExecute = func(context.Context, telemtupdate.Payload, string, telemtupdate.TelemtInfo, telemtrestart.CommandRunner, *slog.Logger) (telemtupdate.Outcome, error) {
		return telemtupdate.Outcome{Message: "health-gate timed out; backup kept at /usr/local/bin/telemt.bak", KeepBackup: true}, errors.New("health-gate timed out")
	}
	t.Cleanup(func() { telemtUpdateExecute = orig })

	job := &gatewayrpc.JobCommand{
		Id:          "job-execute-err",
		Action:      "telemt.update",
		PayloadJson: `{"version":"2026.03","release_base_url":"https://releases.invalid/telemt/2026.03","restart_spec":"systemd:telemt","binary_path":"/usr/local/bin/telemt"}`,
	}
	res := a.HandleJob(context.Background(), job, time.Now())
	if res.Success {
		t.Fatal("expected failure when Execute returns an error")
	}
	if res.Message != "health-gate timed out; backup kept at /usr/local/bin/telemt.bak" {
		t.Fatalf("message = %q, want Outcome.Message verbatim", res.Message)
	}
	if client.invalidateSlowDataCalls != 0 {
		t.Fatalf("expected InvalidateSlowDataCache NOT called on failure, got %d", client.invalidateSlowDataCalls)
	}
}

// TestHandleTelemtUpdateJobExecuteErrorEmptyMessageFallsBackToErr guards
// against a blank JobResult.Message: several of telemtupdate.Execute's
// early error branches (downgrade gate, asset resolve, download, checksum,
// swap) return a zero Outcome{} alongside a non-nil err — only the later
// restart/health-gate branches populate Outcome.Message. The handler must
// fall back to err.Error() so those failures are still visible to the
// operator instead of surfacing as an empty message.
func TestHandleTelemtUpdateJobExecuteErrorEmptyMessageFallsBackToErr(t *testing.T) {
	client := &fakeTelemtClient{systemInfo: telemt.SystemInfo{Version: "2026.02"}}
	a := New(Config{AgentID: "agent-1"}, client)

	orig := telemtUpdateExecute
	telemtUpdateExecute = func(context.Context, telemtupdate.Payload, string, telemtupdate.TelemtInfo, telemtrestart.CommandRunner, *slog.Logger) (telemtupdate.Outcome, error) {
		return telemtupdate.Outcome{}, errors.New("download: connection reset")
	}
	t.Cleanup(func() { telemtUpdateExecute = orig })

	job := &gatewayrpc.JobCommand{
		Id:          "job-execute-err-blank",
		Action:      "telemt.update",
		PayloadJson: `{"version":"2026.03","release_base_url":"https://releases.invalid/telemt/2026.03","restart_spec":"systemd:telemt","binary_path":"/usr/local/bin/telemt"}`,
	}
	res := a.HandleJob(context.Background(), job, time.Now())
	if res.Success {
		t.Fatal("expected failure when Execute returns an error")
	}
	if res.Message != "download: connection reset" {
		t.Fatalf("message = %q, want fallback to err.Error()", res.Message)
	}
}
