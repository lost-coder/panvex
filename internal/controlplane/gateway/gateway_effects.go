package gateway

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type jobResultEffect struct {
	agentID    string
	jobID      string
	success    bool
	message    string
	resultJSON string
	observedAt time.Time
}

type auditEffect struct {
	actorID  string
	action   string
	targetID string
	details  map[string]any
}

func enqueuePriorityResultEffect(
	connectionCtx context.Context,
	priorityResultEffects chan<- jobResultEffect,
	effect jobResultEffect,
) bool {
	if priorityResultEffects == nil {
		return false
	}
	if connectionCtx.Err() != nil {
		return false
	}

	select {
	case <-connectionCtx.Done():
		return false
	case priorityResultEffects <- effect:
		return true
	default:
		return false
	}
}

func enqueuePriorityAuditEffect(
	connectionCtx context.Context,
	priorityAuditEffects chan<- auditEffect,
	effect auditEffect,
) bool {
	if priorityAuditEffects == nil {
		return false
	}
	if connectionCtx.Err() != nil {
		return false
	}

	select {
	case <-connectionCtx.Done():
		return false
	case priorityAuditEffects <- effect:
		return true
	default:
		return false
	}
}

func drainPriorityResultEffects(
	priorityResultEffects <-chan jobResultEffect,
	recordClientJobResult func(agentID string, jobID string, success bool, message string, resultJSON string, observedAt time.Time),
) {
	for {
		select {
		case effect := <-priorityResultEffects:
			if effect.jobID == "" {
				continue
			}
			recordClientJobResult(
				effect.agentID,
				effect.jobID,
				effect.success,
				effect.message,
				effect.resultJSON,
				effect.observedAt,
			)
		default:
			return
		}
	}
}

func drainPriorityAuditEffects(
	priorityAuditEffects <-chan auditEffect,
	appendAudit func(actorID string, action string, targetID string, details map[string]any),
) {
	for {
		select {
		case effect := <-priorityAuditEffects:
			if effect.action == "" {
				continue
			}
			appendAudit(effect.actorID, effect.action, effect.targetID, effect.details)
		default:
			return
		}
	}
}

func enqueueRegularSnapshot(
	connectionCtx context.Context,
	regularSnapshots chan AgentSnapshot,
	snapshot AgentSnapshot,
	dropCounter prometheus.Counter,
	logger *slog.Logger,
) bool {
	if connectionCtx.Err() != nil {
		return false
	}

	// P4: usage-bearing snapshots now carry cumulative totals, not one-shot
	// deltas — dropping one is benign because the next tick's absolute total
	// catches up. So all regular snapshots share the same drop-oldest
	// (freshest-wins) path; there is no longer a blocking backpressure lane.
	select {
	case <-connectionCtx.Done():
		return false
	case regularSnapshots <- snapshot:
		return true
	default:
	}

	// Drop one stale regular snapshot to prioritize the freshest state.
	select {
	case <-regularSnapshots:
	default:
	}

	select {
	case <-connectionCtx.Done():
		return false
	case regularSnapshots <- snapshot:
	default:
		// A concurrent reader re-filled the slot between the drain above and
		// this send, so the fresh snapshot is discarded. Symmetric with the
		// inbound drop path — surface it instead of losing it silently (R4
		// §1.12). The next tick's absolute totals catch up, so this is benign
		// but worth observing under sustained backpressure.
		if dropCounter != nil {
			dropCounter.Inc()
		}
		if logger != nil {
			logger.DebugContext(connectionCtx, "dropped fresh regular snapshot under backpressure",
				"agent_id", snapshot.AgentID)
		}
	}

	return true
}

func drainRegularSnapshots(
	regularSnapshots <-chan AgentSnapshot,
	applyAgentSnapshot func(snapshot AgentSnapshot) error,
) {
	for {
		select {
		case snapshot := <-regularSnapshots:
			if snapshot.AgentID == "" {
				continue
			}
			_ = applyAgentSnapshot(snapshot)
		default:
			return
		}
	}
}
