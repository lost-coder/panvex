package server

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
)

// Q5.U-Q-01: audit-trail subsystem extracted out of server.go. The
// methods stay on Server (they read/write s.metricsAuditMu, s.auditSeq,
// the audit hash-chain tail, s.events, s.batchWriter, s.store, s.logger),
// but the file-level grouping makes it easier to reason about the trail in
// isolation while the larger god-object decomposition lands incrementally.

// appendAudit is the no-ctx convenience shim retained for fire-and-forget
// callers (test helpers, post-result job audits, enroll-driver event
// notifier) that do not have a request- or stream-scoped ctx in hand. It
// routes through the lifecycle context (Server.Context()) so a Close()
// during a wedged audit publish surfaces cancellation rather than an
// unbounded Background tree (Plan 3 / BP-01). Callers that DO have a
// request ctx should call appendAuditWithContext directly so audit
// cancellation tracks the request.
func (s *Server) appendAudit(actorID string, action string, targetID string, details map[string]any) {
	s.appendAuditWithContext(s.Context(), actorID, action, targetID, details)
}

func (s *Server) appendAuditWithContext(ctx context.Context, actorID string, action string, targetID string, details map[string]any) {
	s.metricsAuditMu.Lock()
	s.auditSeq++
	event := AuditEvent{
		ID:       newSequenceID("audit", s.auditSeq),
		ActorID:  actorID,
		Action:   action,
		TargetID: targetID,
		// Second precision: the at-rest store keeps created_at as whole
		// Unix seconds (toUnix), and the hash-chain hashes CreatedAt via
		// RFC3339Nano. Truncating here keeps the hashed value identical to
		// the value read back from storage, so verify-audit-chain
		// recomputes the same event_hash instead of reporting false
		// tampering on every event.
		CreatedAt: s.now().UTC().Truncate(time.Second),
		Details:   normalizeAuditDetails(details),
	}
	record := s.chainAuditRecordLocked(ctx, event)
	s.metricsAuditMu.Unlock()

	// P2-LOG-10 / M-R4 / P7-R6: audit writes no longer block the HTTP
	// request path. Storage persistence runs asynchronously on the batch
	// writer, and the /api/audit read path serves its first page straight
	// from the store (A2: the in-memory ring was removed). Close() drains
	// the queue on shutdown (StopWithTimeout 10s) so in-flight audit events
	// still survive a graceful restart. Persistent failures (NOT NULL,
	// schema mismatch, retry-exhausted) are surfaced by the batch writer
	// with slog.Error + alert=audit_persist_failed so operators can page on
	// the audit pipeline independently of other streams.
	//
	// ctx is intentionally unused here — the batch writer runs under its
	// own long-lived context, so there is nothing the HTTP request's
	// cancellation should abort once the record is enqueued.
	_ = ctx
	if s.batchWriter != nil {
		s.batchWriter.auditEvents.Enqueue(record)
	}

	s.events.Publish(eventbus.Event{
		Type: "audit.created",
		Data: event,
	})
}

// appendAuditSync writes the audit event to storage synchronously before
// returning. Use this for security-critical events (login, privilege
// grants) where the persisted audit record must exist BEFORE the
// user-visible side effect (e.g. session cookie) so a later incident
// response can attribute the action even if the process crashes
// immediately after. The caller is expected to abort the user-visible
// action (reject the login, roll back the cookie) on a non-nil error
// return so we never hand out a session whose issuance wasn't recorded.
//
// The event-bus publish happens regardless of the storage outcome so the
// live event feed always sees the attempt. /api/audit reads its first page
// from the store, so a successful sync persist makes the event immediately
// visible there too. A persistent-failure log line with
// alert=audit_persist_failed is emitted so the metrics alert in
// deploy/prometheus/alerts.yml fires.
//
// When the server has no storage wired (pure in-memory test doubles)
// the sync path degrades to the async contract (no error returned).
// Callers should pass a context with a bounded deadline so a wedged
// database cannot hold an HTTP request forever.
func (s *Server) appendAuditSync(ctx context.Context, actorID, action, targetID string, details map[string]any) error {
	s.metricsAuditMu.Lock()
	s.auditSeq++
	event := AuditEvent{
		ID:       newSequenceID("audit", s.auditSeq),
		ActorID:  actorID,
		Action:   action,
		TargetID: targetID,
		// See appendAuditWithContext: truncate to second so the hashed
		// CreatedAt matches the whole-second value persisted at rest.
		CreatedAt: s.now().UTC().Truncate(time.Second),
		Details:   normalizeAuditDetails(details),
	}
	record := s.chainAuditRecordLocked(ctx, event)
	s.metricsAuditMu.Unlock()

	var persistErr error
	if s.store != nil {
		persistErr = s.store.AppendAuditEvent(ctx, record)
		if persistErr != nil {
			s.logger.ErrorContext(ctx, "audit persist (sync) failed",
				"action", action,
				"actor_id", actorID,
				"target_id", targetID,
				"error", persistErr,
				"alert", "audit_persist_failed",
			)
		}
	}

	s.events.Publish(eventbus.Event{
		Type: "audit.created",
		Data: event,
	})

	// Webhook fan-out: send every successfully persisted audit
	// event to operator-configured external receivers via the
	// outbox. Skipped when persist failed — the outbox would
	// reference a non-existent audit row, breaking the chain
	// invariant the verifier relies on. Filtering by action is
	// the receiver's job (event_filter on webhook_endpoints).
	if persistErr == nil {
		s.publishWebhookEvent(ctx, "audit."+action, event)
	}
	return persistErr
}

// normalizeAuditDetails guarantees the JSON-marshalled details field is an
// object, not null. The web client's Zod schema declares details as a
// non-nullable record (web/src/shared/api/schemas/jobs.ts auditEventSchema)
// and rejects the whole /api/audit response if any entry serializes
// "details": null. A nil Go map encodes to null, so we substitute an empty
// map at construction time.
func normalizeAuditDetails(details map[string]any) map[string]any {
	if details == nil {
		return map[string]any{}
	}
	return details
}
