package jobs

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// Pipeline is a job execution lane. Lanes exist for isolation — a slow lane
// must not head-of-line-block another.
type Pipeline string

const (
	PipelineRuntimeReload  Pipeline = "runtime_reload"
	PipelineClientMutation Pipeline = "client_mutation"
	PipelineDefault        Pipeline = "default"
)

const (
	// QueueCapacity is the per-lane channel capacity. The connection layer
	// sizes the lane channels with it; a full lane fails the job fast rather
	// than blocking the inbound pump (audit #9a).
	QueueCapacity = 16

	jobExecutionTimeout = 30 * time.Second
	// A5: per-action execution budgets. config.apply = health-probe budget
	// (from the payload, default 30s) + a restart allowance + safety margin;
	// the blanket jobExecutionTimeout strangled the apply sequence and
	// killed the ctx mid-health-poll. Self-update needs room for the
	// archive download on slow links.
	configApplyRestartAllowance = 30 * time.Second
	configApplyBudgetMargin     = 30 * time.Second
	selfUpdateExecutionTimeout  = 5 * time.Minute
	// telemtUpdateExecutionTimeout covers the full telemtupdate.Execute
	// cycle for the telemt.update action, which is heavier than
	// self-update's plain download+swap: it also restarts Telemt and runs a
	// post-restart health-gate (with a rollback-and-restart branch on
	// failure). The blanket jobExecutionTimeout (30s) would strangle this
	// exactly like it did config.apply before A5 — the ctx would expire
	// mid-health-poll and Execute would roll back an update that actually
	// succeeded, reporting a false failure. Budget, summed from
	// telemtupdate's own constants:
	//   5m  archive download (telemtupdate.defaultDownloadTimeout stall cap;
	//       the checksum sidecar reuses the same client but is a ~1KB fetch,
	//       not worth double-booking another full 5m for)
	//   60s post-restart health-gate (telemtupdate's defaultHealthTimeout)
	//   60s rollback allowance (restoreBackup + a second Restart call on
	//       the health-gate-timeout / restart-failure branches)
	//   60s margin (extract/swap file I/O, process/JSON overhead)
	// = 8m, rounded up to 10m for headroom.
	telemtUpdateExecutionTimeout = 10 * time.Minute
)

func pipelineForAction(action string) Pipeline {
	switch action {
	case "telemetry.refresh_diagnostics":
		return PipelineRuntimeReload
	case "client.create", "client.update", "client.rotate_secret", "client.delete", "client.reset_quota":
		return PipelineClientMutation
	default:
		return PipelineDefault
	}
}

func shouldSendRuntimeSnapshotAfterJob(action string, success bool) bool {
	if !success {
		return false
	}

	return action == "telemetry.refresh_diagnostics"
}

// workerCountForPipeline sets per-lane concurrency. Every lane is
// single-worker BY DESIGN; the lanes exist for isolation (a slow lane cannot
// head-of-line-block another), not for intra-lane parallelism:
//
//   - runtime_reload: telemetry.refresh_diagnostics runs here, isolated from
//     the mutation lanes so a slow diagnostics pull cannot delay client
//     mutations. (The historical name comes from the removed runtime.reload
//     action that used to share this lane; the lane const is kept to avoid a
//     rename ripple through the connection layer.)
//   - client_mutation: per-client mutations must apply in delivery order;
//     two workers would let client.update#2 overtake #1.
//   - default: config.apply / self-update / telemt.update / transport
//     switches are heavyweight node-level operations — one at a time is the
//     safe default.
func workerCountForPipeline(Pipeline) int {
	return 1
}

// executionBudget returns the ctx deadline for executing one job.
// config.apply derives its budget from the payload's health_timeout_s
// (panel sends 30 by default) so the agent-side deadline always exceeds
// the apply sequence it has to cover: preflight + PATCH + restart +
// health polls. self-update and telemt.update get fixed budgets sized for
// their own download/restart/health-gate cycles (see the constants above).
// Everything else keeps the conservative default.
func executionBudget(job *gatewayrpc.JobCommand) time.Duration {
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
	case "telemt.update":
		return telemtUpdateExecutionTimeout
	default:
		return jobExecutionTimeout
	}
}

