package server

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// RetentionSettings controls how long timeseries data is kept before pruning.
type RetentionSettings struct {
	TSRawSeconds          int `json:"ts_raw_seconds"`
	TSHourlySeconds       int `json:"ts_hourly_seconds"`
	TSDCSeconds           int `json:"ts_dc_seconds"`
	IPHistorySeconds      int `json:"ip_history_seconds"`
	EventSeconds          int `json:"event_history_seconds"`
	AuditEventSeconds     int `json:"audit_event_seconds"`
	MetricSnapshotSeconds int `json:"metric_snapshot_seconds"`
	// JobsSeconds bounds how long terminal jobs (succeeded/failed/
	// expired) live in the jobs table before the rollup loop deletes
	// them via PruneTerminalJobs (Q2.U-P-02).
	JobsSeconds int `json:"jobs_seconds"`
	// WebhookOutboxSeconds bounds how long terminal webhook_outbox rows
	// (delivered or dead) are kept for operator audit before the rollup
	// loop prunes them via webhooks.Storage.PruneOutbox (C4).
	WebhookOutboxSeconds int `json:"webhook_outbox_seconds"`
	// EnrollmentTokenSeconds bounds how long dead enrollment tokens
	// (consumed, revoked, or expired-unconsumed) are kept for operator
	// forensics before the rollup loop prunes them via
	// PruneEnrollmentTokens (C4).
	EnrollmentTokenSeconds int `json:"enrollment_token_seconds"`
}

func defaultRetentionSettings() RetentionSettings {
	return RetentionSettings{
		TSRawSeconds:          86400,   // 24h
		TSHourlySeconds:       604800,  // 7d
		TSDCSeconds:           86400,   // 24h
		IPHistorySeconds:      2592000, // 30d
		EventSeconds:          86400,   // 24h
		AuditEventSeconds:     7776000, // 90d (P2-REL-04 / finding M-R2)
		MetricSnapshotSeconds: 2592000, // 30d (P2-REL-05)
		JobsSeconds:           2592000, // 30d (Q2.U-P-02)
		WebhookOutboxSeconds:   2592000, // 30d
		EnrollmentTokenSeconds: 2592000, // 30d
	}
}

// retentionSettingsToRecord converts the server-side RetentionSettings to the
// storage-layer record. Field layout is identical so this is a straight copy;
// the helper exists so callers do not depend on the alias in storage/store.go.
func retentionSettingsToRecord(settings RetentionSettings) storage.RetentionSettings {
	return storage.RetentionSettings{
		TSRawSeconds:           settings.TSRawSeconds,
		TSHourlySeconds:        settings.TSHourlySeconds,
		TSDCSeconds:            settings.TSDCSeconds,
		IPHistorySeconds:       settings.IPHistorySeconds,
		EventSeconds:           settings.EventSeconds,
		AuditEventSeconds:      settings.AuditEventSeconds,
		MetricSnapshotSeconds:  settings.MetricSnapshotSeconds,
		JobsSeconds:            settings.JobsSeconds,
		WebhookOutboxSeconds:   settings.WebhookOutboxSeconds,
		EnrollmentTokenSeconds: settings.EnrollmentTokenSeconds,
	}
}

// retentionSettingsFromRecord is the inverse of retentionSettingsToRecord.
func retentionSettingsFromRecord(record storage.RetentionSettings) RetentionSettings {
	return RetentionSettings{
		TSRawSeconds:           record.TSRawSeconds,
		TSHourlySeconds:        record.TSHourlySeconds,
		TSDCSeconds:            record.TSDCSeconds,
		IPHistorySeconds:       record.IPHistorySeconds,
		EventSeconds:           record.EventSeconds,
		AuditEventSeconds:      record.AuditEventSeconds,
		MetricSnapshotSeconds:  record.MetricSnapshotSeconds,
		JobsSeconds:            record.JobsSeconds,
		WebhookOutboxSeconds:   record.WebhookOutboxSeconds,
		EnrollmentTokenSeconds: record.EnrollmentTokenSeconds,
	}
}

// restoreRetentionSettings loads persisted retention settings from the store.
// Missing / never-written rows are reported as ErrNotFound by the storage
// layer and cause the caller to keep the pre-assigned defaults.
func (s *Server) restoreRetentionSettings() error {
	if s.store == nil {
		return nil
	}
	// ctx is the boot-time lifecycle context (s.serverCtx) so a Close()
	// during a slow GetRetentionSettings storage call aborts the read
	// instead of holding the constructor open (Plan 3 / BP-01).
	record, err := s.store.GetRetentionSettings(s.Context())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		return err
	}
	s.retention = normalizeRetentionSettings(retentionSettingsFromRecord(record))
	return nil
}

