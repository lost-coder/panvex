package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

func TestServerPendingJobsForAgentIncludesQueuedTarget(t *testing.T) {
	currentTime := time.Date(2026, time.March, 20, 8, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})

	queued := enqueueJobForAgent(t, server, "agent-1", "queued-target", currentTime)
	enqueueJobForAgent(t, server, "agent-2", "queued-other-agent", currentTime.Add(time.Second))

	pending := server.pendingJobsForAgent(context.Background(), "agent-1")
	if len(pending) != 1 {
		t.Fatalf("len(pendingJobsForAgent) = %d, want %d", len(pending), 1)
	}
	if pending[0].ID != queued.ID {
		t.Fatalf("pending[0].ID = %q, want %q", pending[0].ID, queued.ID)
	}
}

func TestServerPendingJobsForAgentSkipsRecentlySentTarget(t *testing.T) {
	currentTime := time.Date(2026, time.March, 20, 8, 30, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})

	job := enqueueJobForAgent(t, server, "agent-1", "sent-recent", currentTime)
	deliveredAt := currentTime.Add(2 * time.Second)
	// D3: target.UpdatedAt is stamped with the panel clock, so advance the
	// clock to the delivery moment before marking delivered — the
	// agent-reported observedAt no longer drives redelivery gating.
	currentTime = deliveredAt
	server.jobs.MarkDelivered(context.Background(), "agent-1", job.ID, deliveredAt)

	currentTime = deliveredAt.Add(jobDispatchRetryAfter - time.Second)
	pending := server.pendingJobsForAgent(context.Background(), "agent-1")
	if len(pending) != 0 {
		t.Fatalf("len(pendingJobsForAgent) = %d, want %d", len(pending), 0)
	}
}

func TestServerPendingJobsForAgentRedeliversStaleSentTarget(t *testing.T) {
	currentTime := time.Date(2026, time.March, 20, 9, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})

	job := enqueueJobForAgent(t, server, "agent-1", "sent-stale", currentTime)
	deliveredAt := currentTime.Add(2 * time.Second)
	server.jobs.MarkDelivered(context.Background(), "agent-1", job.ID, deliveredAt)

	currentTime = deliveredAt.Add(jobDispatchRetryAfter + time.Second)
	pending := server.pendingJobsForAgent(context.Background(), "agent-1")
	if len(pending) != 1 {
		t.Fatalf("len(pendingJobsForAgent) = %d, want %d", len(pending), 1)
	}
	if pending[0].ID != job.ID {
		t.Fatalf("pending[0].ID = %q, want %q", pending[0].ID, job.ID)
	}
}

