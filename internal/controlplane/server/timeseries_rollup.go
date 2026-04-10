package server

import (
	"context"
	"time"
)

// RetentionSettings controls how long timeseries data is kept before pruning.
type RetentionSettings struct {
	TSRawSeconds     int `json:"ts_raw_seconds"`
	TSHourlySeconds  int `json:"ts_hourly_seconds"`
	TSDCSeconds      int `json:"ts_dc_seconds"`
	IPHistorySeconds int `json:"ip_history_seconds"`
	EventSeconds     int `json:"event_history_seconds"`
}

func defaultRetentionSettings() RetentionSettings {
	return RetentionSettings{
		TSRawSeconds:     86400,   // 24h
		TSHourlySeconds:  604800,  // 7d
		TSDCSeconds:      86400,   // 24h
		IPHistorySeconds: 2592000, // 30d
		EventSeconds:     86400,   // 24h
	}
}

const rollupInterval = 5 * time.Minute

func (s *Server) startTimeseriesRollupWorker(ctx context.Context) {
	if s.store == nil {
		return
	}

	go func() {
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
}

func (s *Server) retentionSettings() RetentionSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.retention
}
