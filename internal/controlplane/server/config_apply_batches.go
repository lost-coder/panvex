package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// createConfigApplyBatch persists a config_apply_batches row plus one target
// per agent, then enqueues each target's config.apply job and records the
// resulting job id via SetConfigApplyBatchTargetJob. It is the persistence
// counterpart to the ASYNC apply fan-out: handleApplyGroupConfig and the
// single-agent handleApplyAgentConfig call this instead of enqueueing jobs
// directly, so an in-flight (or completed) rollout survives a panel restart
// and can be inspected independently of the jobs store's TTL/eviction.
//
// fleetGroupID is the empty string for an agent-scoped batch-of-one (P3-3.4,
// single-agent apply): such a batch carries NULL fleet_group_id and never
// surfaces as a group's active batch. The group fan-out passes the group id.
//
// Phase A only implements the all_at_once mode: every agent lands in wave 0,
// enqueued in the same call. Rolling (multi-wave, halt-on-failure) delivery
// is Phase B — when it lands, mode/waveSize become parameters again; the DB
// columns and record fields already carry them, so only this function's
// signature changes (P5, audit #19).
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
func (s *Server) createConfigApplyBatch(ctx context.Context, actorID, fleetGroupID string, agentIDs []string) (string, error) {
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
		Mode:         storage.ConfigApplyBatchModeAllAtOnce,
		WaveSize:     1,
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

// advanceConfigApplyBatch finalizes a running batch once every target's
// config.apply job has reached a terminal state, and does NOTHING before
// that moment.
//
// P8.1 (audit #24): the pre-P8.1 orchestrator re-derived and re-persisted
// every non-terminal target's status on each 500ms tick — a status-copying
// state machine that existed only because jobs used to be lost on in-memory
// eviction. Jobs are durable now (jobs.Service.GetWithContext reads evicted
// jobs back from the jobs table), so live views derive target statuses
// directly (aggregateGroupApplyBatchStatus) and this function's only job is
// the single durable hand-off at the end: write each target's terminal
// status + message (the batch rows outlive the job rows —
// retention.ConfigApplyBatchSeconds vs retention.JobsSeconds — so the
// terminal outcome MUST be copied once) and flip the batch itself to
// succeeded/failed. Skipped counts as terminal but not as failed, same as
// before (Phase B wave-halt readiness).
//
// A store read error inside configApplyJobStatus surfaces as a still-running
// derived status, which keeps the batch running for the next tick — a
// transient DB hiccup can only DELAY finalization, never mis-finalize.
//
// Idempotent: a batch that is already terminal is a no-op, and re-running on
// an already-finalized batch re-writes nothing. Safe to call repeatedly from
// the polling worker (startConfigApplyBatchWorker) and safe to resume after
// a panel restart — ListRunningConfigApplyBatches only hands back batches
// that still need finalizing.
func (s *Server) advanceConfigApplyBatch(ctx context.Context, batch storage.ConfigApplyBatchRecord) error {
	if batch.Status != storage.ConfigApplyBatchStatusRunning {
		return nil
	}
	_, targets, err := s.store.GetConfigApplyBatch(ctx, batch.ID)
	if err != nil {
		return fmt.Errorf("get config-apply batch %s: %w", batch.ID, err)
	}

	// Phase 1: derive, purely in memory. Bail out at the first non-terminal
	// target — nothing is persisted until the WHOLE batch is done.
	type derivedTarget struct {
		agentID string
		status  string
		message string
	}
	pending := make([]derivedTarget, 0, len(targets))
	anyFailed := false
	for _, tgt := range targets {
		status, message := tgt.Status, tgt.Message
		if !isTerminalConfigApplyTargetStatus(status) {
			status, message = s.configApplyJobStatus(ctx, tgt.JobID, tgt.AgentID)
		}
		if !isTerminalConfigApplyTargetStatus(status) {
			return nil // still in flight — nothing to persist yet
		}
		if status == storage.ConfigApplyTargetStatusFailed {
			anyFailed = true
		}
		if status != tgt.Status || message != tgt.Message {
			pending = append(pending, derivedTarget{agentID: tgt.AgentID, status: status, message: message})
		}
	}

	// Phase 2: persist — targets first, then the batch flip. If a write
	// fails midway the batch stays running and the next tick retries; the
	// applyStatus* constants share their string values 1:1 with the
	// storage.ConfigApplyTargetStatus* constants, so the derived status is
	// persisted as-is (no mapping table to keep in sync).
	for _, d := range pending {
		if err := s.store.UpdateConfigApplyBatchTargetStatus(ctx, batch.ID, d.agentID, d.status, d.message); err != nil {
			return fmt.Errorf("update config-apply batch %s target %s status: %w", batch.ID, d.agentID, err)
		}
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
// event-style (jobs.Service only surfaces a failed-job counter via
// MetricsSink.ObserveJobFailed), so polling is the only option — this mirrors
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
