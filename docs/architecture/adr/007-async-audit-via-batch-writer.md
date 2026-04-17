# ADR-007: Async audit writes via batch_writer 8th stream

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P2-LOG-10

## Context

Originally, audit events were persisted by a synchronous `INSERT`
inside the request handler. P2-LOG-10 caught a production incident
where a saturated database made `POST /api/auth/login` block on the
audit insert, and because the handler returned the session cookie
*after* the audit insert returned, every login attempt stalled for
the duration of the DB pressure. The symptom ("cannot log in")
looked like an auth system failure even though auth was healthy;
audit was the bottleneck. We needed audit persistence to be
decoupled from the request lifecycle while preserving two invariants:
no audit event is silently lost on graceful shutdown, and any
persistent failure is visible to operators.

## Decision

Audit writes now go through the existing batch_writer as its
**eighth stream** (`auditEvents`). Handlers call
`audit.Append(ctx, event)` which simply pushes onto a buffered
channel; the handler returns immediately. The batch_writer flushes
the audit stream on the same tick as its other streams.

Two additional rules apply to this stream specifically:

- **Graceful drain on shutdown.** `Server.Shutdown` flushes the audit
  channel with a 10-second timeout before returning. If the timeout
  expires, the remaining events are logged to stderr as
  `audit.drain_lost` with the full payload, giving an operator a
  last-resort recovery path from the container logs.
- **Critical-alert marker.** On persistent storage failure (see
  ADR-004), the audit stream injects a synthetic
  `alert=audit_persist_failed` row so the gap is visible inside the
  audit viewer itself, not only in metrics.

## Alternatives considered

- **Dedicated audit goroutine with its own writer.** Rejected:
  duplicates the batch_writer's machinery (channel, flush loop,
  retry classifier, shutdown drain) for no benefit, and introduces
  a second piece of code to evolve whenever we change storage
  semantics.
- **Fire-and-forget with no drain.** Rejected: would drop audit
  events on every deploy. Audit must survive a graceful restart.
- **Keep audit synchronous but give it its own DB connection
  pool.** Considered; it protects against one pool saturating but
  does not address the fundamental coupling to request latency.

## Consequences

- Audit is eventually-consistent. A read-your-writes UI cannot
  assume an event written during a request will be queryable on the
  next request. The audit viewer tolerates this.
- The batch_writer's shutdown budget grew from ~5s to 10s to
  accommodate audit drain. Deployment scripts must allow at least
  15 seconds `stopGracePeriod`.
- Any future stream with "must not be lost" semantics can follow the
  same pattern.
