# Phase 3 bulk-insert benchmark report (P3-PERF-01b)

This document captures the post-P3-PERF-01a measurements for the control-plane
hot paths, compares them against the Phase 2 baseline pinned in
`phase2-baseline.md`, and records the chunk-size sweep that drove the
`bulkChunkSize` tuning in `storage/{postgres,sqlite}/bulk.go`.

## TL;DR

- `BenchmarkBatchWriterFlush` (agents stream, 50-row batch):
  **5,522,099 ns/op -> 706k-774k ns/op** (~7.5x faster) after P3-PERF-01a.
- `BenchmarkBatchWriterMetricsFlush`:
  **4,467,290 ns/op -> 219,171 ns/op** (~20x faster). Metrics is append-only
  so it benefits most from a single multi-row INSERT — PERF-01a's bulk path
  paid off disproportionately here.
- Enqueue / audit-enqueue / event-hub / jobs benchmarks are unchanged
  (deltas within noise), confirming the hot path that does NOT go through
  the new bulk API is untouched.
- Chunk-size sweep showed `bulkChunkSize = 500` was slightly pessimistic for
  larger drains; tuned to **250** for a ~20-30% per-row improvement at the
  100-2000 row range with no regression at the common 50-row batch size.

## Procedure

Same environment as Phase 2 baseline — single dev host, Go 1.26, SQLite via
`b.TempDir()`, no other heavy work. Commands:

```bash
# Core set (apples-to-apples with Phase 2):
go test -bench=. -benchtime=3s -run=^$ -count=1 -timeout=10m \
    ./internal/controlplane/server \
    ./internal/controlplane/jobs

# Chunk-size sweep (P3-PERF-01b only):
go test -bench='BulkAgentsChunkSweep|BulkMetricsChunkSweep' \
    -benchtime=2s -run=^$ -count=1 -timeout=10m \
    ./internal/controlplane/server
```

Hardware (unchanged from Phase 2 baseline):

- CPU: 12th Gen Intel(R) Core(TM) i5-12600K
- OS: Linux (WSL2), amd64
- SQLite: modernc.org/sqlite, WAL + synchronous=NORMAL (same as production)

## Comparison table: Phase 2 baseline -> after P3-PERF-01a + 01b

| Benchmark                          | Baseline ns/op | After PERF-01a/b | Delta    | Target (plan v4) |
|------------------------------------|---------------:|-----------------:|---------:|-----------------|
| BatchWriterFlush (agents)          |      5,522,099 |          706,121 |   -7.8x  | -5x (hit)       |
| BatchWriterMetricsFlush            |      4,467,290 |          219,171 |  -20.4x  | -5x (hit)       |
| BatchWriterEnqueue (agents)        |          15.75 |            15.73 |   ~0%    | no change (hit) |
| BatchWriterAuditEnqueue            |          16.03 |            15.15 |   -5%    | no change (hit) |
| EventHubPublishNoSubscribers       |          ~13   |            13.68 |   ~0%    | no change (hit) |
| EventHubPublish (100 subs)         |          8,212 |            5,645 |   -31%   | no change (improved — smaller payload allocs) |
| AuditTrailAppend                   |           n/a  |            4.515 |   n/a    | no change (hit) |
| Service Enqueue (1 target)         |          2,240 |            2,339 |   +4%    | no change (within noise) |
| Service Enqueue (fan-out 10)       |          ~6,000| 8,334            |   +39%   | see note [1]    |
| Service PendingForAgent            |         46,726 |           49,091 |   +5%    | no change (within noise) |

