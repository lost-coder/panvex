package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

type jobPipeline string

const (
	jobPipelineRuntimeReload  jobPipeline = "runtime_reload"
	jobPipelineClientMutation jobPipeline = "client_mutation"
	jobPipelineDefault        jobPipeline = "default"
)

func jobPipelineForAction(action string) jobPipeline {
	switch action {
	case "runtime.reload":
		return jobPipelineRuntimeReload
	case "telemetry.refresh_diagnostics":
		return jobPipelineRuntimeReload
	case "client.create", "client.update", "client.rotate_secret", "client.delete":
		return jobPipelineClientMutation
	default:
		return jobPipelineDefault
	}
}

func shouldSendRuntimeSnapshotAfterJob(action string, success bool) bool {
	if !success {
		return false
	}

	return action == "telemetry.refresh_diagnostics"
}

func jobWorkerCountForPipeline(pipeline jobPipeline) int {
	switch pipeline {
	case jobPipelineRuntimeReload:
		return 2
	case jobPipelineClientMutation:
		return 1
	default:
		return 1
	}
}

// jobExecutionBudget returns the ctx deadline for executing one job.
// config.apply derives its budget from the payload's health_timeout_s
// (panel sends 30 by default) so the agent-side deadline always exceeds
// the apply sequence it has to cover: preflight + PATCH + restart +
// health polls. Everything else keeps the conservative default.
func jobExecutionBudget(job *gatewayrpc.JobCommand) time.Duration {
	switch job.GetAction() {
	case "config.apply":
		var p struct {
			HealthTimeoutS int `json:"health_timeout_s"`
		}
		_ = json.Unmarshal([]byte(job.GetPayloadJson()), &p)
		health := time.Duration(p.HealthTimeoutS) * time.Second
		if health <= 0 {
			health = 30 * time.Second
		}
		return health + configApplyRestartAllowance + configApplyBudgetMargin
	case "agent.self-update":
		return selfUpdateExecutionTimeout
	default:
		return jobExecutionTimeout
	}
}

type jobInflightTracker struct {
	mu     sync.Mutex
	jobIDs map[string]struct{}
}

func newJobInflightTracker() *jobInflightTracker {
	return &jobInflightTracker{
		jobIDs: make(map[string]struct{}),
	}
}

func (t *jobInflightTracker) reserve(jobID string) bool {
	if jobID == "" {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.jobIDs[jobID]; exists {
		return false
	}
	t.jobIDs[jobID] = struct{}{}
	return true
}

func (t *jobInflightTracker) release(jobID string) {
	if jobID == "" {
		return
	}

	t.mu.Lock()
	delete(t.jobIDs, jobID)
	t.mu.Unlock()
}

func enqueueReceivedJob(
	connectionCtx context.Context,
	agentID string,
	agent *runtime.Agent,
	tracker *jobInflightTracker,
	jobQueues map[jobPipeline]chan *gatewayrpc.JobCommand,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
	job *gatewayrpc.JobCommand,
) bool {
	if job == nil {
		return false
	}

	jobID := job.GetId()
	if jobID != "" && !tracker.reserve(jobID) {
		// Duplicate delivery (job already in-flight or just completed). If we
		// already have a cached result, resend it rather than a bare ack so a
		// JobResult lost in transit after the first ack still reaches the
		// control-plane on its retry — without re-executing the job. Falls
		// back to an ack when no result is cached yet (still executing).
		outbound := jobAcknowledgementMessage(agentID, jobID, time.Now())
		if agent != nil {
			if cached, ok := agent.CompletedJobResult(jobID, time.Now()); ok {
				outbound = &gatewayrpc.ConnectClientMessage{
					Body: &gatewayrpc.ConnectClientMessage_JobResult{JobResult: cached},
				}
			}
		}
		select {
		case <-connectionCtx.Done():
			return false
		case criticalOutbound <- outbound:
			return true
		}
	}

	targetQueue := jobQueues[jobPipelineForAction(job.GetAction())]
	select {
	case <-connectionCtx.Done():
		tracker.release(jobID)
		return false
	case targetQueue <- job:
	}

	select {
	case <-connectionCtx.Done():
		tracker.release(jobID)
		return false
	case criticalOutbound <- jobAcknowledgementMessage(agentID, jobID, time.Now()):
		return true
	}
}

func startJobWorkers(
	connectionCtx context.Context,
	streamWG *sync.WaitGroup,
	agent *runtime.Agent,
	tracker *jobInflightTracker,
	jobQueues map[jobPipeline]chan *gatewayrpc.JobCommand,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	for pipeline, queue := range jobQueues {
		workerCount := jobWorkerCountForPipeline(pipeline)
		for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
			streamWG.Add(1)
			go func(queue <-chan *gatewayrpc.JobCommand) {
				defer streamWG.Done()
				runJobWorker(connectionCtx, agent, tracker, queue, criticalOutbound)
			}(queue)
		}
	}
}

// releaseQueuedJobs drains jobs still sitting in the per-connection queues
// after the workers exited and releases their in-flight reservations. B4:
// the tracker outlives the connection, so anything left reserved here
// would never be executable again after reconnect.
func releaseQueuedJobs(tracker *jobInflightTracker, jobQueues map[jobPipeline]chan *gatewayrpc.JobCommand) {
	for _, queue := range jobQueues {
		drainJobQueue(tracker, queue)
	}
}

func drainJobQueue(tracker *jobInflightTracker, queue <-chan *gatewayrpc.JobCommand) {
	for {
		select {
		case job := <-queue:
			if job != nil {
				tracker.release(job.GetId())
			}
		default:
			return
		}
	}
}

func runJobWorker(
	connectionCtx context.Context,
	agent *runtime.Agent,
	tracker *jobInflightTracker,
	jobQueue <-chan *gatewayrpc.JobCommand,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	for {
		var job *gatewayrpc.JobCommand
		select {
		case <-connectionCtx.Done():
			return
		case job = <-jobQueue:
		}
		if job == nil {
			continue
		}
		jobID := job.GetId()

		jobCtx, cancelJob := context.WithTimeout(connectionCtx, jobExecutionBudget(job))
		result := agent.HandleJob(jobCtx, job, time.Now())
		cancelJob()
		slog.Debug("job completed", "job_id", jobID, "action", job.GetAction(), "success", result.Success)

		if shouldSendRuntimeSnapshotAfterJob(job.GetAction(), result.Success) {
			runtimeCtx, cancelRuntime := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
			snapshot, err := agent.BuildRuntimeSnapshot(runtimeCtx, time.Now())
			cancelRuntime()
			if err != nil {
				result.Success = false
				result.Message = "diagnostics refresh failed: " + err.Error()
			} else {
				select {
				case <-connectionCtx.Done():
					tracker.release(jobID)
					return
				case criticalOutbound <- &gatewayrpc.ConnectClientMessage{
					Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
				}:
				}
			}
		}
		select {
		case <-connectionCtx.Done():
			tracker.release(jobID)
			return
		case criticalOutbound <- &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_JobResult{JobResult: result},
		}:
		}
		tracker.release(jobID)
	}
}

func jobAcknowledgementMessage(agentID string, jobID string, observedAt time.Time) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobAcknowledgement{
			JobAcknowledgement: &gatewayrpc.JobAcknowledgement{
				AgentId:        agentID,
				JobId:          jobID,
				ObservedAtUnix: observedAt.UTC().Unix(),
			},
		},
	}
}
