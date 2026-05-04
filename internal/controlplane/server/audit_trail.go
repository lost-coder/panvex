package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
)

// Q5.U-Q-01: audit-trail subsystem extracted out of server.go. The
// methods stay on Server (they read/write s.metricsAuditMu, s.auditBuf,
// s.auditSeq, s.events, s.batchWriter, s.store, s.logger), but the
// file-level grouping makes it easier to reason about the trail in
// isolation while the larger god-object decomposition lands
// incrementally.

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
		ID:        newSequenceID("audit", s.auditSeq),
		ActorID:   actorID,
		Action:    action,
		TargetID:  targetID,
		CreatedAt: s.now().UTC(),
		Details:   normalizeAuditDetails(details),
	}
	s.appendAuditTrailLocked(event)
	s.metricsAuditMu.Unlock()

	// P2-LOG-10 / M-R4 / P7-R6: audit writes no longer block the HTTP
	// request path. The in-memory ring buffer above (PERF-02) already
	// serves the /api/audit read path, and storage persistence now runs
	// asynchronously on the batch writer. Close() drains the queue on
	// shutdown (StopWithTimeout 10s) so in-flight audit events still
	// survive a graceful restart. Persistent failures (NOT NULL, schema
	// mismatch, retry-exhausted) are surfaced by the batch writer with
	// slog.Error + alert=audit_persist_failed so operators can page on
	// the audit pipeline independently of other streams.
	//
	// ctx is intentionally unused here — the batch writer runs under its
	// own long-lived context, and the audit record has already been
	// copied into the ring buffer and captured by the snapshot so there
	// is nothing the HTTP request's cancellation should abort.
	_ = ctx
	if s.batchWriter != nil {
		s.batchWriter.auditEvents.Enqueue(auditEventToRecord(event))
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
// The in-memory ring buffer and event-bus publish happen regardless of
// the storage outcome: /api/audit and the live event feed always see
// the attempt. A persistent-failure log line with alert=audit_persist_failed
// is emitted so the metrics alert in deploy/prometheus/alerts.yml fires.
//
// When the server has no storage wired (pure in-memory test doubles)
// the sync path degrades to the async contract (no error returned).
// Callers should pass a context with a bounded deadline so a wedged
// database cannot hold an HTTP request forever.
func (s *Server) appendAuditSync(ctx context.Context, actorID, action, targetID string, details map[string]any) error {
	s.metricsAuditMu.Lock()
	s.auditSeq++
	event := AuditEvent{
		ID:        newSequenceID("audit", s.auditSeq),
		ActorID:   actorID,
		Action:    action,
		TargetID:  targetID,
		CreatedAt: s.now().UTC(),
		Details:   normalizeAuditDetails(details),
	}
	s.appendAuditTrailLocked(event)
	s.metricsAuditMu.Unlock()

	var persistErr error
	if s.store != nil {
		persistErr = s.store.AppendAuditEvent(ctx, auditEventToRecord(event))
		if persistErr != nil {
			s.logger.Error("audit persist (sync) failed",
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

// appendAuditTrailLocked appends one event to the ring buffer in O(1) time.
// Caller must hold s.metricsAuditMu for writing.
func (s *Server) appendAuditTrailLocked(event AuditEvent) {
	capacity := len(s.auditBuf)
	if s.auditSize < capacity {
		// Ring still filling — insert at the next free slot, which is the
		// element immediately after the current tail.
		s.auditBuf[s.auditSize] = event
		s.auditSize++
		return
	}
	// Ring is full — overwrite the oldest slot (auditHead), then advance
	// auditHead to point at the new oldest slot.
	s.auditBuf[s.auditHead] = event
	s.auditHead++
	if s.auditHead == capacity {
		s.auditHead = 0
	}
}

// snapshotAuditTrailLocked returns a newly allocated slice of the current
// audit events in oldest-to-newest order. Caller must hold metricsAuditMu
// for reading. The returned slice is safe to retain after the lock is
// released; it does not alias s.auditBuf.
func (s *Server) snapshotAuditTrailLocked() []AuditEvent {
	out := make([]AuditEvent, s.auditSize)
	if s.auditSize == 0 {
		return out
	}
	capacity := len(s.auditBuf)
	if s.auditSize < capacity {
		// Head is still 0 while the ring is filling; entries are at [0,size).
		copy(out, s.auditBuf[:s.auditSize])
		return out
	}
	// Ring is full: oldest entry lives at auditHead. Copy the tail segment
	// [head, end) first, then wrap around for [0, head).
	n := copy(out, s.auditBuf[s.auditHead:])
	copy(out[n:], s.auditBuf[:s.auditHead])
	return out
}
