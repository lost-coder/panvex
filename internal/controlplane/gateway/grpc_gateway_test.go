package gateway

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestServerPendingJobsForAgentIncludesQueuedTarget(t *testing.T) {
	currentTime := time.Date(2026, time.March, 20, 8, 0, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })

	queued := enqueueJobForAgent(t, svc, "agent-1", "queued-target", currentTime)
	enqueueJobForAgent(t, svc, "agent-2", "queued-other-agent", currentTime.Add(time.Second))

	pending := g.pendingJobsForAgent(context.Background(), "agent-1")
	if len(pending) != 1 {
		t.Fatalf("len(pendingJobsForAgent) = %d, want %d", len(pending), 1)
	}
	if pending[0].ID != queued.ID {
		t.Fatalf("pending[0].ID = %q, want %q", pending[0].ID, queued.ID)
	}
}

func TestServerPendingJobsForAgentSkipsRecentlySentTarget(t *testing.T) {
	currentTime := time.Date(2026, time.March, 20, 8, 30, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })

	job := enqueueJobForAgent(t, svc, "agent-1", "sent-recent", currentTime)
	deliveredAt := currentTime.Add(2 * time.Second)
	// D3: target.UpdatedAt is stamped with the panel clock, so advance the
	// clock to the delivery moment before marking delivered — the
	// agent-reported observedAt no longer drives redelivery gating.
	currentTime = deliveredAt
	svc.MarkDelivered(context.Background(), "agent-1", job.ID, deliveredAt)

	currentTime = deliveredAt.Add(jobDispatchRetryAfter - time.Second)
	pending := g.pendingJobsForAgent(context.Background(), "agent-1")
	if len(pending) != 0 {
		t.Fatalf("len(pendingJobsForAgent) = %d, want %d", len(pending), 0)
	}
}

func TestServerPendingJobsForAgentRedeliversStaleSentTarget(t *testing.T) {
	currentTime := time.Date(2026, time.March, 20, 9, 0, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })

	job := enqueueJobForAgent(t, svc, "agent-1", "sent-stale", currentTime)
	deliveredAt := currentTime.Add(2 * time.Second)
	svc.MarkDelivered(context.Background(), "agent-1", job.ID, deliveredAt)

	currentTime = deliveredAt.Add(jobDispatchRetryAfter + time.Second)
	pending := g.pendingJobsForAgent(context.Background(), "agent-1")
	if len(pending) != 1 {
		t.Fatalf("len(pendingJobsForAgent) = %d, want %d", len(pending), 1)
	}
	if pending[0].ID != job.ID {
		t.Fatalf("pending[0].ID = %q, want %q", pending[0].ID, job.ID)
	}
}

// TestServerPendingJobsForAgentRedeliversAcknowledgedAfterRetryWindow guards
// H-7 at the gateway layer: an acknowledged target is skipped within the
// retryAfter window, but re-dispatched once it elapses (so a JobResult lost
// after the ack is retried instead of hanging until a CP restart). The
// agent's idempotency cache dedups the replay.
func TestServerPendingJobsForAgentRedeliversAcknowledgedAfterRetryWindow(t *testing.T) {
	currentTime := time.Date(2026, time.March, 20, 9, 30, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })

	job := enqueueJobForAgent(t, svc, "agent-1", "acknowledged-target", currentTime)
	deliveredAt := currentTime.Add(2 * time.Second)
	acknowledgedAt := deliveredAt.Add(time.Second)
	svc.MarkDelivered(context.Background(), "agent-1", job.ID, deliveredAt)
	svc.MarkAcknowledged(context.Background(), "agent-1", job.ID, acknowledgedAt)

	// Within the retry window: not re-dispatched.
	currentTime = acknowledgedAt.Add(time.Second)
	if pending := g.pendingJobsForAgent(context.Background(), "agent-1"); len(pending) != 0 {
		t.Fatalf("within retry window len(pendingJobsForAgent) = %d, want 0", len(pending))
	}

	// After the retry window: re-dispatched (lost-after-ack recovery).
	currentTime = acknowledgedAt.Add(jobDispatchRetryAfter + time.Second)
	if pending := g.pendingJobsForAgent(context.Background(), "agent-1"); len(pending) != 1 {
		t.Fatalf("after retry window len(pendingJobsForAgent) = %d, want 1 (redelivery)", len(pending))
	}
}

