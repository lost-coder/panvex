package jobs

import (
	"context"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func TestJobPipelineForActionRoutesRuntimeReload(t *testing.T) {
	pipeline := pipelineForAction("runtime.reload")
	if pipeline != PipelineRuntimeReload {
		t.Fatalf("pipelineForAction(runtime.reload) = %q, want %q", pipeline, PipelineRuntimeReload)
	}
}

func TestJobPipelineForActionRoutesDiagnosticsRefreshToRuntimePipeline(t *testing.T) {
	pipeline := pipelineForAction("telemetry.refresh_diagnostics")
	if pipeline != PipelineRuntimeReload {
		t.Fatalf("pipelineForAction(telemetry.refresh_diagnostics) = %q, want %q", pipeline, PipelineRuntimeReload)
	}
}

func TestJobPipelineForActionRoutesClientMutations(t *testing.T) {
	clientActions := []string{
		"client.create",
		"client.update",
		"client.rotate_secret",
		"client.delete",
	}
	for _, action := range clientActions {
		pipeline := pipelineForAction(action)
		if pipeline != PipelineClientMutation {
			t.Fatalf("pipelineForAction(%q) = %q, want %q", action, pipeline, PipelineClientMutation)
		}
	}
}

func TestJobPipelineForActionRoutesUnknownActionsToDefault(t *testing.T) {
	pipeline := pipelineForAction("users.create")
	if pipeline != PipelineDefault {
		t.Fatalf("pipelineForAction(users.create) = %q, want %q", pipeline, PipelineDefault)
	}
}

func TestShouldSendRuntimeSnapshotAfterJobOnlyForSuccessfulDiagnosticsRefresh(t *testing.T) {
	if !shouldSendRuntimeSnapshotAfterJob("telemetry.refresh_diagnostics", true) {
		t.Fatal("shouldSendRuntimeSnapshotAfterJob(refresh, true) = false, want true")
	}
	if shouldSendRuntimeSnapshotAfterJob("telemetry.refresh_diagnostics", false) {
		t.Fatal("shouldSendRuntimeSnapshotAfterJob(refresh, false) = true, want false")
	}
	if shouldSendRuntimeSnapshotAfterJob("runtime.reload", true) {
		t.Fatal("shouldSendRuntimeSnapshotAfterJob(runtime.reload, true) = true, want false")
	}
}

func TestRunJobWorkerSendsDiagnosticsSnapshotBeforeSuccessResult(t *testing.T) {
	connectionCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	telemtClient := &fakeDiagnosticsRefreshTelemtClient{
		state: telemt.RuntimeState{
			Version: "2026.03",
			Gates: telemt.RuntimeGates{
				AcceptingNewConnections: true,
				MERuntimeReady:          true,
				StartupStatus:           "ready",
				StartupStage:            "steady_state",
				StartupProgressPct:      100,
			},
			Initialization: telemt.RuntimeInitialization{
				Status:        "ready",
				CurrentStage:  "steady_state",
				ProgressPct:   100,
				TransportMode: "direct",
			},
			ConnectionTotals: telemt.RuntimeConnectionTotals{
				CurrentConnections: 4,
				ActiveUsers:        2,
			},
			Diagnostics: telemt.RuntimeDiagnostics{
				State:          "fresh",
				SystemInfoJSON: `{"version":"2026.03"}`,
			},
		},
	}
	agent := runtime.New(runtime.Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "test",
	}, telemtClient)

	tracker := NewInflightTracker()
	jobQueue := make(chan *gatewayrpc.JobCommand, 1)
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 4)

	go runWorker(connectionCtx, agent, tracker, jobQueue, criticalOutbound)

	jobQueue <- &gatewayrpc.JobCommand{
		Id:     "job-refresh",
		Action: "telemetry.refresh_diagnostics",
	}

	first := <-criticalOutbound
	second := <-criticalOutbound

	if first.GetSnapshot() == nil {
		t.Fatal("first outbound message = nil snapshot, want diagnostics snapshot first")
	}
	if second.GetJobResult() == nil {
		t.Fatal("second outbound message = nil job result, want success result after snapshot")
	}
	if !second.GetJobResult().GetSuccess() {
		t.Fatalf("job result success = false, want true: %s", second.GetJobResult().GetMessage())
	}
}

func TestRunJobWorkerMarksDiagnosticsRefreshFailedWhenSnapshotBuildFails(t *testing.T) {
	connectionCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	telemtClient := &fakeDiagnosticsRefreshTelemtClient{
		fetchErrAfterInvalidation: true,
	}
	agent := runtime.New(runtime.Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "test",
	}, telemtClient)

	tracker := NewInflightTracker()
	jobQueue := make(chan *gatewayrpc.JobCommand, 1)
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 2)

	go runWorker(connectionCtx, agent, tracker, jobQueue, criticalOutbound)

	jobQueue <- &gatewayrpc.JobCommand{
		Id:     "job-refresh-fail",
		Action: "telemetry.refresh_diagnostics",
	}

	message := <-criticalOutbound
	if message.GetJobResult() == nil {
		t.Fatal("outbound message = nil job result, want failure result")
	}
	if message.GetJobResult().GetSuccess() {
		t.Fatal("job result success = true, want false when snapshot build fails")
	}
	if !strings.Contains(message.GetJobResult().GetMessage(), "diagnostics refresh failed") {
		t.Fatalf("job result message = %q, want diagnostics refresh failure", message.GetJobResult().GetMessage())
	}
}