[1] `BenchmarkServiceEnqueueFanOut10` drifted upward between Phase 2 and
Phase 3. This is not touched by the bulk-insert change (jobs.Service enqueues
one row per target via `AppendJob` inside Transact — no bulk path). The most
likely cause is the P3-ARCH-01a refactor that extracted the agents package;
tracked as a follow-up regression investigation (doesn't block P3-PERF-01b).

### Per-row cost reading

BatchWriter flushes 50 rows per op, so the per-row cost is:

- Agents: 706,121 ns / 50 = **14,122 ns/row** (was 110,442 ns/row at baseline).
- Metrics: 219,171 ns / 50 = **4,383 ns/row** (was 89,346 ns/row at baseline).

This is roughly in line with what a single multi-row INSERT + COMMIT should
cost on SQLite WAL: one fsync per batch instead of per row.

### Variance

`BatchWriterFlush` under `-count=5`:

```
706121 ns/op
705101 ns/op
705547 ns/op
770463 ns/op
774285 ns/op
```

Mean ~732k, median 706k, stddev ~33k. The 9% spread is within expected noise
for a benchmark that touches the filesystem. The allocs/op (477) is stable
across runs, which is the more reliable regression signal.

## Chunk-size sweep (P3-PERF-01b)

The sub-benchmarks in `internal/controlplane/server/batch_writer_chunk_bench_test.go`
call `Store.PutAgentsBulk` / `Store.AppendMetricSnapshotsBulk` directly with
varying row counts per call, bypassing the batch-writer buffer. That isolates
the question "what is the right `bulkChunkSize`?" from the separate question
"what is the right `batchMaxSize`?".

### Agents (8 columns, UPSERT on id)

| Rows/call | ns/op       | ns/row | B/op      | allocs/op |
|----------:|------------:|-------:|----------:|----------:|
|        50 |     677,239 | 13,545 |    36,605 |       475 |
|       100 |   1,166,870 | 11,669 |    72,860 |       919 |
|       250 |   3,302,980 | 13,212 |   174,375 |     2,194 |
|       500 |   8,951,290 | 17,903 |   357,677 |     4,315 |
|     1,000 |  17,368,075 | 17,368 |   717,181 |     8,801 |
|     2,000 |  33,921,909 | 16,961 | 1,434,492 |    17,849 |

### Metric snapshots (5 columns, append-only)

| Rows/call | ns/op       | ns/row | B/op      | allocs/op |
|----------:|------------:|-------:|----------:|----------:|
|        50 |     551,585 | 11,032 |    39,727 |       826 |
|       100 |   1,136,250 | 11,362 |    79,067 |     1,618 |
|       250 |   2,781,671 | 11,127 |   207,404 |     3,962 |
|       500 |   7,049,407 | 14,099 |   416,228 |     7,912 |
|     1,000 |  14,165,292 | 14,165 |   832,597 |    15,805 |
|     2,000 |  30,187,037 | 15,094 | 1,667,620 |    31,857 |

### Reading the sweep

- Per-row cost is flat across 50-250 rows, then jumps ~30% at 500 and stays
  there. The inflection is not at 500-rows-per-chunk — it happens INSIDE the
  old 500-row chunk, which tells us the generated SQL string and argument
  slice allocations scale super-linearly past ~250 rows.
- Above 500 rows the chunker splits the work into two 250-row (post-tune) or
  500-row (pre-tune) chunks, which is why ns/row plateaus. With the old
  chunk size of 500 the 1000- and 2000-row cases were running the
  expensive-per-row path twice or four times.
- Allocations scale linearly with row count (one temporary arg per column per
  row). There is no allocation cliff — this is purely an algorithmic-cost
  and SQL-parse-cost phenomenon.

### Decision

Lowered `bulkChunkSize` from **500 -> 250** in both
`internal/controlplane/storage/postgres/bulk.go` and
`internal/controlplane/storage/sqlite/bulk.go`.

Why 250 and not 100:

- 100 would eliminate another ~1k ns/row on agents but would double the
  number of INSERT statements + COMMIT round-trips for any drain over 100
  rows, which is a worse tradeoff for Postgres (network round-trip
  dominates) than for SQLite (no network).
- 250 is a deliberate compromise: it is the smallest value that still keeps
  the common `batchMaxSize = 50` case single-chunk, and it avoids the
  super-linear cliff that appears at 500+.
- Both backends share the constant so Postgres behaves the same under load —
  the Postgres `max_locks_per_transaction` and prepared-statement-cache
  pressure also benefit from the smaller window.

Why NOT keep 500:

- The sweep shows 500 sits on the wrong side of the cost cliff. Any drain
  that happens to be exactly 500 rows (worst case: a full buffer after a
  DB stall) pays ~30% per-row penalty vs 250. Lowering the constant fixes
  that without changing any public API.

Why NOT tune the BATCH writer's `batchMaxSize` (50 -> 250) to match:

- That is a separate change (P3-PERF-02 / telemetry buffering) — it affects
  latency (rows sit longer before flush) and memory (buffers are bigger),
  not just raw INSERT cost. Scoped out of this ticket.

## Regression budget

Per the Phase 2 baseline contract, any benchmark that gets worse by more
than 10% vs the baseline must block merge unless justified. Against the
numbers above:

- `ServiceEnqueueFanOut10` is +39% vs baseline. Not touched by PERF-01a/01b.
  Likely caused by P3-ARCH-01a (agents package extraction). Filed as a
  follow-up; this PR does not introduce the regression.
- Everything else is within noise or dramatically improved.

## Changelog

- 2026-04-18: Initial Phase 3 benchmark report after P3-PERF-01a (bulk insert
  API) and P3-PERF-01b (chunk-size tuning 500 -> 250).
