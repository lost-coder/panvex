package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestEnqueueReceivedJobQueueFullFailsFast reproduces audit #9a: with a
// full lane (no workers draining — simulating 17 long-running jobs:
// 1 executing + 16 queued), the next job must NOT block the inbound
// pump; it must produce a terminal failed JobResult and release its
// in-flight reservation so the panel's retry can be executed later.
func TestEnqueueReceivedJobQueueFullFailsFast(t *testing.T) {
	ctx := context.Background()
	tracker := newJobInflightTracker()
	jobQueues := map[jobPipeline]chan *gatewayrpc.JobCommand{
		jobPipelineRuntimeReload:  make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
		jobPipelineClientMutation: make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
		jobPipelineDefault:        make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
	}
	critical := make(chan *gatewayrpc.ConnectClientMessage, 64)

	for i := 0; i < jobQueueCapacity; i++ {
		job := &gatewayrpc.JobCommand{Id: fmt.Sprintf("job-%d", i), Action: "config.apply"}
		if !enqueueReceivedJob(ctx, "agent-1", nil, tracker, jobQueues, critical, job) {
			t.Fatalf("enqueue job-%d: want accepted", i)
		}
	}

	// The overflow enqueue must return promptly — before the fix it
	// blocks forever on the full lane (head-of-line blocking the pump,
	// so renewal responses would never be processed).
	overflowDone := make(chan bool, 1)
	go func() {
		overflowDone <- enqueueReceivedJob(ctx, "agent-1", nil, tracker, jobQueues, critical,
			&gatewayrpc.JobCommand{Id: "job-overflow", Action: "config.apply"})
	}()
	select {
	case <-overflowDone:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueueReceivedJob blocked on a full lane")
	}

	// It must have produced a terminal failed JobResult on criticalOutbound.
	var failed *gatewayrpc.JobResult
drain:
	for {
		select {
		case msg := <-critical:
			if jr := msg.GetJobResult(); jr != nil && jr.GetJobId() == "job-overflow" {
				failed = jr
				break drain
			}
		default:
			break drain
		}
	}
	if failed == nil {
		t.Fatal("no JobResult for the overflow job on criticalOutbound")
	}
	if failed.GetSuccess() {
		t.Fatal("overflow JobResult.Success = true, want false")
	}
	if failed.GetMessage() != "job queue full, retry later" {
		t.Fatalf("overflow JobResult.Message = %q, want %q", failed.GetMessage(), "job queue full, retry later")
	}
	// Reservation released: the panel's retry of the same job id must be accepted.
	if !tracker.reserve("job-overflow") {
		t.Fatal("in-flight reservation was not released on queue-full")
	}
}
