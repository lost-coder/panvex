# ADR-003: Retention — configurable TTL with default 90d audit / 30d metrics

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P2-REL-03, P2-REL-04, P2-REL-05

## Context

Phase 1 stored every audit event, metric sample, and rollup row forever.
On a long-running panel with even a modest fleet, the timeseries and
audit tables grew without bound, degrading query latency and eventually
threatening disk exhaustion. The audit called this out under
P2-REL-03/04/05. Operators told us different environments have
different obligations: some want 7 days of metrics to keep a demo panel
cheap; regulated deployments need 365 days of audit for compliance. A
single compile-time default could not satisfy both.

## Decision

Retention is driven by per-panel settings persisted in
`panel_settings.retention_json`. The JSON document exposes three keys
today: `audit_days`, `metrics_days`, and `rollup_days`. Defaults on a
fresh install are **90 days audit, 30 days metrics, 180 days rollups**.
A worker in `internal/timeseries/timeseries_rollup.go` prunes rows
older than the configured cutoff on a scheduled tick and emits a
`retention.pruned` audit event summarising the delete counts. Settings
are editable via the Settings UI (backed by the same JSON column) and
take effect on the next worker tick without restart.

## Alternatives considered

- **Per-environment compile-time defaults** (e.g. build tags for
  "demo" vs "prod"). Rejected: forces a rebuild to change policy,
  cannot differentiate between tenants on the same binary, and
  makes it impossible for operators to experiment.
- **Postgres-only `pg_partman` / partitioned tables.** Attractive for
  bulk drop efficiency but incompatible with the SQLite backend, which
  we still support for single-node deployments. The delete-by-cutoff
  worker is dumber but uniform across backends.
- **Infinite retention with manual archival.** Rejected as the status
  quo that caused the finding.

## Consequences

- New deployments will silently prune data older than 90 days. The
  install guide and the Settings UI explain the defaults loudly.
- The retention worker now issues large DELETEs periodically. We chose
  batched deletes (`LIMIT 10000` with a loop) to avoid long-running
  transactions that would block other writers.
- Any feature that needs historical data beyond the TTL (future:
  long-term trend analytics, SLA reports) must either materialise a
  separate rollup table with its own retention or require the operator
  to raise the TTL.