func TestEnqueueInboundAgentMessageDropsStaleRegularUpdateWhenQueueIsFull(t *testing.T) {
	priorityInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	regularInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	stale := heartbeatMessageForTest("stale")
	latest := heartbeatMessageForTest("latest")
	regularInbound <- stale

	ok := enqueueInboundAgentMessage(context.Background(), priorityInbound, regularInbound, latest, nil)
	if !ok {
		t.Fatal("enqueueInboundAgentMessage() = false, want true")
	}
	if len(priorityInbound) != 0 {
		t.Fatalf("len(priorityInbound) = %d, want %d", len(priorityInbound), 0)
	}

	select {
	case received := <-regularInbound:
		if received != latest {
			t.Fatal("regularInbound received stale message, want latest")
		}
	default:
		t.Fatal("regularInbound = empty, want latest message")
	}
}

func TestEnqueueInboundAgentMessagePrioritizesJobAcknowledgement(t *testing.T) {
	priorityInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	regularInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	stale := heartbeatMessageForTest("stale")
	ack := jobAcknowledgementMessageForTest("job-1")
	regularInbound <- stale

	ok := enqueueInboundAgentMessage(context.Background(), priorityInbound, regularInbound, ack, nil)
	if !ok {
		t.Fatal("enqueueInboundAgentMessage() = false, want true")
	}
	if len(priorityInbound) != 1 {
		t.Fatalf("len(priorityInbound) = %d, want %d", len(priorityInbound), 1)
	}
	if len(regularInbound) != 1 {
		t.Fatalf("len(regularInbound) = %d, want %d", len(regularInbound), 1)
	}

	select {
	case received := <-priorityInbound:
		if received != ack {
			t.Fatal("priorityInbound received unexpected message")
		}
	default:
		t.Fatal("priorityInbound = empty, want acknowledgement message")
	}
	select {
	case received := <-regularInbound:
		if received != stale {
			t.Fatal("regularInbound message changed, want stale heartbeat to remain")
		}
	default:
		t.Fatal("regularInbound = empty, want stale heartbeat")
	}
}

func TestEnqueueInboundAgentMessageStopsWhenContextCancelled(t *testing.T) {
	connectionCtx, cancel := context.WithCancel(context.Background())
	cancel()

	priorityInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	regularInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)

	ok := enqueueInboundAgentMessage(connectionCtx, priorityInbound, regularInbound, heartbeatMessageForTest("latest"), nil)
	if ok {
		t.Fatal("enqueueInboundAgentMessage() = true, want false")
	}
	if len(priorityInbound) != 0 {
		t.Fatalf("len(priorityInbound) = %d, want %d", len(priorityInbound), 0)
	}
	if len(regularInbound) != 0 {
		t.Fatalf("len(regularInbound) = %d, want %d", len(regularInbound), 0)
	}
}

// TestEnqueueInboundAgentMessageIncrementsDropCounter covers the silent-drop
// branch (D-2): all three non-blocking steps fail and the function used to
// return true without recording the loss. An unbuffered regular-inbound chan
// with no concurrent reader/writer makes every select hit `default`, so the
// drop path is reached deterministically and the counter must increment.
func TestEnqueueInboundAgentMessageIncrementsDropCounter(t *testing.T) {
	priorityInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	regularInbound := make(chan *gatewayrpc.ConnectClientMessage) // unbuffered → all selects miss
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "panvex_agent_inbound_drops_total_test",
		Help: "test-local counter",
	})

	ok := enqueueInboundAgentMessage(
		context.Background(),
		priorityInbound,
		regularInbound,
		heartbeatMessageForTest("dropped"),
		counter,
	)
	if !ok {
		t.Fatal("enqueueInboundAgentMessage() = false, want true (drop path still reports routed=true)")
	}
	if got := testutil.ToFloat64(counter); got != 1 {
		t.Fatalf("dropCounter = %v, want 1", got)
	}
	if len(priorityInbound) != 0 {
		t.Fatalf("len(priorityInbound) = %d, want 0", len(priorityInbound))
	}
}

