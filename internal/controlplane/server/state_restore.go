package server

import (
	"context"
	"strconv"
	"strings"
	"time"
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
//  4. Metric + audit sequence — both histories are served straight from the
//     store (A2: the in-memory rings were removed), so only the sequence
//     counters are seeded so post-restart IDs never collide with old ones.
//     restoreAuditSeq additionally seeds the audit hash-chain tail so the
//     next in-process append continues the persisted chain.
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
		s.restoreMetricSeq,
		s.restoreAuditSeq,
		s.restoreStoredTelemetry,
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
		// Restore the agent live-state baseline with no instances yet;
		// restoreInstances + restoreStoredTelemetry layer the rest on top.
		// Agents are restored before instances (see restoreStoredState order),
		// so the instance prune in ApplySnapshot has a fresh set to work with.
		s.live.ApplySnapshot(agent.ID, agent, nil)
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
	// Group instances by owning agent, then commit each agent's set via
	// live.SetInstances (which prunes that agent's stale entries without
	// touching the agent value restored by restoreAgents). Agents were
	// restored first (see restoreStoredState order).
	byAgent := make(map[string][]Instance)
	for _, record := range instances {
		instance := instanceFromRecord(record)
		byAgent[instance.AgentID] = append(byAgent[instance.AgentID], instance)
	}
	for agentID, set := range byAgent {
		s.live.SetInstances(agentID, set)
	}
	return nil
}

// restoreMetricSeq seeds the in-memory metric sequence counter from the
// most-recently-persisted IDs so new IDs minted after a restart never collide
// with old ones. The metric history itself is served straight from the store
// (A2: the in-memory ring was removed), so nothing is hydrated into memory here.
func (s *Server) restoreMetricSeq(ctx context.Context) error {
	metrics, err := s.store.ListMetricSnapshots(ctx)
	if err != nil {
		return err
	}
	for _, record := range metrics {
		s.metricSeq = maxPrefixedSequence(s.metricSeq, "metric", record.ID)
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
	entered := make(map[string]time.Time, len(records))
	for _, r := range records {
		entered[r.AgentID] = r.EnteredAt.UTC()
	}
	s.fallback.Restore(entered)
	return nil
}

// restoreAuditSeq seeds the in-memory audit sequence counter from the
// most-recently-persisted IDs so new IDs minted after a restart never collide
// with old ones, and seeds the audit hash-chain tail so the next in-process
// append continues the persisted chain instead of starting a fresh empty-prev
// branch (migration 0038). The audit history itself is served straight from the
// store (A2: the in-memory ring was removed), so nothing is hydrated into
// memory here.
func (s *Server) restoreAuditSeq(ctx context.Context) error {
	auditEvents, err := s.store.ListAuditEvents(ctx, auditFirstPageLimit)
	if err != nil {
		return err
	}
	for _, record := range auditEvents {
		s.auditSeq = maxPrefixedSequence(s.auditSeq, "audit", record.ID)
	}
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