// TestServerPendingJobsForAgentRedeliversAcknowledgedAfterRetryWindow guards
// H-7 at the server layer: an acknowledged target is skipped within the
// retryAfter window, but re-dispatched once it elapses (so a JobResult lost
// after the ack is retried instead of hanging until a CP restart). The
// agent's idempotency cache dedups the replay.
func TestServerPendingJobsForAgentRedeliversAcknowledgedAfterRetryWindow(t *testing.T) {
	currentTime := time.Date(2026, time.March, 20, 9, 30, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return currentTime },
	})

	job := enqueueJobForAgent(t, server, "agent-1", "acknowledged-target", currentTime)
	deliveredAt := currentTime.Add(2 * time.Second)
	acknowledgedAt := deliveredAt.Add(time.Second)
	server.jobs.MarkDelivered(context.Background(), "agent-1", job.ID, deliveredAt)
	server.jobs.MarkAcknowledged(context.Background(), "agent-1", job.ID, acknowledgedAt)

	// Within the retry window: not re-dispatched.
	currentTime = acknowledgedAt.Add(time.Second)
	if pending := server.pendingJobsForAgent(context.Background(), "agent-1"); len(pending) != 0 {
		t.Fatalf("within retry window len(pendingJobsForAgent) = %d, want 0", len(pending))
	}

	// After the retry window: re-dispatched (lost-after-ack recovery).
	currentTime = acknowledgedAt.Add(jobDispatchRetryAfter + time.Second)
	if pending := server.pendingJobsForAgent(context.Background(), "agent-1"); len(pending) != 1 {
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

// TestAgentInboundDropsTotalRegistered makes sure the production counter is
// wired through newMetricsCollectors and surfaces on /metrics scrapes.
func TestAgentInboundDropsTotalRegistered(t *testing.T) {
	mc := newMetricsCollectors()
	if mc.agentInboundDropsTotal == nil {
		t.Fatal("agentInboundDropsTotal is nil after newMetricsCollectors()")
	}
	mc.agentInboundDropsTotal.Inc()
	if got := testutil.ToFloat64(mc.agentInboundDropsTotal); got != 1 {
		t.Fatalf("agentInboundDropsTotal = %v, want 1", got)
	}
}

func TestEnqueueRegularSnapshotDropsStaleUpdateWhenQueueIsFull(t *testing.T) {
	regularSnapshots := make(chan agentSnapshot, 1)
	stale := agentSnapshot{
		AgentID:    "agent-1",
		NodeName:   "stale",
		ObservedAt: time.Unix(1, 0).UTC(),
	}
	latest := agentSnapshot{
		AgentID:    "agent-1",
		NodeName:   "latest",
		ObservedAt: time.Unix(2, 0).UTC(),
	}
	regularSnapshots <- stale

	ok := enqueueRegularSnapshot(context.Background(), regularSnapshots, latest)
	if !ok {
		t.Fatal("enqueueRegularSnapshot() = false, want true")
	}

	select {
	case received := <-regularSnapshots:
		if received.NodeName != "latest" {
			t.Fatalf("received.NodeName = %q, want %q", received.NodeName, "latest")
		}
	default:
		t.Fatal("regularSnapshots = empty, want latest snapshot")
	}
}

func TestProcessRegularAgentMessageRoutesAckToPriorityHandler(t *testing.T) {
	currentTime := time.Date(2026, time.March, 21, 7, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})
	job := enqueueJobForAgent(t, server, "agent-1", "regular-routes-ack", currentTime)
	server.jobs.MarkDelivered(context.Background(), "agent-1", job.ID, currentTime.Add(time.Second))

	regularSnapshots := make(chan agentSnapshot, 1)
	message := &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobAcknowledgement{
			JobAcknowledgement: &gatewayrpc.JobAcknowledgement{
				JobId:          job.ID,
				ObservedAtUnix: currentTime.Add(2 * time.Second).Unix(),
			},
		},
	}
	if err := server.processRegularAgentMessage(context.Background(), "agent-1", nil, regularSnapshots, message); err != nil {
		t.Fatalf("processRegularAgentMessage() error = %v", err)
	}

	if len(regularSnapshots) != 0 {
		t.Fatalf("len(regularSnapshots) = %d, want %d", len(regularSnapshots), 0)
	}
	listedJobs := server.jobs.List()
	if len(listedJobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 1)
	}
	if listedJobs[0].Targets[0].Status != jobs.TargetStatusAcknowledged {
		t.Fatalf("targets[0].Status = %q, want %q", listedJobs[0].Targets[0].Status, jobs.TargetStatusAcknowledged)
	}
}