// TestEnqueueInboundAgentMessageDoesNotIncrementOnSuccess guards against a
// spurious Inc() in the happy path: when the first non-blocking send accepts
// the message, the counter must remain at zero.
func TestEnqueueInboundAgentMessageDoesNotIncrementOnSuccess(t *testing.T) {
	priorityInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	regularInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "panvex_agent_inbound_drops_total_test_success",
		Help: "test-local counter",
	})

	ok := enqueueInboundAgentMessage(
		context.Background(),
		priorityInbound,
		regularInbound,
		heartbeatMessageForTest("ok"),
		counter,
	)
	if !ok {
		t.Fatal("enqueueInboundAgentMessage() = false, want true")
	}
	if got := testutil.ToFloat64(counter); got != 0 {
		t.Fatalf("dropCounter = %v, want 0 (no drop on happy path)", got)
	}
}

func TestEnqueueRegularSnapshotDropsStaleUpdateWhenQueueIsFull(t *testing.T) {
	regularSnapshots := make(chan AgentSnapshot, 1)
	stale := AgentSnapshot{
		AgentID:    "agent-1",
		Snap:       &gatewayrpc.Snapshot{NodeName: "stale"},
		ObservedAt: time.Unix(1, 0).UTC(),
	}
	latest := AgentSnapshot{
		AgentID:    "agent-1",
		Snap:       &gatewayrpc.Snapshot{NodeName: "latest"},
		ObservedAt: time.Unix(2, 0).UTC(),
	}
	regularSnapshots <- stale

	ok := enqueueRegularSnapshot(context.Background(), regularSnapshots, latest)
	if !ok {
		t.Fatal("enqueueRegularSnapshot() = false, want true")
	}

	select {
	case received := <-regularSnapshots:
		if received.Snap.NodeName != "latest" {
			t.Fatalf("received.Snap.NodeName = %q, want %q", received.Snap.NodeName, "latest")
		}
	default:
		t.Fatal("regularSnapshots = empty, want latest snapshot")
	}
}

func TestProcessRegularAgentMessageRoutesAckToPriorityHandler(t *testing.T) {
	currentTime := time.Date(2026, time.March, 21, 7, 0, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })
	job := enqueueJobForAgent(t, svc, "agent-1", "regular-routes-ack", currentTime)
	svc.MarkDelivered(context.Background(), "agent-1", job.ID, currentTime.Add(time.Second))

	regularSnapshots := make(chan AgentSnapshot, 1)
	message := &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobAcknowledgement{
			JobAcknowledgement: &gatewayrpc.JobAcknowledgement{
				JobId:          job.ID,
				ObservedAtUnix: currentTime.Add(2 * time.Second).Unix(),
			},
		},
	}
	if err := g.processRegularAgentMessage(context.Background(), "agent-1", nil, regularSnapshots, message); err != nil {
		t.Fatalf("processRegularAgentMessage() error = %v", err)
	}

	if len(regularSnapshots) != 0 {
		t.Fatalf("len(regularSnapshots) = %d, want %d", len(regularSnapshots), 0)
	}
	listedJobs := svc.List()
	if len(listedJobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 1)
	}
	if listedJobs[0].Targets[0].Status != jobs.TargetStatusAcknowledged {
		t.Fatalf("targets[0].Status = %q, want %q", listedJobs[0].Targets[0].Status, jobs.TargetStatusAcknowledged)
	}
}

func TestProcessPriorityAgentMessageRecordsAcknowledgement(t *testing.T) {
	currentTime := time.Date(2026, time.March, 21, 8, 0, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })

	job := enqueueJobForAgent(t, svc, "agent-1", "priority-ack", currentTime)
	svc.MarkDelivered(context.Background(), "agent-1", job.ID, currentTime.Add(time.Second))

	message := &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobAcknowledgement{
			JobAcknowledgement: &gatewayrpc.JobAcknowledgement{
				JobId:          job.ID,
				ObservedAtUnix: currentTime.Add(2 * time.Second).Unix(),
			},
		},
	}
	if err := g.processPriorityAgentMessage(context.Background(), "agent-1", message); err != nil {
		t.Fatalf("processPriorityAgentMessage() error = %v", err)
	}

	listedJobs := svc.List()
	if len(listedJobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 1)
	}
	if listedJobs[0].Targets[0].Status != jobs.TargetStatusAcknowledged {
		t.Fatalf("targets[0].Status = %q, want %q", listedJobs[0].Targets[0].Status, jobs.TargetStatusAcknowledged)
	}
}