func (s *Server) startTimeseriesRollupWorker(ctx context.Context, interval time.Duration) {
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
				start := time.Now()
				s.runTimeseriesRollup(ctx)
				elapsed := time.Since(start)
				// Per-tick lap log (P2-LOG-10 / L-10). The constituent
				// rollup/prune helpers each log their own row counts at
				// Info when they touch rows and at Error on failure; this
				// outer log captures cadence + lap latency at Debug so
				// production stays quiet but enables troubleshooting a
				// slow tick without code changes.
				slog.DebugContext(ctx, "rollup worker tick ok",
					"worker", "timeseries",
					"lap_ms", elapsed.Milliseconds())
			}
		}
	}()
}

func (s *Server) runTimeseriesRollup(ctx context.Context) {
	now := s.now().UTC()
	retention := s.retentionSettings()

	// 1. Hourly rollup: process all completed hours in the raw data window.
	// Roll up the previous 2 hours to catch any late-arriving data.
	s.rollupRecentHours(ctx, now)

	// 2-6. Prune raw timeseries / IP history / runtime events.
	s.runInlineRetentionPrune(ctx, "ts_server_load", "raw server load points", now, retention.TSRawSeconds, s.store.PruneServerLoadPoints)
	s.runInlineRetentionPrune(ctx, "ts_dc_health", "DC health points", now, retention.TSDCSeconds, s.store.PruneDCHealthPoints)
	s.runInlineRetentionPrune(ctx, "ts_server_load_hourly", "hourly rollup points", now, retention.TSHourlySeconds, s.store.PruneServerLoadHourly)
	s.runInlineRetentionPrune(ctx, "client_ip_history", "client IP history entries", now, retention.IPHistorySeconds, s.store.PruneClientIPHistory)
	s.runInlineRetentionPrune(ctx, "telemt_runtime_events", "telemt runtime events", now, retention.EventSeconds, s.store.PruneTelemetryRuntimeEvents)

	// 7. Prune audit events (P2-REL-04 / finding M-R2). Previously
	// audit_events grew unbounded; now it honours AuditEventSeconds.
	s.runRetentionPrune(ctx, "audit_events", now, retention.AuditEventSeconds, s.store.PruneAuditEvents)

	// 8. Prune metric snapshots (P2-REL-05). metric_snapshots also grew
	// unbounded prior to this worker being wired in.
	s.runRetentionPrune(ctx, "metric_snapshots", now, retention.MetricSnapshotSeconds, s.store.PruneMetricSnapshots)

	// 9. Prune terminal jobs (Q2.U-P-02). Active (queued/running)
	// targets are preserved so an in-flight rollout cannot be deleted
	// mid-flight.
	s.runRetentionPrune(ctx, "jobs", now, retention.JobsSeconds, s.store.PruneTerminalJobs)

	// 10. Prune terminal webhook outbox rows (C4). The webhook outbox is
	// a separate storage subsystem (webhooks.Storage), wired only when
	// the serve path supplies a WebhookStorageFactory — hence the guard.
	if s.webhookStorage != nil {
		s.runRetentionPrune(ctx, "webhook_outbox", now, retention.WebhookOutboxSeconds, s.webhookStorage.PruneOutbox)
	}

	// 11. Drop revocation rows whose certificate already expired (C4):
	// the mTLS window they guard is closed, the row is pure dead weight.
	// Cutoff is `now` by definition — no retention knob needed. The
	// in-memory revokedAgentIDs set keeps its entries until restart,
	// which is safe (it only over-rejects, never under-rejects).
	if pruned, err := s.store.DeleteExpiredAgentRevocations(ctx, now); err != nil {
		s.logger.ErrorContext(ctx, "retention prune failed", "table", "agent_revocations", "error", err)
	} else if pruned > 0 {
		s.logger.InfoContext(ctx, "pruned rows by retention", "table", "agent_revocations", "count", pruned)
		if s.obs != nil {
			s.obs.retentionPrunedRowsTotal.WithLabelValues("agent_revocations").Add(float64(pruned))
		}
	}

	// 12. Prune dead enrollment tokens (C4): consumed/revoked/expired
	// rows are kept EnrollmentTokenSeconds for operator forensics, then
	// dropped.
	s.runRetentionPrune(ctx, "enrollment_tokens", now, retention.EnrollmentTokenSeconds, s.store.PruneEnrollmentTokens)
}

