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
func (s *Server) restoreStoredState() error {
	agents, err := s.store.ListAgents(context.Background())
	if err != nil {
		return err
	}
	for _, record := range agents {
		agent := agentFromRecord(record)
		s.agents[agent.ID] = agent
	}

	// Restore persisted agent revocations (P1-SEC-06). Without this, a CP
	// restart silently forgets the revocation and a deleted agent whose
	// 30-day client cert is still valid could reconnect over mTLS.
	revocations, err := s.store.ListAgentRevocations(context.Background())
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

	instances, err := s.store.ListInstances(context.Background())
	if err != nil {
		return err
	}
	for _, record := range instances {
		instance := instanceFromRecord(record)
		s.instances[instance.ID] = instance
	}

	metrics, err := s.store.ListMetricSnapshots(context.Background())
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

	auditEvents, err := s.store.ListAuditEvents(context.Background(), maxInMemoryAuditEvents)
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

	return s.restoreStoredTelemetry()
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
