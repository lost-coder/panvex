package server

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// startMetricsPoller runs a loop that refreshes gauges derived from live
// in-memory state (agent connection count, event-hub subscribers, job queue
// depth, lockout count). Counters and histograms are pushed at observation
// time elsewhere and therefore do not need polling.
//
// A 5-second interval keeps the scrape-time value reasonably fresh without
// adding noticeable load; scrape intervals in production are typically 15s+.
func (s *Server) startMetricsPoller(ctx context.Context, interval time.Duration) {
	if s.obs == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}

	s.metricsPollerWG.Add(1)
	go func() {
		defer s.metricsPollerWG.Done()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		// Refresh once immediately so the first scrape after startup has
		// non-zero values (otherwise a test that scrapes right after New()
		// sees only zeros and cannot tell polling is wired).
		s.refreshPolledMetrics(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshPolledMetrics(ctx)
			}
		}
	}()
}

// poolStatsProvider is implemented by storage backends that expose
// their database/sql pool counters. Both postgres.Store and
// sqlite.Store satisfy it; tx-bound stores return zero values.
type poolStatsProvider interface {
	PoolStats() sql.DBStats
}

// refreshPolledMetrics samples in-memory state and updates the corresponding
// Prometheus gauges. Kept intentionally lock-light: reads use the same RLocks
// as the HTTP handlers.
func (s *Server) refreshPolledMetrics(ctx context.Context) {
	if s.obs == nil {
		return
	}
	// EvaluateAll sweeps every tracked agent at the current time, driving
	// presence transitions (so silent agents flip to offline) and counting
	// only non-offline agents — unlike TrackedCount, which counted stale
	// entries until deregistration (L-8).
	s.obs.AgentConnected.Set(float64(s.presence.EvaluateAll(s.now())))
	s.obs.EventHubSubscribers.Set(float64(s.events.SubscriberCount()))
	if s.jobs != nil {
		s.obs.JobQueueDepth.Set(float64(s.jobs.QueueDepth()))
	}
	if s.loginLockout != nil {
		s.obs.LockoutActive.Set(float64(s.loginLockout.ActiveCount(s.now())))
	}
	if s.authority != nil {
		s.obs.CACertExpiryTimestamp.Set(float64(s.authority.certificate.NotAfter.Unix()))
		if na := s.authority.serverCertNotAfter(); !na.IsZero() {
			s.obs.ServerCertExpiryTimestamp.Set(float64(na.Unix()))
		}
	}
	s.refreshAgentCertExpiry(ctx)
	s.refreshPoolMetrics()
}

// refreshAgentCertExpiry samples the earliest agent certificate expiry.
// Unlike the rest of refreshPolledMetrics this touches the store, so it
// is throttled to once per minute (the poller ticks every 5s). Only
// called from the single poller goroutine — no locking on the
// timestamp field. P6-6.3f: a single MIN query replaced the previous
// full ListAgents scan (O(fleet) rows over the wire per refresh).
func (s *Server) refreshAgentCertExpiry(ctx context.Context) {
	if s.obs == nil || s.store == nil {
		return
	}
	now := s.now()
	if !s.agentCertExpiryRefreshedAt.IsZero() && now.Sub(s.agentCertExpiryRefreshedAt) < time.Minute {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	earliest, err := s.store.EarliestAgentCertExpiry(ctx)
	if err != nil {
		// Don't advance the throttle stamp on a transient store error —
		// retry on the next poller tick instead of blacking out the gauge
		// for a full minute.
		slog.Warn("metrics: earliest agent cert expiry query failed", "err", err)
		return
	}
	s.agentCertExpiryRefreshedAt = now
	if earliest == nil {
		s.obs.AgentCertEarliestExpiryTimestamp.Set(0)
		return
	}
	s.obs.AgentCertEarliestExpiryTimestamp.Set(float64(earliest.Unix()))
}

// refreshPoolMetrics snapshots the storage backend's connection-pool
// stats. Gauges (Open/InUse/Idle/MaxOpen) get Set; cumulative counters
// (Wait/MaxIdleClosed/LifetimeClosed) get Add'd by the delta against
// prevPoolStats so Prometheus sees a monotonically increasing series
// from a fresh per-process zero.
func (s *Server) refreshPoolMetrics() {
	provider, ok := s.store.(poolStatsProvider)
	if !ok {
		return
	}
	curr := provider.PoolStats()
	s.obs.ObservePoolGauges(curr)
	s.poolStatsMu.Lock()
	prev := s.prevPoolStats
	s.prevPoolStats = curr
	s.poolStatsMu.Unlock()
	s.obs.AddPoolCounterDeltas(prev, curr)
}

// metricsShutdown stops the metrics polling goroutine, if any. It is safe to
// call multiple times and when no poller was started (token empty).
func (s *Server) metricsShutdown() {
	if s.metricsPollerCancel != nil {
		s.metricsPollerCancel()
	}
	s.metricsPollerWG.Wait()
}
