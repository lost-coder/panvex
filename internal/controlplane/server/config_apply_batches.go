package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// createGroupApplyBatch persists a config_apply_batches row plus one target
// per agent, then enqueues each target's config.apply job and records the
// resulting job id via SetConfigApplyBatchTargetJob. It is the persistence
// counterpart to the ASYNC group-apply fan-out: handleApplyGroupConfig calls
// this instead of enqueueing jobs directly, so an in-flight (or completed)
// rollout survives a panel restart and can be inspected independently of the
// jobs store's TTL/eviction.
//
// Phase A only implements the all_at_once mode: every agent lands in wave 0,
// enqueued in the same call. Rolling (multi-wave, halt-on-failure) delivery
// is Phase B — mode/waveSize are threaded through now so that handler and
// storage plumbing do not need to change shape again when it lands.
//
// An empty agentIDs list means the group has no in-scope agents; no batch is
// created and ("", nil) is returned, matching the pre-batch behavior where an
// empty group produced no jobs.
//
// Enqueueing is NOT rolled back on a partial failure: targets already
// persisted (and any job already enqueued) stay as-is, mirroring the
// pre-existing concurrent-fan-out semantics documented on
// handleApplyGroupConfig — the operator sees each agent's own outcome via the
// batch/status views rather than the whole request failing atomically.
func (s *Server) createGroupApplyBatch(ctx context.Context, actorID, fleetGroupID, mode string, waveSize int, agentIDs []string) (string, error) {
	if len(agentIDs) == 0 {
		return "", nil
	}
	batchID := newConfigApplyBatchID()
	now := s.now()
	targets := make([]storage.ConfigApplyBatchTargetRecord, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		targets = append(targets, storage.ConfigApplyBatchTargetRecord{
			BatchID:   batchID,
			AgentID:   agentID,
			WaveIndex: 0,
			Status:    storage.ConfigApplyTargetStatusPending,
		})
	}
	batch := storage.ConfigApplyBatchRecord{
		ID:           batchID,
		FleetGroupID: fleetGroupID,
		Mode:         mode,
		WaveSize:     waveSize,
		Status:       storage.ConfigApplyBatchStatusRunning,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.store.CreateConfigApplyBatch(ctx, batch, targets); err != nil {
		return "", fmt.Errorf("create config-apply batch: %w", err)
	}

	for _, agentID := range agentIDs {
		jobID, err := s.enqueueConfigApplyJob(ctx, actorID, agentID)
		if err != nil {
			slog.ErrorContext(ctx, "config-apply batch: enqueue failed",
				"batch_id", batchID, "fleet_group_id", fleetGroupID, "agent_id", agentID, "error", err)
			return batchID, fmt.Errorf("enqueue config.apply for %s: %w", agentID, err)
		}
		// An empty job id means the agent's effective config was already
		// empty (no-op) — the target is done, not merely dispatched.
		status := storage.ConfigApplyTargetStatusRunning
		if jobID == "" {
			status = storage.ConfigApplyTargetStatusSucceeded
		}
		if err := s.store.SetConfigApplyBatchTargetJob(ctx, batchID, agentID, jobID, status); err != nil {
			slog.ErrorContext(ctx, "config-apply batch: record target job failed",
				"batch_id", batchID, "fleet_group_id", fleetGroupID, "agent_id", agentID, "error", err)
			return batchID, fmt.Errorf("record job for %s on batch %s: %w", agentID, batchID, err)
		}
	}
	return batchID, nil
}

// newConfigApplyBatchID mints a time-ordered UUIDv7 for a config_apply_batches
// row's primary key, matching the id-generation convention used elsewhere in
// this package (see newRequestID).
func newConfigApplyBatchID() string {
	v, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 only fails when crypto/rand fails — extremely rare.
		return uuid.NewString()
	}
	return v.String()
}
