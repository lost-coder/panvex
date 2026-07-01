package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/audit/hashchain"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// chainAuditRecordLocked converts an in-memory AuditEvent into the
// storage-shape record, populating PrevHash + EventHash by chaining
// onto Server.auditChainTail and advancing the tail in the same
// critical section.
//
// Caller MUST hold metricsAuditMu — the function reads and writes
// auditChainTail without further locking. This serialises the chain
// across both the async (appendAuditWithContext) and sync
// (appendAuditSync) producer paths.
//
// On a fresh process where state_restore hasn't run (test fixtures,
// store-less servers), auditChainLoaded is false and the tail is "" —
// which is the same value the verifier treats as the chain-genesis
// sentinel. The chain begins forming on the first append.
//
// If hash computation fails (extreme: a Marshaler in Details
// returns an error), the function falls back to leaving PrevHash
// and EventHash empty rather than dropping the event. Persistence
// continues; the verifier reports the gap as legacy/genesis prefix.
// We log the failure so operators notice but do not block the audit
// path on a hashing edge case.
//
// The actual hash + canonicalisation primitives live in the shared
// internal/controlplane/audit/hashchain package — same source of
// truth used by the verify-audit-chain subcommand. Wave 4.1 first
// slice (2026-05-08) extracted them out of this file to remove the
// previous server↔cmd/control-plane duplication.
func (s *Server) chainAuditRecordLocked(ctx context.Context, event AuditEvent) storage.AuditEventRecord {
	record := auditEventToRecord(event)
	prev := s.auditChainTail
	hash, err := hashchain.ComputeEventHash(prev, record)
	if err != nil {
		s.logger.ErrorContext(ctx, "audit chain hash compute failed",
			"event_id", record.ID,
			"action", record.Action,
			"error", err,
			"alert", "audit_chain_compute_failed",
		)
		return record
	}
	record.PrevHash = prev
	record.EventHash = hash
	s.auditChainTail = hash
	return record
}