func TestProcessPriorityAgentMessageRecordsResult(t *testing.T) {
	currentTime := time.Date(2026, time.March, 21, 8, 30, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })

	job := enqueueJobForAgent(t, svc, "agent-1", "priority-result", currentTime)
	svc.MarkDelivered(context.Background(), "agent-1", job.ID, currentTime.Add(time.Second))

	message := &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobResult{
			JobResult: &gatewayrpc.JobResult{
				JobId:          job.ID,
				Success:        false,
				Message:        "apply failed",
				ObservedAtUnix: currentTime.Add(2 * time.Second).Unix(),
			},
		},
	}
	if err := g.processPriorityAgentMessage(context.Background(), "agent-1", message); err != nil {
		t.Fatalf("processPriorityAgentMessage() error = %v", err)
	}

	listedJobs := svc.List()
	if len(listedJobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 1)
	}
	if listedJobs[0].Targets[0].Status != jobs.TargetStatusFailed {
		t.Fatalf("targets[0].Status = %q, want %q", listedJobs[0].Targets[0].Status, jobs.TargetStatusFailed)
	}
	if listedJobs[0].Targets[0].ResultText != "apply failed" {
		t.Fatalf("targets[0].ResultText = %q, want %q", listedJobs[0].Targets[0].ResultText, "apply failed")
	}
}

func TestProcessPriorityAgentMessageAsyncQueuesClientResultEffect(t *testing.T) {
	currentTime := time.Date(2026, time.March, 21, 8, 45, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })

	job := enqueueJobForAgent(t, svc, "agent-1", "priority-result-async", currentTime)
	svc.MarkDelivered(context.Background(), "agent-1", job.ID, currentTime.Add(time.Second))

	effects := make(chan jobResultEffect, 1)
	message := &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobResult{
			JobResult: &gatewayrpc.JobResult{
				JobId:          job.ID,
				Success:        false,
				Message:        "async apply failed",
				ResultJson:     `{"error":"boom"}`,
				ObservedAtUnix: currentTime.Add(2 * time.Second).Unix(),
			},
		},
	}
	if err := g.processPriorityAgentMessageAsync(context.Background(), effects, nil, "agent-1", message); err != nil {
		t.Fatalf("processPriorityAgentMessageAsync() error = %v", err)
	}

	if len(effects) != 1 {
		t.Fatalf("len(effects) = %d, want %d", len(effects), 1)
	}
	effect := <-effects
	if effect.jobID != job.ID {
		t.Fatalf("effect.jobID = %q, want %q", effect.jobID, job.ID)
	}
	if effect.resultJSON != `{"error":"boom"}` {
		t.Fatalf("effect.resultJSON = %q, want %q", effect.resultJSON, `{"error":"boom"}`)
	}

	listedJobs := svc.List()
	if len(listedJobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 1)
	}
	if listedJobs[0].Targets[0].Status != jobs.TargetStatusFailed {
		t.Fatalf("targets[0].Status = %q, want %q", listedJobs[0].Targets[0].Status, jobs.TargetStatusFailed)
	}
}

func TestEnqueuePriorityResultEffectStopsWhenContextCancelled(t *testing.T) {
	connectionCtx, cancel := context.WithCancel(context.Background())
	cancel()

	effects := make(chan jobResultEffect, 1)
	ok := enqueuePriorityResultEffect(connectionCtx, effects, jobResultEffect{
		agentID:    "agent-1",
		jobID:      "job-1",
		success:    true,
		message:    "ok",
		resultJSON: "{}",
		observedAt: time.Unix(1, 0).UTC(),
	})
	if ok {
		t.Fatal("enqueuePriorityResultEffect() = true, want false")
	}
	if len(effects) != 0 {
		t.Fatalf("len(effects) = %d, want %d", len(effects), 0)
	}
}

func TestEnqueuePriorityAuditEffectStopsWhenContextCancelled(t *testing.T) {
	connectionCtx, cancel := context.WithCancel(context.Background())
	cancel()

	effects := make(chan auditEffect, 1)
	ok := enqueuePriorityAuditEffect(connectionCtx, effects, auditEffect{
		actorID:  "agent-1",
		action:   "jobs.result",
		targetID: "job-1",
		details: map[string]any{
			"success": true,
		},
	})
	if ok {
		t.Fatal("enqueuePriorityAuditEffect() = true, want false")
	}
	if len(effects) != 0 {
		t.Fatalf("len(effects) = %d, want %d", len(effects), 0)
	}
}

