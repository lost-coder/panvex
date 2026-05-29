package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// telemtUnreachableThreshold is the wall-clock window of consecutive
// FetchRuntimeState failures before the agent declares Telemt unreachable
// and starts emitting BuildRuntimeUnreachableSnapshot payloads to the panel.
// Sized to absorb a normal Telemt restart (typically 5–15s) without
// triggering a false alarm.
const telemtUnreachableThreshold = 30 * time.Second

func startPollingWorkers(
	connectionCtx context.Context,
	schedule connectionSchedule,
	agent *runtime.Agent,
	telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	startPeriodicPollingWorker(connectionCtx, schedule.config(pollHeartbeat),
		makeHeartbeatTick(connectionCtx, agent, telemetryOutbound))

	runtimeBuffer := runtime.NewRuntimeRingBuffer(8)
	startRuntimePollWorker(connectionCtx, schedule.config(pollRuntime), agent, runtimeBuffer, telemetryOutbound)
	startRuntimeUploadWorker(connectionCtx, schedule.config(pollRuntimeUpload), runtimeBuffer, telemetryOutbound)

	startPeriodicPollingWorker(connectionCtx, schedule.config(pollUsage),
		makeUsageSnapshotTick(connectionCtx, agent, telemetryOutbound))
	startPeriodicPollingWorker(connectionCtx, schedule.config(pollIPPoll),
		makeIPPollTick(connectionCtx, agent))
	startPeriodicPollingWorker(connectionCtx, schedule.config(pollIPUpload),
		makeIPUploadTick(connectionCtx, agent, telemetryOutbound))
}

func makeHeartbeatTick(connectionCtx context.Context, agent *runtime.Agent, telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage) func(time.Time) {
	return func(observedAt time.Time) {
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, heartbeatMessage(agent, observedAt)) {
			slog.Debug("heartbeat sent", "agent_id", agent.AgentID())
			return
		}
		if connectionCtx.Err() == nil {
			slog.Warn("heartbeat dropped due to outbound backpressure")
		}
	}
}

func makeUsageSnapshotTick(connectionCtx context.Context, agent *runtime.Agent, telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage) func(time.Time) {
	return func(observedAt time.Time) {
		usageCtx, cancelUsage := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
		snapshot, err := agent.BuildUsageSnapshot(usageCtx, observedAt)
		cancelUsage()
		if err != nil {
			slog.Error("usage snapshot failed", "error", err)
			return
		}
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
		}) {
			slog.Debug("usage snapshot enqueued", "agent_id", agent.AgentID())
			return
		}
		if connectionCtx.Err() == nil {
			slog.Warn("usage snapshot dropped due to outbound backpressure")
		}
	}
}

func makeIPPollTick(connectionCtx context.Context, agent *runtime.Agent) func(time.Time) {
	return func(observedAt time.Time) {
		ipPollCtx, cancelIPPoll := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
		err := agent.PollActiveIPs(ipPollCtx)
		cancelIPPoll()
		if err != nil {
			slog.Error("ip poll failed", "error", err)
		}
	}
}

func makeIPUploadTick(connectionCtx context.Context, agent *runtime.Agent, telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage) func(time.Time) {
	return func(observedAt time.Time) {
		snapshot := agent.BuildIPSnapshot(observedAt)
		if len(snapshot.ClientIps) == 0 {
			return
		}
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
		}) {
			slog.Debug("ip snapshot enqueued", "agent_id", agent.AgentID(), "client_ips", len(snapshot.ClientIps))
			return
		}
		// IN-H4: enqueue failed (backpressure) — restore the flushed IPs so
		// they are retried on the next tick instead of being lost.
		agent.RestoreIPSnapshot(snapshot)
		if connectionCtx.Err() == nil {
			slog.Warn("ip snapshot dropped due to outbound backpressure; restored for retry")
		}
	}
}

func startPeriodicPollingWorker(
	connectionCtx context.Context,
	config pollingGroupConfig,
	run func(observedAt time.Time),
) {
	if !config.Enabled || config.Interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(config.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-connectionCtx.Done():
				return
			case observedAt := <-ticker.C:
				run(observedAt.UTC())
			}
		}
	}()
}

// telemtReachabilityTracker accumulates consecutive runtime-snapshot failures
// and decides when to start emitting BuildRuntimeUnreachableSnapshot payloads.
// Zero value is "Telemt healthy".
type telemtReachabilityTracker struct {
	firstFailureAt time.Time
}