func TestJobInflightTrackerReserveRelease(t *testing.T) {
	tracker := NewInflightTracker()

	if !tracker.reserve("job-1") {
		t.Fatal("reserve(job-1) = false, want true")
	}
	if tracker.reserve("job-1") {
		t.Fatal("reserve(job-1) = true, want false for duplicate")
	}

	tracker.release("job-1")

	if !tracker.reserve("job-1") {
		t.Fatal("reserve(job-1) after release = false, want true")
	}
}

func TestEnqueueReceivedJobQueuesAndAcknowledges(t *testing.T) {
	connectionCtx := context.Background()
	tracker := NewInflightTracker()
	jobQueues := map[Pipeline]chan *gatewayrpc.JobCommand{
		PipelineRuntimeReload:  make(chan *gatewayrpc.JobCommand, 1),
		PipelineClientMutation: make(chan *gatewayrpc.JobCommand, 1),
		PipelineDefault:        make(chan *gatewayrpc.JobCommand, 1),
	}
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	job := &gatewayrpc.JobCommand{
		Id:     "job-1",
		Action: "runtime.reload",
	}

	queued := EnqueueReceived(connectionCtx, "agent-1", nil, tracker, jobQueues, criticalOutbound, job)
	if !queued {
		t.Fatal("EnqueueReceived() = false, want true")
	}
	if len(jobQueues[PipelineRuntimeReload]) != 1 {
		t.Fatalf("len(runtime reload queue) = %d, want %d", len(jobQueues[PipelineRuntimeReload]), 1)
	}
	if len(criticalOutbound) != 1 {
		t.Fatalf("len(criticalOutbound) = %d, want %d", len(criticalOutbound), 1)
	}

	ack := <-criticalOutbound
	if ack.GetJobAcknowledgement() == nil {
		t.Fatal("ack body = nil, want job acknowledgement")
	}
	if ack.GetJobAcknowledgement().GetJobId() != "job-1" {
		t.Fatalf("ack job id = %q, want %q", ack.GetJobAcknowledgement().GetJobId(), "job-1")
	}
}

func TestEnqueueReceivedJobSkipsDuplicateQueueEntry(t *testing.T) {
	connectionCtx := context.Background()
	tracker := NewInflightTracker()
	jobQueues := map[Pipeline]chan *gatewayrpc.JobCommand{
		PipelineRuntimeReload:  make(chan *gatewayrpc.JobCommand, 2),
		PipelineClientMutation: make(chan *gatewayrpc.JobCommand, 1),
		PipelineDefault:        make(chan *gatewayrpc.JobCommand, 1),
	}
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 2)
	job := &gatewayrpc.JobCommand{
		Id:     "job-dup",
		Action: "runtime.reload",
	}

	firstQueued := EnqueueReceived(connectionCtx, "agent-1", nil, tracker, jobQueues, criticalOutbound, job)
	secondQueued := EnqueueReceived(connectionCtx, "agent-1", nil, tracker, jobQueues, criticalOutbound, job)

	if !firstQueued {
		t.Fatal("first EnqueueReceived() = false, want true")
	}
	if !secondQueued {
		t.Fatal("second EnqueueReceived() = false, want true")
	}
	if len(jobQueues[PipelineRuntimeReload]) != 1 {
		t.Fatalf("len(runtime reload queue) = %d, want %d", len(jobQueues[PipelineRuntimeReload]), 1)
	}
	if len(criticalOutbound) != 2 {
		t.Fatalf("len(criticalOutbound) = %d, want %d", len(criticalOutbound), 2)
	}
}

func TestEnqueueReceivedJobQueuesCommandWithoutIdentifier(t *testing.T) {
	connectionCtx := context.Background()
	tracker := NewInflightTracker()
	jobQueues := map[Pipeline]chan *gatewayrpc.JobCommand{
		PipelineRuntimeReload:  make(chan *gatewayrpc.JobCommand, 1),
		PipelineClientMutation: make(chan *gatewayrpc.JobCommand, 1),
		PipelineDefault:        make(chan *gatewayrpc.JobCommand, 1),
	}
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	job := &gatewayrpc.JobCommand{
		Action: "runtime.reload",
	}

	queued := EnqueueReceived(connectionCtx, "agent-1", nil, tracker, jobQueues, criticalOutbound, job)
	if !queued {
		t.Fatal("EnqueueReceived() = false, want true")
	}
	if len(jobQueues[PipelineRuntimeReload]) != 1 {
		t.Fatalf("len(runtime reload queue) = %d, want %d", len(jobQueues[PipelineRuntimeReload]), 1)
	}
	if len(criticalOutbound) != 1 {
		t.Fatalf("len(criticalOutbound) = %d, want %d", len(criticalOutbound), 1)
	}
}
