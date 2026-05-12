package server

import (
	"context"
	"strconv"
	"strings"
)

// restoreStoredState rehydrates the in-memory inventory from the storage
// backend at boot. Split out of server.go (R-Q-01/07) so the constructor
// stays focused on dependency wiring; the actual state-loading sequence
// lives here next to the helper that derives the next sequence number
// from a previously-persisted ID prefix.
//
// Order matters:
//
//  1. Agents — inventory baseline.
//  2. Revocations — applied AFTER the agent map is populated so a
//     restored revocation immediately gates any future stream.
//  3. Instances — leaf objects under agents.
//  4. Metrics + audit events — bounded by maxInMemoryMetricSnapshots /
//     maxInMemoryAuditEvents so the working set never blows up under a
//     long-lived store.
//  5. Telemetry — delegated to restoreStoredTelemetry, which has its own
//     ordering invariants.
//
// Errors short-circuit so the caller can fail boot loudly instead of
// running on a half-restored snapshot.
//
// ctx is the caller-supplied boot context (typically s.serverCtx) so a
// Close() arriving mid-restore — e.g. SIGINT during a slow Postgres
// startup — propagates cancellation into the DB calls instead of
// hanging on context.Background(). Restore steps are idempotent — they
// populate in-memory mirrors and at most issue idempotent cleanup
// deletes (expired-boost GC), so an aborted restore leaves no torn
// persistent state.
func (s *Server) restoreStoredState(ctx context.Context) error {
	for _, step := range []func(context.Context) error{
		s.restoreAgents,
		s.restoreAgentRevocations,
		s.restoreInstances,
		s.restoreMetrics,
		s.restoreAuditEvents,
		func(context.Context) error { return s.restoreStoredTelemetry() },
		s.restoreFallbackState,
	} {
		if err := step(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) restoreAgents(ctx context.Context) error {
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		return err
	}
	for _, record := range agents {
		agent := agentFromRecord(record)
		s.agents[agent.ID] = agent
	}
	return nil
}

// restoreAgentRevocations replays persisted agent revocations (P1-SEC-06)
// so a control-plane restart cannot silently forget a revocation while a
// deleted agent's 30-day client cert is still otherwise valid.
func (s *Server) restoreAgentRevocations(ctx context.Context) error {
	revocations, err := s.store.ListAgentRevocations(ctx)
	if err != nil {
		return err
	}
	now := s.now()
	for _, rec := range revocations {
		if rec.CertExpiresAt.Before(now) {
			// Cert is already past expiry — the TLS handshake will reject
			// it on its own, no need to carry the revocation entry.
			continue
		}
		s.revokedAgentIDs[rec.AgentID] = struct{}{}
	}
	return nil
}

func (s *Server) restoreInstances(ctx context.Context) error {
	instances, err := s.store.ListInstances(ctx)
	if err != nil {
		return err
	}
	for _, record := range instances {
		instance := instanceFromRecord(record)
		s.instances[instance.ID] = instance
	}
	return nil
}

func (s *Server) restoreMetrics(ctx context.Context) error {
	metrics, err := s.store.ListMetricSnapshots(ctx)
	if err != nil {
		return err
	}
	for _, record := range metrics {
		s.metricSeq = maxPrefixedSequence(s.metricSeq, "metric", record.ID)
	}
	// Keep only the most recent entries to avoid O(n²) copy-shift.
	if len(metrics) > maxInMemoryMetricSnapshots {
		metrics = metrics[len(metrics)-maxInMemoryMetricSnapshots:]
	}
	for _, record := range metrics {
		s.metrics = append(s.metrics, metricSnapshotFromRecord(record))
	}
	return nil
}

// restoreFallbackState rehydrates the in-memory fallback-entered-at map from
// agent_fallback_state. Failures are logged but non-fatal: the worst case is
// that the next snapshot re-enters fallback with a fresh timestamp, which
// only affects the duration-bucket boundary used by severity escalation.
//
// Hydrate failures share the same alert key as runtime flush failures
// (streamAlerts["fallback_state"] in batch_writer.go) so cold-start drift and
// runtime flush drift land in one operator-paging rule.
//
// The alert is emitted inline via slog.Error with the stable
// alert=streamAlerts["fallback_state"] attribute, intentionally bypassing the
// batch writer's flushItem path. The batch writer is for high-frequency
// background streams; this is a one-shot startup hook with no need for the
// retry/queue machinery. Operators page on the alert key, not the call path.
func (s *Server) restoreFallbackState(ctx context.Context) error {
	records, err := s.store.ListAgentFallbackState(ctx)
	if err != nil {
		s.logger.Error("hydrate fallback state failed",
			"err", err,
			"alert", streamAlerts["fallback_state"],
		)
		return nil
	}
	s.mu.Lock()
	for _, r := range records {
		s.fallbackEnteredAt[r.AgentID] = r.EnteredAt.UTC()
	}
	s.mu.Unlock()
	return nil
}

func (s *Server) restoreAuditEvents(ctx context.Context) error {
	auditEvents, err := s.store.ListAuditEvents(ctx, maxInMemoryAuditEvents)
	if err != nil {
		return err
	}
	for _, record := range auditEvents {
		s.auditSeq = maxPrefixedSequence(s.auditSeq, "audit", record.ID)
	}
	// Keep only the most recent entries to avoid O(n²) copy-shift.
	if len(auditEvents) > maxInMemoryAuditEvents {
		auditEvents = auditEvents[len(auditEvents)-maxInMemoryAuditEvents:]
	}
	for _, record := range auditEvents {
		s.appendAuditTrailLocked(auditEventFromRecord(record))
	}
	// Seed the chain tail from the persisted store so the next
	// in-process append continues the chain instead of starting a
	// fresh empty-prev branch (migration 0038).
	tail, err := s.store.LatestAuditChainHash(ctx)
	if err != nil {
		return err
	}
	s.auditChainTail = tail
	s.auditChainLoaded = true
	return nil
}

// maxPrefixedSequence returns the larger of `current` and the trailing
// integer parsed out of `value` when it has the form `prefix-N`. Used
// to seed in-memory sequence counters from the most-recently-persisted
// IDs so new IDs minted after a restart never collide with old ones.
func maxPrefixedSequence(current uint64, prefix string, value string) uint64 {
	if !strings.HasPrefix(value, prefix+"-") {
		return current
	}

	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, prefix+"-"), 10, 64)
	if err != nil {
		return current
	}
	if parsed > current {
		return parsed
	}

	return current
}
