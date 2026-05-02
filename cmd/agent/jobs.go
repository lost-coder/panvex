package main

import (
	"context"
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
		select {
		case <-connectionCtx.Done():
			return false
		case criticalOutbound <- jobAcknowledgementMessage(agentID, jobID, time.Now()):
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
	agent *runtime.Agent,
	tracker *jobInflightTracker,
	jobQueues map[jobPipeline]chan *gatewayrpc.JobCommand,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	for pipeline, queue := range jobQueues {
		workerCount := jobWorkerCountForPipeline(pipeline)
		for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
			go runJobWorker(connectionCtx, agent, tracker, queue, criticalOutbound)
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

		jobCtx, cancelJob := context.WithTimeout(connectionCtx, jobExecutionTimeout)
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