func TestProcessPriorityAgentMessageRecordsAcknowledgement(t *testing.T) {
	currentTime := time.Date(2026, time.March, 21, 8, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})

	job := enqueueJobForAgent(t, server, "agent-1", "priority-ack", currentTime)
	server.jobs.MarkDelivered(context.Background(), "agent-1", job.ID, currentTime.Add(time.Second))

	message := &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobAcknowledgement{
			JobAcknowledgement: &gatewayrpc.JobAcknowledgement{
				JobId:          job.ID,
				ObservedAtUnix: currentTime.Add(2 * time.Second).Unix(),
			},
		},
	}
	if err := server.processPriorityAgentMessage(context.Background(), "agent-1", message); err != nil {
		t.Fatalf("processPriorityAgentMessage() error = %v", err)
	}

	listedJobs := server.jobs.List()
	if len(listedJobs) != 1 {
		t.Fatalf("len(List()) = %d, want %d", len(listedJobs), 1)
	}
	if listedJobs[0].Targets[0].Status != jobs.TargetStatusAcknowledged {
		t.Fatalf("targets[0].Status = %q, want %q", listedJobs[0].Targets[0].Status, jobs.TargetStatusAcknowledged)
	}
}

func TestProcessPriorityAgentMessageRecordsResult(t *testing.T) {
	currentTime := time.Date(2026, time.March, 21, 8, 30, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})

	job := enqueueJobForAgent(t, server, "agent-1", "priority-result", currentTime)
	server.jobs.MarkDelivered(context.Background(), "agent-1", job.ID, currentTime.Add(time.Second))

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
	if err := server.processPriorityAgentMessage(context.Background(), "agent-1", message); err != nil {
		t.Fatalf("processPriorityAgentMessage() error = %v", err)
	}

	listedJobs := server.jobs.List()
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
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})

	job := enqueueJobForAgent(t, server, "agent-1", "priority-result-async", currentTime)
	server.jobs.MarkDelivered(context.Background(), "agent-1", job.ID, currentTime.Add(time.Second))

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
	if err := server.processPriorityAgentMessageAsync(context.Background(), effects, nil, "agent-1", message); err != nil {
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

	listedJobs := server.jobs.List()
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
	snapshots := make(chan agentSnapshot, 2)
	snapshots <- agentSnapshot{
		AgentID:    "agent-1",
		NodeName:   "node-a",
		ObservedAt: time.Unix(1, 0).UTC(),
	}

	calls := 0
	drainRegularSnapshots(snapshots, func(snapshot agentSnapshot) error {
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
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})

	job := enqueueJobForAgent(t, server, "agent-1", "dispatch-reconnect", currentTime)
	firstStream := newFakeGatewayConnectStream(context.Background())
	if err := server.dispatchPendingJobs(context.Background(), firstStream, "agent-1"); err != nil {
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
	if err := server.dispatchPendingJobs(context.Background(), secondStream, "agent-1"); err != nil {
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

	if err := server.processPriorityAgentMessage(context.Background(), "agent-1", &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobResult{
			JobResult: secondResult,
		},
	}); err != nil {
		t.Fatalf("processPriorityAgentMessage(job_result) error = %v", err)
	}

	listedJobs := server.jobs.List()
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
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})

	jobCount := jobDispatchBatchSize + 3
	for index := 0; index < jobCount; index++ {
		key := fmt.Sprintf("dispatch-batch-%03d", index)
		enqueueJobForAgent(t, server, "agent-1", key, currentTime.Add(time.Duration(index)*time.Second))
	}

	stream := newFakeGatewayConnectStream(context.Background())
	if err := server.dispatchPendingJobs(context.Background(), stream, "agent-1"); err != nil {
		t.Fatalf("dispatchPendingJobs() error = %v", err)
	}

	if len(stream.sent) != jobDispatchBatchSize {
		t.Fatalf("len(stream.sent) = %d, want %d", len(stream.sent), jobDispatchBatchSize)
	}

	pending := server.pendingJobsForAgent(context.Background(), "agent-1")
	expectedPending := jobCount - jobDispatchBatchSize
	if len(pending) != expectedPending {
		t.Fatalf("len(pendingJobsForAgent) = %d, want %d", len(pending), expectedPending)
	}
}

