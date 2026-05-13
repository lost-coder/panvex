package server

import (
	"context"
	"log/slog"
	"time"
)

// enrollmentCleanupTickInterval is the cadence at which the retention
// worker wakes up to prune attempts older than the configured
// retention. Hourly is far more frequent than the retention window
// (default 30 days), giving operators a near-realtime view of the
// table size without burning meaningful CPU on the DELETE.
const enrollmentCleanupTickInterval = time.Hour

// runEnrollmentCleanupOnce deletes attempts whose started_at is
// strictly before now - retain. Returns the count of attempts removed
// (events cascade via the schema's ON DELETE CASCADE) so callers can
// surface the deletion in their own logs. Returns (0, nil) when the
// recorder is not wired (test fixtures without DB() — see
// initStoreBackedSubsystems).
func (s *Server) runEnrollmentCleanupOnce(ctx context.Context, retain time.Duration) (int64, error) {
	if s.enrollmentRec == nil {
		return 0, nil
	}
	cutoff := s.now().Add(-retain)
	return s.enrollmentRec.DeleteOlderThan(ctx, cutoff)
}

// startEnrollmentCleanupWorker spawns the periodic retention loop on
// s.serverCtx. The loop is a no-op when retain ≤ 0 so operators can
// disable retention by setting PANVEX_ENROLLMENT_RETENTION_DAYS=0 at
// boot. Errors from the cleanup are logged at WARN and the loop
// continues — a transient DB error must not stop future ticks.
func (s *Server) startEnrollmentCleanupWorker(ctx context.Context, retain time.Duration) {
	if retain <= 0 {
		return
	}
	ticker := time.NewTicker(enrollmentCleanupTickInterval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				n, err := s.runEnrollmentCleanupOnce(ctx, retain)
				if err != nil {
					slog.WarnContext(ctx, "enrollment cleanup failed", "error", err)
					continue
				}
				if n > 0 {
					slog.InfoContext(ctx, "enrollment cleanup", "deleted", n, "retain", retain)
				}
			}
		}
	}()
}