func TestDrainPriorityResultEffectsProcessesQueuedEffects(t *testing.T) {
	effects := make(chan jobResultEffect, 2)
	effects <- jobResultEffect{
		agentID:    "agent-1",
		jobID:      "job-1",
		success:    true,
		message:    "ok",
		resultJSON: "{}",
		observedAt: time.Unix(1, 0).UTC(),
	}

	calls := 0
	drainPriorityResultEffects(effects, func(agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time) {
		calls++
		if agentID != "agent-1" {
			t.Fatalf("agentID = %q, want %q", agentID, "agent-1")
		}
		if jobID != "job-1" {
			t.Fatalf("jobID = %q, want %q", jobID, "job-1")
		}
	})

	if calls != 1 {
		t.Fatalf("calls = %d, want %d", calls, 1)
	}
	if len(effects) != 0 {
		t.Fatalf("len(effects) = %d, want %d", len(effects), 0)
	}
}

func TestDrainPriorityAuditEffectsProcessesQueuedEffects(t *testing.T) {
	effects := make(chan auditEffect, 2)
	effects <- auditEffect{
		actorID:  "agent-1",
		action:   "jobs.result",
		targetID: "job-1",
		details: map[string]any{
			"success": true,
		},
	}

	calls := 0
	drainPriorityAuditEffects(effects, func(actorID string, action string, targetID string, details map[string]any) {
		calls++
		if actorID != "agent-1" {
			t.Fatalf("actorID = %q, want %q", actorID, "agent-1")
		}
		if action != "jobs.result" {
			t.Fatalf("action = %q, want %q", action, "jobs.result")
		}
		if targetID != "job-1" {
			t.Fatalf("targetID = %q, want %q", targetID, "job-1")
		}
	})

	if calls != 1 {
		t.Fatalf("calls = %d, want %d", calls, 1)
	}
	if len(effects) != 0 {
		t.Fatalf("len(effects) = %d, want %d", len(effects), 0)
	}
}

func TestDrainRegularSnapshotsProcessesQueuedSnapshots(t *testing.T) {
	snapshots := make(chan AgentSnapshot, 2)
	snapshots <- AgentSnapshot{
		AgentID:    "agent-1",
		Snap:       &gatewayrpc.Snapshot{NodeName: "node-a"},
		ObservedAt: time.Unix(1, 0).UTC(),
	}

	calls := 0
	drainRegularSnapshots(snapshots, func(snapshot AgentSnapshot) error {
		calls++
		if snapshot.AgentID != "agent-1" {
			t.Fatalf("snapshot.AgentID = %q, want %q", snapshot.AgentID, "agent-1")
		}
		return nil
	})

	if calls != 1 {
		t.Fatalf("calls = %d, want %d", calls, 1)
	}
	if len(snapshots) != 0 {
		t.Fatalf("len(snapshots) = %d, want %d", len(snapshots), 0)
	}
}

func TestIsPriorityAgentMessageClassifiesJobSignals(t *testing.T) {
	if !isPriorityAgentMessage(jobAcknowledgementMessageForTest("job-1")) {
		t.Fatal("isPriorityAgentMessage(ack) = false, want true")
	}
	if !isPriorityAgentMessage(&gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobResult{
			JobResult: &gatewayrpc.JobResult{
				JobId: "job-1",
			},
		},
	}) {
		t.Fatal("isPriorityAgentMessage(result) = false, want true")
	}
	if isPriorityAgentMessage(heartbeatMessageForTest("node-a")) {
		t.Fatal("isPriorityAgentMessage(heartbeat) = true, want false")
	}
}