func TestServerConnectRateLimitRejectsBurstReconnects(t *testing.T) {
	currentTime := time.Date(2026, time.March, 23, 8, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now: func() time.Time { return currentTime },
	})
	server.grpcConnectRateLimiter = newFixedWindowRateLimiter(1, time.Minute)

	firstStream := newFakeGatewayConnectStream(authenticatedAgentContextForTest("agent-1"))
	if err := server.Connect(firstStream); err != nil {
		t.Fatalf("first Connect() error = %v", err)
	}

	secondStream := newFakeGatewayConnectStream(authenticatedAgentContextForTest("agent-1"))
	err := server.Connect(secondStream)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("second Connect() code = %v, want %v", status.Code(err), codes.ResourceExhausted)
	}
}

func usageSnapshotMessageForTest() *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Snapshot{
			Snapshot: &gatewayrpc.Snapshot{
				AgentId:        "agent-1",
				ObservedAtUnix: 1,
				HasClientUsage: true,
				Clients: []*gatewayrpc.ClientUsageSnapshot{
					{ClientId: "client-1", TrafficDeltaBytes: 100, Seq: 5},
				},
			},
		},
	}
}

// TestEnqueueInboundAgentMessageDoesNotDropUsageSnapshot guards IN-C1: a
// snapshot carrying one-shot client-usage deltas must NOT be dropped under
// load (drop-oldest) — it blocks until accepted, preserving the queued
// message instead of discarding traffic that the agent never resends.
func TestEnqueueInboundAgentMessageDoesNotDropUsageSnapshot(t *testing.T) {
	priorityInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	regularInbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	stale := heartbeatMessageForTest("stale")
	regularInbound <- stale // queue full

	usage := usageSnapshotMessageForTest()
	done := make(chan bool, 1)
	go func() {
		done <- enqueueInboundAgentMessage(context.Background(), priorityInbound, regularInbound, usage, nil)
	}()

	// The usage enqueue must block rather than drop the stale heartbeat.
	select {
	case got := <-regularInbound:
		if got != stale {
			t.Fatal("usage snapshot dropped the stale heartbeat; want blocking, not drop-oldest")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout draining stale heartbeat")
	}
	// After draining, the blocked enqueue completes and delivers the usage snapshot.
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("enqueueInboundAgentMessage(usage) = false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("usage enqueue did not complete after space freed")
	}
	select {
	case got := <-regularInbound:
		if got != usage {
			t.Fatal("usage snapshot not delivered to regularInbound")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("usage snapshot missing from regularInbound")
	}
}

func heartbeatMessageForTest(nodeName string) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Heartbeat{
			Heartbeat: &gatewayrpc.Heartbeat{
				NodeName:       nodeName,
				FleetGroupId:   "default",
				Version:        "1.0.0",
				ReadOnly:       false,
				ObservedAtUnix: 1,
			},
		},
	}
}

func jobAcknowledgementMessageForTest(jobID string) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobAcknowledgement{
			JobAcknowledgement: &gatewayrpc.JobAcknowledgement{
				JobId:          jobID,
				ObservedAtUnix: 1,
			},
		},
	}
}

func enqueueJobForAgent(t *testing.T, server *Server, agentID string, idempotencyKey string, now time.Time) jobs.Job {
	t.Helper()

	job, err := server.jobs.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{agentID},
		TTL:            time.Minute,
		IdempotencyKey: idempotencyKey,
		ActorID:        "user-1",
		ReadOnlyAgents: map[string]bool{
			agentID: false,
		},
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	return job
}

func authenticatedAgentContextForTest(agentID string) context.Context {
	certificate := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: agentID,
		},
	}
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{
			State: tls.ConnectionState{
				PeerCertificates: []*x509.Certificate{certificate},
			},
		},
	})
}

type fakeGatewayConnectStream struct {
	ctx  context.Context
	sent []*gatewayrpc.ConnectServerMessage
}