type InflightTracker struct {
	mu     sync.Mutex
	jobIDs map[string]struct{}
}

func NewInflightTracker() *InflightTracker {
	return &InflightTracker{
		jobIDs: make(map[string]struct{}),
	}
}

func (t *InflightTracker) reserve(jobID string) bool {
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

func (t *InflightTracker) release(jobID string) {
	if jobID == "" {
		return
	}

	t.mu.Lock()
	delete(t.jobIDs, jobID)
	t.mu.Unlock()
}

func EnqueueReceived(
	connectionCtx context.Context,
	agentID string,
	agent *runtime.Agent,
	tracker *InflightTracker,
	jobQueues map[Pipeline]chan *gatewayrpc.JobCommand,
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
		outbound := acknowledgementMessage(agentID, jobID, time.Now())
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

	targetQueue := jobQueues[pipelineForAction(job.GetAction())]
	select {
	case <-connectionCtx.Done():
		tracker.release(jobID)
		return false
	case targetQueue <- job:
	default:
		// Audit #9a: the lane is full. Blocking here would head-of-line
		// block the inbound pump — no further Recv() means renewal
		// responses and every other message stall behind slow jobs. Fail
		// fast with a terminal JobResult instead; the panel retries
		// terminal failures, and the released reservation lets the retry
		// execute once the lane drains.
		tracker.release(jobID)
		failed := &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_JobResult{JobResult: &gatewayrpc.JobResult{
				AgentId:        agentID,
				JobId:          jobID,
				Success:        false,
				Message:        "job queue full, retry later",
				ObservedAtUnix: time.Now().UTC().Unix(),
			}},
		}
		select {
		case <-connectionCtx.Done():
			return false
		case criticalOutbound <- failed:
			return true
		}
	}

	select {
	case <-connectionCtx.Done():
		tracker.release(jobID)
		return false
	case criticalOutbound <- acknowledgementMessage(agentID, jobID, time.Now()):
		return true
	}
}

func StartWorkers(
	connectionCtx context.Context,
	streamWG *sync.WaitGroup,
	agent *runtime.Agent,
	tracker *InflightTracker,
	jobQueues map[Pipeline]chan *gatewayrpc.JobCommand,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	for pipeline, queue := range jobQueues {
		workerCount := workerCountForPipeline(pipeline)
		for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
			streamWG.Add(1)
			go func(queue <-chan *gatewayrpc.JobCommand) {
				defer streamWG.Done()
				runWorker(connectionCtx, agent, tracker, queue, criticalOutbound)
			}(queue)
		}
	}
}

// ReleaseQueued drains jobs still sitting in the per-connection queues
// after the workers exited and releases their in-flight reservations. B4:
// the tracker outlives the connection, so anything left reserved here
// would never be executable again after reconnect.
func ReleaseQueued(tracker *InflightTracker, jobQueues map[Pipeline]chan *gatewayrpc.JobCommand) {
	for _, queue := range jobQueues {
		drainQueue(tracker, queue)
	}
}

func drainQueue(tracker *InflightTracker, queue <-chan *gatewayrpc.JobCommand) {
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

func runWorker(
	connectionCtx context.Context,
	agent *runtime.Agent,
	tracker *InflightTracker,
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

		jobCtx, cancelJob := context.WithTimeout(connectionCtx, executionBudget(job))
		result := agent.HandleJob(jobCtx, job, time.Now())
		cancelJob()
		slog.Debug("job completed", "job_id", jobID, "action", job.GetAction(), "success", result.Success)

		if shouldSendRuntimeSnapshotAfterJob(job.GetAction(), result.Success) {
			runtimeCtx, cancelRuntime := context.WithTimeout(connectionCtx, runtime.OperationTimeout)
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

func acknowledgementMessage(agentID string, jobID string, observedAt time.Time) *gatewayrpc.ConnectClientMessage {
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