func TestDispatchReconnectRedeliveryAvoidsDuplicateRuntimeMutation(t *testing.T) {
	currentTime := time.Date(2026, time.March, 22, 9, 0, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })

	job := enqueueJobForAgent(t, svc, "agent-1", "dispatch-reconnect", currentTime)
	firstStream := newFakeGatewayConnectStream(context.Background())
	if err := g.dispatchPendingJobs(context.Background(), firstStream, "agent-1"); err != nil {
		t.Fatalf("dispatchPendingJobs(first) error = %v", err)
	}
	if len(firstStream.sent) != 1 {
		t.Fatalf("len(firstStream.sent) = %d, want %d", len(firstStream.sent), 1)
	}
	firstCommand := firstStream.sent[0].GetJob()
	if firstCommand == nil {
		t.Fatal("first dispatch command = nil, want job command")
	}
	if firstCommand.GetId() != job.ID {
		t.Fatalf("first command id = %q, want %q", firstCommand.GetId(), job.ID)
	}

	telemtClient := &fakeRuntimeReloadClient{}
	agent := runtime.New(runtime.Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "test",
	}, telemtClient)

	firstResult := agent.HandleJob(context.Background(), firstCommand, currentTime.Add(2*time.Second))
	if !firstResult.GetSuccess() {
		t.Fatalf("first HandleJob() success = false, want true: %s", firstResult.GetMessage())
	}
	if telemtClient.reloadCalls != 1 {
		t.Fatalf("reload call count after first execution = %d, want %d", telemtClient.reloadCalls, 1)
	}

	// Simulate stream disconnect before result delivery, then trigger redelivery after lease timeout.
	currentTime = currentTime.Add(jobDispatchRetryAfter + time.Second)
	secondStream := newFakeGatewayConnectStream(context.Background())
	if err := g.dispatchPendingJobs(context.Background(), secondStream, "agent-1"); err != nil {
		t.Fatalf("dispatchPendingJobs(second) error = %v", err)
	}
	if len(secondStream.sent) != 1 {
		t.Fatalf("len(secondStream.sent) = %d, want %d", len(secondStream.sent), 1)
	}
	secondCommand := secondStream.sent[0].GetJob()
	if secondCommand == nil {
		t.Fatal("second dispatch command = nil, want job command")
	}
	if secondCommand.GetId() != job.ID {
		t.Fatalf("second command id = %q, want %q", secondCommand.GetId(), job.ID)
	}

	secondResult := agent.HandleJob(context.Background(), secondCommand, currentTime.Add(time.Second))
	if !secondResult.GetSuccess() {
		t.Fatalf("second HandleJob() success = false, want true: %s", secondResult.GetMessage())
	}
	if telemtClient.reloadCalls != 1 {
		t.Fatalf("reload call count after redelivery = %d, want %d", telemtClient.reloadCalls, 1)
	}

	if err := g.processPriorityAgentMessage(context.Background(), "agent-1", &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobResult{
			JobResult: secondResult,
		},
	}); err != nil {
		t.Fatalf("processPriorityAgentMessage(job_result) error = %v", err)
	}

	listedJobs := svc.List()
	if len(listedJobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 1)
	}
	if listedJobs[0].Status != jobs.StatusSucceeded {
		t.Fatalf("job status = %q, want %q", listedJobs[0].Status, jobs.StatusSucceeded)
	}
	if listedJobs[0].Targets[0].Status != jobs.TargetStatusSucceeded {
		t.Fatalf("target status = %q, want %q", listedJobs[0].Targets[0].Status, jobs.TargetStatusSucceeded)
	}
}

func TestDispatchPendingJobsSendsBoundedBatchAndLeavesRemainderQueued(t *testing.T) {
	currentTime := time.Date(2026, time.March, 22, 10, 0, 0, 0, time.UTC)
	g, svc := newTestGateway(func() time.Time { return currentTime })

	jobCount := jobDispatchBatchSize + 3
	for index := 0; index < jobCount; index++ {
		key := fmt.Sprintf("dispatch-batch-%03d", index)
		enqueueJobForAgent(t, svc, "agent-1", key, currentTime.Add(time.Duration(index)*time.Second))
	}

	stream := newFakeGatewayConnectStream(context.Background())
	if err := g.dispatchPendingJobs(context.Background(), stream, "agent-1"); err != nil {
		t.Fatalf("dispatchPendingJobs() error = %v", err)
	}

	if len(stream.sent) != jobDispatchBatchSize {
		t.Fatalf("len(stream.sent) = %d, want %d", len(stream.sent), jobDispatchBatchSize)
	}

	pending := g.pendingJobsForAgent(context.Background(), "agent-1")
	expectedPending := jobCount - jobDispatchBatchSize
	if len(pending) != expectedPending {
		t.Fatalf("len(pendingJobsForAgent) = %d, want %d", len(pending), expectedPending)
	}
}
