package server

import (
	"context"
	"time"
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
	regularSnapshots chan agentSnapshot,
	snapshot agentSnapshot,
) bool {
	if connectionCtx.Err() != nil {
		return false
	}

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
	}

	return true
}

func drainRegularSnapshots(
	regularSnapshots <-chan agentSnapshot,
	applyAgentSnapshot func(snapshot agentSnapshot) error,
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
