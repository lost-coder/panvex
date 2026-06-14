package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestJobExecutionBudget guards A5: the blanket 30s job timeout cannot fit a
// config.apply whose own health probe is 30s plus a Telemt restart — the
// budget must be derived from the action (and its payload).
func TestJobExecutionBudget(t *testing.T) {
	cases := []struct {
		name string
		job  *gatewayrpc.JobCommand
		want time.Duration
	}{
		{
			name: "default action keeps the 30s budget",
			job:  &gatewayrpc.JobCommand{Action: "runtime.reload"},
			want: jobExecutionTimeout,
		},
		{
			name: "config.apply with default health timeout",
			job:  &gatewayrpc.JobCommand{Action: "config.apply", PayloadJson: `{"patch":{"general":{}}}`},
			want: 30*time.Second + configApplyRestartAllowance + configApplyBudgetMargin,
		},
		{
			name: "config.apply with explicit health timeout",
			job:  &gatewayrpc.JobCommand{Action: "config.apply", PayloadJson: `{"health_timeout_s":60,"patch":{}}`},
			want: 60*time.Second + configApplyRestartAllowance + configApplyBudgetMargin,
		},
		{
			name: "config.apply with malformed payload falls back to default health",
			job:  &gatewayrpc.JobCommand{Action: "config.apply", PayloadJson: `not-json`},
			want: 30*time.Second + configApplyRestartAllowance + configApplyBudgetMargin,
		},
		{
			name: "self-update gets the download budget",
			job:  &gatewayrpc.JobCommand{Action: "agent.self-update"},
			want: selfUpdateExecutionTimeout,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jobExecutionBudget(tc.job); got != tc.want {
				t.Fatalf("jobExecutionBudget = %v, want %v", got, tc.want)
			}
		})
	}
}

// newTestJobQueues returns the three per-pipeline queues with capacity 1.
func newTestJobQueues() map[jobPipeline]chan *gatewayrpc.JobCommand {
	return map[jobPipeline]chan *gatewayrpc.JobCommand{
		jobPipelineRuntimeReload:  make(chan *gatewayrpc.JobCommand, 1),
		jobPipelineClientMutation: make(chan *gatewayrpc.JobCommand, 1),
		jobPipelineDefault:        make(chan *gatewayrpc.JobCommand, 1),
	}
}

// TestStartJobWorkersJoinWaitGroupOnCancel guards B4: every job worker must
// register in the per-connection WaitGroup so RunOnce's drain actually waits
// for them — previously they were spawned untracked and a quick reconnect
// could race a still-running worker.
func TestStartJobWorkersJoinWaitGroupOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	agent := runtime.New(runtime.Config{AgentID: "agent-1"}, failingTelemt{})
	tracker := newJobInflightTracker()
	out := make(chan *gatewayrpc.ConnectClientMessage, 8)

	startJobWorkers(ctx, &wg, agent, tracker, newTestJobQueues(), out)
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("job workers did not join the WaitGroup after cancel")
	}
}

// TestReleaseQueuedJobsFreesReservations guards B4: with the tracker hoisted
// above the connection, jobs that were queued but never picked up by a
// worker must have their reservations released at teardown, or they could
// never be executed again after reconnect.
func TestReleaseQueuedJobsFreesReservations(t *testing.T) {
	tracker := newJobInflightTracker()
	queues := newTestJobQueues()
	job := &gatewayrpc.JobCommand{Id: "job-9", Action: "runtime.reload"}
	if !tracker.reserve(job.GetId()) {
		t.Fatal("setup: reserve failed")
	}
	queues[jobPipelineRuntimeReload] <- job

	releaseQueuedJobs(tracker, queues)

	if !tracker.reserve("job-9") {
		t.Fatal("queued job reservation must be released after drain")
	}
}

// TestDuplicateDeliveryAcrossConnectionsIsNotReExecuted guards B4: the
// in-flight tracker is shared across reconnects, so a job re-delivered on a
// new connection while its first execution is still running must be answered
// with an ack/cached result instead of being queued for a second execution.
func TestDuplicateDeliveryAcrossConnectionsIsNotReExecuted(t *testing.T) {
	tracker := newJobInflightTracker() // hoisted: one tracker, two connections
	agent := runtime.New(runtime.Config{AgentID: "agent-1"}, failingTelemt{})
	job := &gatewayrpc.JobCommand{Id: "job-7", Action: "runtime.reload"}

	// Connection 1 accepts the job; no worker runs, so it stays in flight.
	queues1 := newTestJobQueues()
	out1 := make(chan *gatewayrpc.ConnectClientMessage, 4)
	if !enqueueReceivedJob(context.Background(), "agent-1", agent, tracker, queues1, out1, job) {
		t.Fatal("first delivery must be accepted")
	}

	// Connection 2 (after reconnect) re-delivers the same job.
	queues2 := newTestJobQueues()
	out2 := make(chan *gatewayrpc.ConnectClientMessage, 4)
	if !enqueueReceivedJob(context.Background(), "agent-1", agent, tracker, queues2, out2, job) {
		t.Fatal("duplicate delivery must still be answered with an ack")
	}
	if got := len(queues2[jobPipelineRuntimeReload]); got != 0 {
		t.Fatalf("duplicate must not be queued for re-execution, queue len = %d", got)
	}
}
