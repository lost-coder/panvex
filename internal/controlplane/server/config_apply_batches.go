package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

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
			s.markConfigApplyBatchFailed(ctx, batchID, fleetGroupID)
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
			s.markConfigApplyBatchFailed(ctx, batchID, fleetGroupID)
			return batchID, fmt.Errorf("record job for %s on batch %s: %w", agentID, batchID, err)
		}
	}
	return batchID, nil
}

// markConfigApplyBatchFailed transitions a batch to
// ConfigApplyBatchStatusFailed after a mid-loop enqueue/persist error in the
// fan-out loop above. Without this, a batch that fails partway through stays
// "running" forever: targets after the failure point are never touched (no
// job id, still pending) and nothing else self-terminates the batch, so the
// operator/UI sees what looks like an actively in-progress rollout
// indefinitely. Already-enqueued jobs are NOT rolled back — those agents
// legitimately received the config; this only marks the group-apply as not
// having fully dispatched.
//
// Uses context.WithoutCancel(ctx) for the status update because the incoming
// ctx may already be cancelled/expired on the error path (e.g. request
// timeout, client disconnect during the fan-out) — the status write must
// still land, mirroring the shutdown-drain convention documented in
// CLAUDE.md's Context Propagation section (see lifecycle.go: Close).
func (s *Server) markConfigApplyBatchFailed(ctx context.Context, batchID, fleetGroupID string) {
	updateCtx := context.WithoutCancel(ctx)
	if err := s.store.UpdateConfigApplyBatchStatus(updateCtx, batchID, storage.ConfigApplyBatchStatusFailed, s.now()); err != nil {
		slog.ErrorContext(ctx, "config-apply batch: marking batch failed after mid-loop error also failed",
			"batch_id", batchID, "fleet_group_id", fleetGroupID, "error", err)
	}
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

// isTerminalConfigApplyTargetStatus reports whether a
// ConfigApplyBatchTargetRecord.Status value is terminal — the target will
// not be re-derived from its job on a later poll. Skipped is included even
// though Phase A never produces it (no rolling wave-halt logic yet): once
// Phase B introduces halt-on-failure, halted waves will mark not-yet-run
// targets skipped, and treating it as non-terminal here would let the
// orchestrator spin on a target it will never see move.
func isTerminalConfigApplyTargetStatus(status string) bool {
	switch status {
	case storage.ConfigApplyTargetStatusSucceeded,
		storage.ConfigApplyTargetStatusFailed,
		storage.ConfigApplyTargetStatusSkipped:
		return true
	default:
		return false
	}
}

// advanceConfigApplyBatch re-derives each non-terminal target's status (and
// message) from its config.apply job (via the shared configApplyJobStatus
// helper — see http_config_apply.go) and persists any change. Persisting the
// message here — not just the status — is what lets the batch-status
// endpoints (handleGetGroupApplyBatchStatus) surface a failure reason after
// the underlying job has rolled off the in-memory jobs store: the target row
// becomes the durable record once it goes terminal. Once every target in the
// batch's (only, for Phase A) wave is terminal, the batch itself is
// finalized: succeeded if no target failed, failed otherwise.
//
// The applyStatus* constants (pending/running/succeeded/failed) share their
// string values 1:1 with the storage.ConfigApplyTargetStatus* constants
// (pending/running/succeeded/failed), so configApplyJobStatus's return value
// is used directly as the persisted target status — no separate mapping
// table to keep in sync.
//
// Idempotent: a batch that is already terminal (not Running) is a no-op, and
// a target whose stored status AND message already match the job-derived
// values is not re-written. This makes the function safe to call repeatedly
// from the polling worker (startConfigApplyBatchWorker) without
// double-transitioning an already-finalized batch, and safe to resume after
// a panel restart — ListRunningConfigApplyBatches only ever hands back
// batches that still need advancing.
//
// Phase B (rolling, multi-wave) will need to additionally decide whether to
// enqueue the next wave here; Phase A's all_at_once mode has exactly one
// wave, so "all targets terminal" always means "batch done".
func (s *Server) advanceConfigApplyBatch(ctx context.Context, batch storage.ConfigApplyBatchRecord) error {
	if batch.Status != storage.ConfigApplyBatchStatusRunning {
		return nil
	}
	_, targets, err := s.store.GetConfigApplyBatch(ctx, batch.ID)
	if err != nil {
		return fmt.Errorf("get config-apply batch %s: %w", batch.ID, err)
	}

	allTerminal := true
	anyFailed := false
	for _, tgt := range targets {
		status := tgt.Status
		if !isTerminalConfigApplyTargetStatus(status) {
			derived, message := s.configApplyJobStatus(tgt.JobID, tgt.AgentID)
			if derived != status || message != tgt.Message {
				if err := s.store.UpdateConfigApplyBatchTargetStatus(ctx, batch.ID, tgt.AgentID, derived, message); err != nil {
					return fmt.Errorf("update config-apply batch %s target %s status: %w", batch.ID, tgt.AgentID, err)
				}
				status = derived
			}
		}
		if !isTerminalConfigApplyTargetStatus(status) {
			allTerminal = false
			continue
		}
		if status == storage.ConfigApplyTargetStatusFailed {
			anyFailed = true
		}
	}

	if !allTerminal {
		return nil
	}
	finalStatus := storage.ConfigApplyBatchStatusSucceeded
	if anyFailed {
		finalStatus = storage.ConfigApplyBatchStatusFailed
	}
	if err := s.store.UpdateConfigApplyBatchStatus(ctx, batch.ID, finalStatus, s.now()); err != nil {
		return fmt.Errorf("finalize config-apply batch %s as %s: %w", batch.ID, finalStatus, err)
	}
	return nil
}

// startConfigApplyBatchWorker polls ListRunningConfigApplyBatches on a fixed
// interval and calls advanceConfigApplyBatch on each. There is no
// terminal-notification/wake mechanism in the jobs engine to drive this
// event-style (jobs.Service exposes only a failure-only bare-func hook via
// SetJobFailureHook), so polling is the only option — this mirrors
// startTimeseriesRollupWorker's shape (ctx-derived from rollupCtx, joined via
// s.rollupWg, ticker + select).
//
// Polling ListRunningConfigApplyBatches on every tick (rather than tracking
// batches created this process lifetime) is what makes a panel restart
// resilient: any batch still "running" in the store — including one left
// running by a prior process that crashed or was killed mid-rollout — is
// picked up and advanced on the very first tick after startup.
func (s *Server) startConfigApplyBatchWorker(ctx context.Context, interval time.Duration) {
	if s.store == nil {
		return
	}

	s.rollupWg.Add(1)
	go func() {
		defer s.rollupWg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.pollConfigApplyBatches(ctx)
			}
		}
	}()
}

// pollConfigApplyBatches is the per-tick body of startConfigApplyBatchWorker,
// split out so a per-batch failure is logged and the loop moves on to the
// next batch rather than the whole tick — and, incidentally, so it is
// trivially unit-testable without a ticker.
func (s *Server) pollConfigApplyBatches(ctx context.Context) {
	batches, err := s.store.ListRunningConfigApplyBatches(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "config-apply batch worker: list running batches failed", "error", err)
		return
	}
	for _, batch := range batches {
		if err := s.advanceConfigApplyBatch(ctx, batch); err != nil {
			slog.ErrorContext(ctx, "config-apply batch worker: advance failed",
				"batch_id", batch.ID, "fleet_group_id", batch.FleetGroupID, "error", err)
		}
	}
}
