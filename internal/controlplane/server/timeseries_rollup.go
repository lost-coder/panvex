package server

import (
	"context"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// RetentionSettings controls how long timeseries data is kept before pruning.
type RetentionSettings struct {
	TSRawSeconds           int `json:"ts_raw_seconds"`
	TSHourlySeconds        int `json:"ts_hourly_seconds"`
	TSDCSeconds            int `json:"ts_dc_seconds"`
	IPHistorySeconds       int `json:"ip_history_seconds"`
	EventSeconds           int `json:"event_history_seconds"`
	AuditEventSeconds      int `json:"audit_event_seconds"`
	MetricSnapshotSeconds  int `json:"metric_snapshot_seconds"`
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
	}
}

// retentionSettingsToRecord converts the server-side RetentionSettings to the
// storage-layer record. Field layout is identical so this is a straight copy;
// the helper exists so callers do not depend on the alias in storage/store.go.
func retentionSettingsToRecord(settings RetentionSettings) storage.RetentionSettings {
	return storage.RetentionSettings{
		TSRawSeconds:          settings.TSRawSeconds,
		TSHourlySeconds:       settings.TSHourlySeconds,
		TSDCSeconds:           settings.TSDCSeconds,
		IPHistorySeconds:      settings.IPHistorySeconds,
		EventSeconds:          settings.EventSeconds,
		AuditEventSeconds:     settings.AuditEventSeconds,
		MetricSnapshotSeconds: settings.MetricSnapshotSeconds,
	}
}

// retentionSettingsFromRecord is the inverse of retentionSettingsToRecord.
func retentionSettingsFromRecord(record storage.RetentionSettings) RetentionSettings {
	return RetentionSettings{
		TSRawSeconds:          record.TSRawSeconds,
		TSHourlySeconds:       record.TSHourlySeconds,
		TSDCSeconds:           record.TSDCSeconds,
		IPHistorySeconds:      record.IPHistorySeconds,
		EventSeconds:          record.EventSeconds,
		AuditEventSeconds:     record.AuditEventSeconds,
		MetricSnapshotSeconds: record.MetricSnapshotSeconds,
	}
}

// restoreRetentionSettings loads persisted retention settings from the store.
// Missing / never-written rows are reported as ErrNotFound by the storage
// layer and cause the caller to keep the pre-assigned defaults.
func (s *Server) restoreRetentionSettings() error {
	if s.store == nil {
		return nil
	}
	record, err := s.store.GetRetentionSettings(context.Background())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		return err
	}
	s.retention = normalizeRetentionSettings(retentionSettingsFromRecord(record))
	return nil
}

const rollupInterval = 5 * time.Minute

func (s *Server) startTimeseriesRollupWorker(ctx context.Context) {
	if s.store == nil {
		return
	}

	s.rollupWg.Add(1)
	go func() {
		defer s.rollupWg.Done()
		ticker := time.NewTicker(rollupInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runTimeseriesRollup(ctx)
			}
		}
	}()
}

func (s *Server) runTimeseriesRollup(ctx context.Context) {
	now := s.now().UTC()
	retention := s.retentionSettings()

	// 1. Hourly rollup: process all completed hours in the raw data window.
	// Roll up the previous 2 hours to catch any late-arriving data.
	for hoursAgo := 2; hoursAgo >= 1; hoursAgo-- {
		bucketHour := now.Add(-time.Duration(hoursAgo) * time.Hour).Truncate(time.Hour)
		if err := s.store.RollupServerLoadHourly(ctx, bucketHour); err != nil {
			s.logger.Error("timeseries rollup failed", "bucket_hour", bucketHour.Format(time.RFC3339), "error", err)
		}
	}

	// 2. Prune raw server load.
	if retention.TSRawSeconds > 0 {
		cutoff := now.Add(-time.Duration(retention.TSRawSeconds) * time.Second)
		if pruned, err := s.store.PruneServerLoadPoints(ctx, cutoff); err != nil {
			s.logger.Error("prune ts_server_load failed", "error", err)
		} else if pruned > 0 {
			s.logger.Info("pruned raw server load points", "count", pruned, "cutoff", cutoff.Format(time.RFC3339))
		}
	}

	// 3. Prune DC health.
	if retention.TSDCSeconds > 0 {
		cutoff := now.Add(-time.Duration(retention.TSDCSeconds) * time.Second)
		if pruned, err := s.store.PruneDCHealthPoints(ctx, cutoff); err != nil {
			s.logger.Error("prune ts_dc_health failed", "error", err)
		} else if pruned > 0 {
			s.logger.Info("pruned DC health points", "count", pruned, "cutoff", cutoff.Format(time.RFC3339))
		}
	}

	// 4. Prune hourly rollups.
	if retention.TSHourlySeconds > 0 {
		cutoff := now.Add(-time.Duration(retention.TSHourlySeconds) * time.Second)
		if pruned, err := s.store.PruneServerLoadHourly(ctx, cutoff); err != nil {
			s.logger.Error("prune ts_server_load_hourly failed", "error", err)
		} else if pruned > 0 {
			s.logger.Info("pruned hourly rollup points", "count", pruned, "cutoff", cutoff.Format(time.RFC3339))
		}
	}

	// 5. Prune client IP history.
	if retention.IPHistorySeconds > 0 {
		cutoff := now.Add(-time.Duration(retention.IPHistorySeconds) * time.Second)
		if pruned, err := s.store.PruneClientIPHistory(ctx, cutoff); err != nil {
			s.logger.Error("prune client_ip_history failed", "error", err)
		} else if pruned > 0 {
			s.logger.Info("pruned client IP history entries", "count", pruned, "cutoff", cutoff.Format(time.RFC3339))
		}
	}

	// 6. Prune telemt runtime events.
	if retention.EventSeconds > 0 {
		cutoff := now.Add(-time.Duration(retention.EventSeconds) * time.Second)
		if pruned, err := s.store.PruneTelemetryRuntimeEvents(ctx, cutoff); err != nil {
			s.logger.Error("prune telemt_runtime_events failed", "error", err)
		} else if pruned > 0 {
			s.logger.Info("pruned telemt runtime events", "count", pruned, "cutoff", cutoff.Format(time.RFC3339))
		}
	}

	// 7. Prune audit events (P2-REL-04 / finding M-R2). Previously
	// audit_events grew unbounded; now it honours AuditEventSeconds.
	s.runRetentionPrune(ctx, "audit_events", now, retention.AuditEventSeconds, s.store.PruneAuditEvents)

	// 8. Prune metric snapshots (P2-REL-05). metric_snapshots also grew
	// unbounded prior to this worker being wired in.
	s.runRetentionPrune(ctx, "metric_snapshots", now, retention.MetricSnapshotSeconds, s.store.PruneMetricSnapshots)
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
		return
	}
	cutoff := now.Add(-time.Duration(ttlSeconds) * time.Second)
	pruned, err := pruneFn(ctx, cutoff)
	if err != nil {
		s.logger.Error("retention prune failed", "table", table, "error", err)
		return
	}
	if pruned > 0 {
		s.logger.Info("pruned rows by retention", "table", table, "count", pruned, "cutoff", cutoff.Format(time.RFC3339))
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