// startRuntimePollWorker polls Telemt at a fast interval and stores samples in the ring buffer.
func startRuntimePollWorker(
	connectionCtx context.Context,
	config pollingGroupConfig,
	agent *runtime.Agent,
	buffer *runtime.RuntimeRingBuffer,
	telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	if !config.Enabled || config.Interval <= 0 {
		return
	}

	go runRuntimePollLoop(connectionCtx, config, agent, buffer, telemetryOutbound)
}

func runRuntimePollLoop(
	connectionCtx context.Context,
	config pollingGroupConfig,
	agent *runtime.Agent,
	buffer *runtime.RuntimeRingBuffer,
	telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	consecutiveFailures := 0
	tracker := &telemtReachabilityTracker{}
	for {
		delay := nextRuntimePollDelay(agent, config, consecutiveFailures)
		observedAt, ok := waitRuntimePollTick(connectionCtx, delay)
		if !ok {
			return
		}
		performRuntimePoll(connectionCtx, agent, buffer, telemetryOutbound, observedAt,
			&consecutiveFailures, tracker)
	}
}

func nextRuntimePollDelay(agent *runtime.Agent, config pollingGroupConfig, consecutiveFailures int) time.Duration {
	delay := agent.RuntimeSnapshotInterval(config.Interval, runtimeInitializationFastInterval, time.Now())
	if consecutiveFailures > 0 {
		backoff := time.Duration(consecutiveFailures) * config.Interval
		if backoff > 5*time.Minute {
			backoff = 5 * time.Minute
		}
		delay = backoff
	}
	return delay
}

func waitRuntimePollTick(connectionCtx context.Context, delay time.Duration) (time.Time, bool) {
	timer := time.NewTimer(delay)
	select {
	case <-connectionCtx.Done():
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		return time.Time{}, false
	case observedAt := <-timer.C:
		return observedAt, true
	}
}

// performRuntimePoll executes one snapshot fetch, updates failure counters,
// and pushes either a real sample on success or — once we've been failing for
// telemtUnreachableThreshold — an unreachable snapshot directly to the
// outbound channel. The ring buffer is left untouched while we are unreachable
// so the upload worker keeps sending nothing (no aggregated noise).
func performRuntimePoll(
	connectionCtx context.Context,
	agent *runtime.Agent,
	buffer *runtime.RuntimeRingBuffer,
	telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage,
	observedAt time.Time,
	consecutiveFailures *int,
	tracker *telemtReachabilityTracker,
) {
	runtimeCtx, cancelRuntime := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
	snapshot, err := agent.BuildRuntimeSnapshot(runtimeCtx, observedAt.UTC())
	cancelRuntime()
	if err != nil {
		*consecutiveFailures++
		if tracker.firstFailureAt.IsZero() {
			tracker.firstFailureAt = observedAt.UTC()
		}
		if *consecutiveFailures <= 3 || *consecutiveFailures%10 == 0 {
			slog.Error("runtime poll failed", "attempt", *consecutiveFailures, "error", err)
		}
		if observedAt.UTC().Sub(tracker.firstFailureAt) >= telemtUnreachableThreshold {
			unreachable := agent.BuildRuntimeUnreachableSnapshot(observedAt.UTC(), tracker.firstFailureAt)
			if !enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
				Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: unreachable},
			}) && connectionCtx.Err() == nil {
				slog.Warn("telemt unreachable snapshot dropped due to outbound backpressure")
			}
		}
		return
	}
	*consecutiveFailures = 0
	tracker.firstFailureAt = time.Time{}
	buffer.Push(runtime.RuntimeSample{
		ObservedAt: observedAt.UTC(),
		Snapshot:   snapshot,
	})
}

// startRuntimeUploadWorker drains the ring buffer, aggregates samples, and sends one snapshot.
func startRuntimeUploadWorker(
	connectionCtx context.Context,
	config pollingGroupConfig,
	buffer *runtime.RuntimeRingBuffer,
	telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	if !config.Enabled || config.Interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(config.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-connectionCtx.Done():
				return
			case <-ticker.C:
				snapshot := buffer.DrainAndAggregate()
				if snapshot == nil {
					continue
				}
				if enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
					Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
				}) {
					slog.Debug("runtime snapshot enqueued")
					continue
				}
				if connectionCtx.Err() == nil {
					slog.Warn("runtime upload dropped due to outbound backpressure")
				}
			}
		}
	}()
}
