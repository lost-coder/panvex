# ADR-004: Batch writer error semantics — retry transient, drop persistent, counter alerts

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P2-REL-06

## Context

The batch writer is Panvex's central ingest funnel: metrics, events,
audit entries, and several other stream types all flow through it before
hitting the storage layer. The audit (P2-REL-06) discovered that the
writer swallowed every storage error silently — a transient connection
blip and a schema mismatch looked identical to a healthy flush to any
external observer. Lost writes produced no log line, no metric, and no
alerting signal. We needed error handling that preserved availability
(one slow backend must not stall every producer) without becoming a
silent sink for real bugs.

## Decision

The batch writer now classifies every storage error into one of three
buckets and applies a distinct policy to each:

1. **Transient** (context deadline, connection reset, serialization
   failure, SQLite `SQLITE_BUSY`) — retried up to 3 times with
   exponential backoff (base 100ms, cap 2s). If all retries fail, the
   batch is demoted to "persistent" and handled below.
2. **Persistent** (constraint violation, schema mismatch, malformed
   row) — the offending batch is dropped, a structured `slog` warning
   is emitted with the error class and stream name, and a Prometheus
   counter `panvex_batch_writer_drops_total{stream,reason}` is
   incremented. For the audit stream specifically, a synthetic entry
   with `alert=audit_persist_failed` is appended to the next successful
   flush so operators see the gap on the audit timeline itself.
3. **Fatal process-wide** (storage completely unreachable past retry
   budget) — the writer keeps draining its channels but marks itself
   `degraded=true`; `/healthz` flips to non-ready.

## Alternatives considered

- **Circuit-breaker around storage.** Deferred to a later phase. It
  reduces retry storms under sustained outage but complicates the error
  taxonomy and we wanted the classifier in place first.
- **Fail-fast: `os.Exit(1)` on any persistent error.** Rejected: the
  batch writer holds tens of thousands of in-memory samples from every
  connected agent. Exiting abandons all of them and induces an alert
  storm as agents reconnect.
- **Log-only, no counter.** Rejected: logs are not aggregated uniformly
  across deployments; counters feed the dashboard and alerts.

## Consequences

- Operators must scrape and alert on
  `panvex_batch_writer_drops_total`. The default Grafana dashboard now
  includes a panel for it.
- The audit stream's `audit_persist_failed` marker becomes part of the
  API contract for the audit viewer: downstream UI filters must not
  hide it.
- Persistent errors no longer block the pipeline, so a bug that
  produces malformed rows will drop data indefinitely until the
  counter fires. Alert thresholds therefore need to be tight.