func newFakeGatewayConnectStream(ctx context.Context) *fakeGatewayConnectStream {
	return &fakeGatewayConnectStream{
		ctx:  ctx,
		sent: make([]*gatewayrpc.ConnectServerMessage, 0),
	}
}

func (s *fakeGatewayConnectStream) Send(message *gatewayrpc.ConnectServerMessage) error {
	s.sent = append(s.sent, message)
	return nil
}

func (s *fakeGatewayConnectStream) Recv() (*gatewayrpc.ConnectClientMessage, error) {
	return nil, io.EOF
}

func (s *fakeGatewayConnectStream) SetHeader(_ metadata.MD) error {
	return nil
}

func (s *fakeGatewayConnectStream) SendHeader(_ metadata.MD) error {
	return nil
}

func (s *fakeGatewayConnectStream) SetTrailer(_ metadata.MD) {}

func (s *fakeGatewayConnectStream) Context() context.Context {
	return s.ctx
}

func (s *fakeGatewayConnectStream) SendMsg(_ any) error {
	return nil
}

func (s *fakeGatewayConnectStream) RecvMsg(_ any) error {
	return io.EOF
}

type fakeRuntimeReloadClient struct {
	reloadCalls int
}

func (c *fakeRuntimeReloadClient) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return telemt.RuntimeState{}, nil
}

func (c *fakeRuntimeReloadClient) PatchConfig(context.Context, map[string]any, string) (telemt.PatchConfigResult, error) {
	return telemt.PatchConfigResult{}, nil
}

func (c *fakeRuntimeReloadClient) GetManagedConfig(context.Context) (map[string]any, string, error) {
	return nil, "", nil
}

func (c *fakeRuntimeReloadClient) HealthReady(context.Context) (bool, string, error) {
	return true, "", nil
}

func (c *fakeRuntimeReloadClient) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	return telemt.ClientUsageMetricsSnapshot{}, nil
}

func (c *fakeRuntimeReloadClient) FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error) {
	return nil, nil
}

func (c *fakeRuntimeReloadClient) ExecuteRuntimeReload(context.Context) error {
	c.reloadCalls++
	return nil
}

func (c *fakeRuntimeReloadClient) CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeRuntimeReloadClient) UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeRuntimeReloadClient) DeleteClient(context.Context, string) error {
	return nil
}

func (c *fakeRuntimeReloadClient) InvalidateSlowDataCache() {}

func (c *fakeRuntimeReloadClient) FetchSystemInfo(context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, nil
}

func (c *fakeRuntimeReloadClient) FetchDiscoveredUsers(_ context.Context, _ string) ([]telemt.DiscoveredUser, error) {
	return nil, nil
}

func (c *fakeRuntimeReloadClient) ResetUserQuota(context.Context, string) (telemt.ResetUserQuotaResult, error) {
	return telemt.ResetUserQuotaResult{}, nil
}

// ---- In-stream cert renewal tests -----------------------------------------------

// fakeSendSession captures outbound ConnectServerMessages sent by the handler.
type fakeSendSession struct {
	sent []*gatewayrpc.ConnectServerMessage
}

func (s *fakeSendSession) Send(msg *gatewayrpc.ConnectServerMessage) error {
	s.sent = append(s.sent, msg)
	return nil
}

func (s *fakeSendSession) Recv() (*gatewayrpc.ConnectClientMessage, error) {
	return nil, io.EOF
}

func (s *fakeSendSession) Context() context.Context {
	return context.Background()
}