// rollupRecentHours rebuilds hourly aggregates for the previous 2 hours so
// late-arriving raw points are still folded in.
func (s *Server) rollupRecentHours(ctx context.Context, now time.Time) {
	for hoursAgo := 2; hoursAgo >= 1; hoursAgo-- {
		bucketHour := now.Add(-time.Duration(hoursAgo) * time.Hour).Truncate(time.Hour)
		if err := s.store.RollupServerLoadHourly(ctx, bucketHour); err != nil {
			s.logger.ErrorContext(ctx, "timeseries rollup failed", "bucket_hour", bucketHour.Format(time.RFC3339), "error", err)
		}
	}
}

// warnRetentionDisabled logs a loud, one-time (per table, until re-enabled)
// warning that retention pruning is disabled for a series. A ttlSeconds of
// 0 (or negative) is a legitimate "keep forever" operator choice — pruning
// intentionally no-ops rather than hard-failing — but silently doing nothing
// risks unbounded disk growth for tables like metric_snapshots that an
// operator might disable without realising the consequence. The warning
// fires once per table for as long as the series stays disabled; it resets
// (and will fire again) if the operator re-enables and later re-disables
// retention, so a fresh disable is never missed after a restart or a
// settings round-trip.
func (s *Server) warnRetentionDisabled(ctx context.Context, table string) {
	s.retentionWarnMu.Lock()
	alreadyWarned := s.retentionDisabledWarned[table]
	if !alreadyWarned {
		s.retentionDisabledWarned[table] = true
	}
	s.retentionWarnMu.Unlock()
	if alreadyWarned {
		return
	}
	s.logger.WarnContext(ctx, "retention disabled for series; table will grow unbounded",
		"table", table,
		"reason", "ttl_seconds<=0",
		"hint", "set a positive retention TTL to bound disk growth; this is a one-time warning per disable")
}

// clearRetentionDisabledWarning drops the "already warned" marker for table
// so a future disable (after the operator re-enables retention) logs a fresh
// warning instead of staying silent forever.
func (s *Server) clearRetentionDisabledWarning(table string) {
	s.retentionWarnMu.Lock()
	delete(s.retentionDisabledWarned, table)
	s.retentionWarnMu.Unlock()
}

// runInlineRetentionPrune mirrors runRetentionPrune for tables that have
// table-specific log messages. It runs the prune only when ttlSeconds > 0,
// logs a per-table error on failure, and a row count on success.
func (s *Server) runInlineRetentionPrune(
	ctx context.Context,
	table, label string,
	now time.Time,
	ttlSeconds int,
	pruneFn func(context.Context, time.Time) (int64, error),
) {
	if ttlSeconds <= 0 {
		s.warnRetentionDisabled(ctx, table)
		return
	}
	s.clearRetentionDisabledWarning(table)
	cutoff := now.Add(-time.Duration(ttlSeconds) * time.Second)
	pruned, err := pruneFn(ctx, cutoff)
	if err != nil {
		s.logger.ErrorContext(ctx, "prune "+table+" failed", "error", err)
		return
	}
	if pruned > 0 {
		s.logger.InfoContext(ctx, "pruned "+label, "count", pruned, "cutoff", cutoff.Format(time.RFC3339))
	}
}

// runRetentionPrune is the shared helper used by audit_events and
// metric_snapshots pruning. It applies the same skip-on-disabled /
// log-on-success shape as the timeseries pruners above and, on success,
// pushes the row count into panvex_retention_pruned_rows_total so Grafana
// can alert when retention stops trimming (nothing to delete) or when it
// trims catastrophically large batches.
func (s *Server) runRetentionPrune(
	ctx context.Context,
	table string,
	now time.Time,
	ttlSeconds int,
	pruneFn func(ctx context.Context, before time.Time) (int64, error),
) {
	if ttlSeconds <= 0 {
		s.warnRetentionDisabled(ctx, table)
		return
	}
	s.clearRetentionDisabledWarning(table)
	cutoff := now.Add(-time.Duration(ttlSeconds) * time.Second)
	pruned, err := pruneFn(ctx, cutoff)
	if err != nil {
		s.logger.ErrorContext(ctx, "retention prune failed", "table", table, "error", err)
		return
	}
	if pruned > 0 {
		s.logger.InfoContext(ctx, "pruned rows by retention", "table", table, "count", pruned, "cutoff", cutoff.Format(time.RFC3339))
	}
	if s.obs != nil {
		s.obs.retentionPrunedRowsTotal.WithLabelValues(table).Add(float64(pruned))
	}
}

func (s *Server) retentionSettings() RetentionSettings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	return s.retention
}
