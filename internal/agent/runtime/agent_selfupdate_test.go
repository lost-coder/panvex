package runtime

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/updater"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestHandleSelfUpdateJobNoopDoesNotScheduleRestart guards A3: a self-update
// to the already-running version must converge to a successful result and
// must NOT schedule a restart. Uses the real updater gate (no network is
// touched on the equality path).
func TestHandleSelfUpdateJobNoopDoesNotScheduleRestart(t *testing.T) {
	restarts := 0
	a := New(Config{
		AgentID:             "agent-1",
		Version:             "1.4.7",
		ScheduleSelfRestart: func() { restarts++ },
	}, &fakeTelemtClient{})

	job := &gatewayrpc.JobCommand{
		Id:          "job-noop",
		Action:      "agent.self-update",
		PayloadJson: `{"version":"1.4.7","release_base_url":"https://releases.invalid/agent/v1.4.7"}`,
	}
	res := a.HandleJob(context.Background(), job, time.Now())
	if !res.Success {
		t.Fatalf("expected no-op success, got failure: %q", res.Message)
	}
	if !strings.Contains(res.Message, "already at target version") {
		t.Fatalf("message = %q, want it to mention already-at-target-version", res.Message)
	}
	if restarts != 0 {
		t.Fatalf("no-op must not schedule a restart, got %d", restarts)
	}
}

// TestHandleSelfUpdateJobSchedulesRestartAfterResult guards A3: on a real
// update the handler must RETURN a successful result (so the worker can
// flush the JobResult to the panel) and delegate the restart to the
// ScheduleSelfRestart hook instead of exiting in-handler.
func TestHandleSelfUpdateJobSchedulesRestartAfterResult(t *testing.T) {
	restarts := 0
	a := New(Config{
		AgentID:             "agent-1",
		Version:             "1.4.7",
		ScheduleSelfRestart: func() { restarts++ },
	}, &fakeTelemtClient{})

	orig := selfUpdateExecute
	selfUpdateExecute = func(context.Context, updater.Payload, string, *slog.Logger) (updater.Outcome, error) {
		return updater.OutcomeUpdated, nil
	}
	t.Cleanup(func() { selfUpdateExecute = orig })

	job := &gatewayrpc.JobCommand{
		Id:          "job-upd",
		Action:      "agent.self-update",
		PayloadJson: `{"version":"1.5.0","release_base_url":"https://releases.invalid/agent/v1.5.0"}`,
	}
	res := a.HandleJob(context.Background(), job, time.Now())
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Message)
	}
	if restarts != 1 {
		t.Fatalf("expected exactly one scheduled restart, got %d", restarts)
	}
}

// TestHandleSelfUpdateJobFailureDoesNotScheduleRestart: a failed update must
// surface the error and leave the process alone.
func TestHandleSelfUpdateJobFailureDoesNotScheduleRestart(t *testing.T) {
	restarts := 0
	a := New(Config{Version: "1.4.7", ScheduleSelfRestart: func() { restarts++ }}, &fakeTelemtClient{})

	orig := selfUpdateExecute
	selfUpdateExecute = func(context.Context, updater.Payload, string, *slog.Logger) (updater.Outcome, error) {
		return updater.OutcomeNoop, errors.New("download: boom")
	}
	t.Cleanup(func() { selfUpdateExecute = orig })

	job := &gatewayrpc.JobCommand{
		Id:          "job-fail",
		Action:      "agent.self-update",
		PayloadJson: `{"version":"1.5.0","release_base_url":"https://releases.invalid/agent/v1.5.0"}`,
	}
	res := a.HandleJob(context.Background(), job, time.Now())
	if res.Success {
		t.Fatal("expected failure")
	}
	if restarts != 0 {
		t.Fatalf("failed update must not schedule a restart, got %d", restarts)
	}
}

// TestHandleSelfUpdateJobWithoutRestartHookStillSucceeds: nil hook (tests,
// exotic deployments) must not panic; the result still reports success and
// the new binary takes effect on the next external restart.
func TestHandleSelfUpdateJobWithoutRestartHookStillSucceeds(t *testing.T) {
	a := New(Config{Version: "1.4.7"}, &fakeTelemtClient{})

	orig := selfUpdateExecute
	selfUpdateExecute = func(context.Context, updater.Payload, string, *slog.Logger) (updater.Outcome, error) {
		return updater.OutcomeUpdated, nil
	}
	t.Cleanup(func() { selfUpdateExecute = orig })

	job := &gatewayrpc.JobCommand{
		Id:          "job-nohook",
		Action:      "agent.self-update",
		PayloadJson: `{"version":"1.5.0","release_base_url":"https://releases.invalid/agent/v1.5.0"}`,
	}
	res := a.HandleJob(context.Background(), job, time.Now())
	if !res.Success {
		t.Fatalf("expected success with nil hook, got %q", res.Message)
	}
}