// TestHandleInStreamRenewalRequestRejectsRevokedAgent guards H-3: a revoked
// agent whose Connect stream is still alive must not be able to renew its
// certificate over the stream (which would also re-pin its serial and defeat
// the revocation + serial-pin defenses).
func TestHandleInStreamRenewalRequestRejectsRevokedAgent(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	srv.mu.Lock()
	srv.revokedAgentIDs["agent-1"] = struct{}{}
	srv.mu.Unlock()

	sess := &fakeSendSession{}
	srv.handleInStreamRenewalRequest(
		context.Background(), "agent-1", sess,
		&gatewayrpc.RenewalRequest{AgentId: "agent-1", CsrPem: "unused-because-revoked"},
	)

	if len(sess.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sess.sent))
	}
	resp := sess.sent[0].GetRenewalResponse()
	if resp == nil {
		t.Fatal("response body is nil, want RenewalResponse")
	}
	if resp.GetError() == "" {
		t.Fatal("revoked agent renewal must return an error")
	}
	if resp.GetCertificatePem() != "" {
		t.Fatal("revoked agent must not receive a certificate")
	}
}

func TestHandleInStreamRenewalRequestSucceeds(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if srv.authority == nil {
		t.Fatal("server authority is nil")
	}

	// Build a CSR for agent-1 using a fresh keypair.
	agentKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "agent-1"},
	}, agentKey)
	if err != nil {
		t.Fatalf("CreateCertificateRequest: %v", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	sess := &fakeSendSession{}
	srv.handleInStreamRenewalRequest(
		context.Background(), "agent-1", sess,
		&gatewayrpc.RenewalRequest{AgentId: "agent-1", CsrPem: csrPEM},
	)

	if len(sess.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sess.sent))
	}
	resp := sess.sent[0].GetRenewalResponse()
	if resp == nil {
		t.Fatal("response body is nil, want RenewalResponse")
	}
	if resp.GetError() != "" {
		t.Fatalf("RenewalResponse.error = %q, want empty", resp.GetError())
	}
	if resp.GetCertificatePem() == "" {
		t.Fatal("RenewalResponse.certificate_pem is empty")
	}
	if resp.GetCaPem() == "" {
		t.Fatal("RenewalResponse.ca_pem is empty")
	}
	if resp.GetExpiresAtUnix() == 0 {
		t.Fatal("RenewalResponse.expires_at_unix is zero")
	}

	// Validate the returned cert chains to the panel CA.
	caBlock, _ := pem.Decode([]byte(resp.GetCaPem()))
	if caBlock == nil {
		t.Fatal("ca_pem decode failed")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate(ca): %v", err)
	}
	certBlock, _ := pem.Decode([]byte(resp.GetCertificatePem()))
	if certBlock == nil {
		t.Fatal("certificate_pem decode failed")
	}
	leafCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate(leaf): %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	if _, err := leafCert.Verify(x509.VerifyOptions{Roots: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("cert verification failed: %v", err)
	}
}

func TestHandleInStreamRenewalRequestRejectsAgentIDMismatch(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	sess := &fakeSendSession{}
	srv.handleInStreamRenewalRequest(
		context.Background(), "agent-1", sess,
		&gatewayrpc.RenewalRequest{AgentId: "agent-2", CsrPem: "irrelevant"},
	)

	if len(sess.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sess.sent))
	}
	resp := sess.sent[0].GetRenewalResponse()
	if resp == nil {
		t.Fatal("expected RenewalResponse, got nil")
	}
	if resp.GetError() == "" {
		t.Fatal("expected error in RenewalResponse for agent_id mismatch, got empty")
	}
}

func TestHandleInStreamRenewalRequestRejectsInvalidCSR(t *testing.T) {
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	sess := &fakeSendSession{}
	srv.handleInStreamRenewalRequest(
		context.Background(), "agent-1", sess,
		&gatewayrpc.RenewalRequest{AgentId: "agent-1", CsrPem: "not-a-csr"},
	)

	if len(sess.sent) != 1 {
		t.Fatalf("len(sent) = %d, want 1", len(sess.sent))
	}
	resp := sess.sent[0].GetRenewalResponse()
	if resp == nil {
		t.Fatal("expected RenewalResponse, got nil")
	}
	if resp.GetError() == "" {
		t.Fatal("expected error in RenewalResponse for invalid CSR, got empty")
	}
}
