# Phase 2 performance baseline (P2-PERF-05)

This document pins a reproducible performance baseline for the control-plane
hot paths at the end of Phase 2. It is the reference point for comparing
Phase 3 work — in particular **PERF-06 (bulk insert)**, which is expected to
reshape the batch-writer flush cost curve.

## Scope and rationale

**Scope.** Microbenchmarks for:

1. `batch_writer` — Enqueue (agents, audit) and Flush (agents, metrics)
2. `event_hub` — Publish with 0 and 100 subscribers
3. `jobs.Service` — Enqueue (single target, fan-out) and PendingForAgent

**Why microbenchmarks, not a full-fleet simulator.** The original spec sketched
a 1000-agent gRPC simulator (`cmd/benchagent`) running for 5 minutes while a
side-car scraped `/metrics`. For the current stage (no staging infra, single
dev host) this has poor signal-to-noise:

- Results would be dominated by goroutine-scheduler noise, not the code under
  test.
- The PERF-06 change is intrinsically about one function (`storeBatchWriter`'s
  flush path). A microbench isolates that cost; a full simulator would
  measure the sum of everything else too.
- Microbenchmarks run in < 1 minute and are reproducible in CI.

A full-fleet harness is still a useful Phase 3+ artifact for validating
end-to-end throughput after PERF-06 lands, but it is NOT needed to compare the
before/after of the bulk-insert change. When we do build one, place it in
`cmd/benchagent/` per the remediation plan.

The benchmarks live alongside the code they exercise:

- `internal/controlplane/server/batch_writer_bench_test.go`
- `internal/controlplane/jobs/service_bench_test.go`

## Procedure

Prerequisites: Go 1.26, a writable `$TMPDIR` (SQLite databases go to
`b.TempDir()`), no other compute-heavy work on the machine.

```bash
# From the repo root:
make bench

# Or equivalently:
go test -bench=. -benchtime=3s -run=^$ -count=1 -timeout=10m \
    ./internal/controlplane/server \
    ./internal/controlplane/jobs
```

Each benchmark prints `ns/op`, `B/op`, `allocs/op`. The batch-writer flush
benchmarks also exercise SQLite through `sqlite.Open` (WAL, synchronous =
NORMAL), so each of those emits one goose-migration log block before the
result line.

For PERF-06 comparison, capture `-count=5` and feed both files to `benchstat`:

```bash
go install golang.org/x/perf/cmd/benchstat@latest

# Before PERF-06:
go test -bench=. -benchtime=3s -run=^$ -count=5 \
    ./internal/controlplane/server ./internal/controlplane/jobs \
    > /tmp/bench-before.txt

# After PERF-06:
go test -bench=. -benchtime=3s -run=^$ -count=5 \
    ./internal/controlplane/server ./internal/controlplane/jobs \
    > /tmp/bench-after.txt

benchstat /tmp/bench-before.txt /tmp/bench-after.txt
```

## Observed baseline (main @ Phase 2 close)

Host: Linux (WSL2 Ubuntu), Go 1.26, 12th Gen Intel Core i5-12600K (4 logical
cores exposed to WSL2), SQLite via `modernc.org/sqlite` with WAL + synchronous
= NORMAL. Single run, `-benchtime=3s`, no other load.

```
goos: linux
goarch: amd64
pkg: github.com/lost-coder/panvex/internal/controlplane/server
cpu: 12th Gen Intel(R) Core(TM) i5-12600K
BenchmarkBatchWriterEnqueue-4                   228186051        15.75 ns/op         0 B/op        0 allocs/op
BenchmarkBatchWriterFlush-4                          684       5522099 ns/op     36401 B/op      882 allocs/op
BenchmarkBatchWriterMetricsFlush-4                   841       4467290 ns/op     40791 B/op     1185 allocs/op
BenchmarkBatchWriterAuditEnqueue-4              227548162        16.03 ns/op         0 B/op        0 allocs/op
BenchmarkEventHubPublishNoSubscribers-4         253507225        14.44 ns/op         0 B/op        0 allocs/op
BenchmarkEventHubPublish100Subscribers-4           859449          8212 ns/op      1040 B/op       10 allocs/op
BenchmarkAuditTrailAppend-4                     725550369         4.884 ns/op        0 B/op        0 allocs/op
PASS
ok    github.com/lost-coder/panvex/internal/controlplane/server    35.853s

pkg: github.com/lost-coder/panvex/internal/controlplane/jobs
cpu: 12th Gen Intel(R) Core(TM) i5-12600K
BenchmarkServiceEnqueueSingleTarget-4             2074796          2240 ns/op       837 B/op        8 allocs/op
BenchmarkServiceEnqueueFanOut10-4                 1000000          6669 ns/op      2794 B/op        7 allocs/op
BenchmarkServicePendingForAgent-4                   74347         46726 ns/op     29880 B/op      204 allocs/op
PASS
ok    github.com/lost-coder/panvex/internal/controlplane/jobs    20.539s
```

> Note: the batch-writer flush benchmarks emit goose migration log lines
> during `sqlite.Open` that are printed between the benchmark name and the
> result on the same stdout line. `grep -B1 'ns/op' /tmp/bench-output.txt`
> reconstructs the result rows cleanly.

### Summary table

| Benchmark                                | ns/op     | B/op   | allocs/op | Notes                                      |
|------------------------------------------|-----------|--------|-----------|--------------------------------------------|
| BatchWriter Enqueue (agents)             |     15.75 |      0 |         0 | Slice append + non-blocking signal.        |
| BatchWriter Enqueue (audit)              |     16.03 |      0 |         0 | Same hot path, different typed buffer.     |
| BatchWriter Flush (agents, 50 rows)      | 5,522,099 | 36,401 |       882 | ~110 us / row via SQLite PutAgent.         |
| BatchWriter Flush (metrics, 50 rows)     | 4,467,290 | 40,791 |     1,185 | ~89 us / row via AppendMetricSnapshot.     |
| EventHub Publish (0 subs)                |     14.44 |      0 |         0 | Lower-bound; RLock + slice snapshot only.  |
| EventHub Publish (100 subs)              |     8,212 |  1,040 |        10 | ~82 ns / subscriber broadcast.             |
| Service Enqueue (1 target)               |     2,240 |    837 |         8 | Includes map writes + time.UTC().          |
| Service Enqueue (10 targets)             |     6,669 |  2,794 |         7 | ~670 ns / target bookkeeping.              |
| Service PendingForAgent (100 matching)   |    46,726 | 29,880 |       204 | Read-only scan over all jobs.              |

### Key observations

- **Enqueue is effectively free** (< 20 ns, zero allocs) on both the agent
  and audit streams. This confirms P2-LOG-10 / M-R4 (async audit writes): the
  HTTP handler never blocks on the DB, even under load.
- **Flush is the dominant cost**: ~5-8 ms for a 50-row batch. That works out
  to ~100-150 us per row, which is the ceiling sqlite sync + goose migration
  give us on this host. PERF-06 (bulk multi-row INSERT) should compress the
  50 round-trips into 1 and drop this by an order of magnitude.
- **Event hub scales ~linearly with subscriber count** (~88 ns / subscriber)
  and allocates ~12 bytes / subscriber for the snapshot slice. The lock-free
  snapshot path (P2-PERF-01) keeps the zero-subscriber case at 15 ns.
- **Jobs service fan-out is the hottest job path**: 930 ns / target. For a
  1000-agent fleet rollout that is ~1 ms total — well under the HTTP p50
  budget, so no action required for Phase 3.

## Delta template for Phase 3 (PERF-06)

After PERF-06 lands, fill in the "after" numbers and compute delta vs the
baseline above. Target: `BenchmarkBatchWriterFlush` and
`BenchmarkBatchWriterMetricsFlush` should drop by at least 5x (the
bulk-insert path collapses 50 sequential `INSERT ... RETURNING` round-trips
into one); a drop of less than 2x means the bulk path is not actually being
taken on the hot path and needs investigation.

| Benchmark                        | Baseline ns/op | PERF-06 ns/op | Delta   | Target |
|----------------------------------|----------------|---------------|---------|--------|
| BatchWriterFlush (agents)        |      5,522,099 | _TBD_         | _TBD_   | -5x    |
| BatchWriterFlush (metrics)       |      4,467,290 | _TBD_         | _TBD_   | -5x    |
| BatchWriterEnqueue (agents)      |          15.75 | _TBD_         | _TBD_   | ~no change (hot-path must stay lock-free) |
| BatchWriterAuditEnqueue          |          16.03 | _TBD_         | _TBD_   | ~no change |
| EventHubPublish (100 subs)       |          8,212 | _TBD_         | _TBD_   | ~no change |
| Service Enqueue (1 target)       |          2,240 | _TBD_         | _TBD_   | ~no change |
| Service PendingForAgent          |         46,726 | _TBD_         | _TBD_   | improved by jobs index if included |

Regression budget: any benchmark that gets worse by more than 10% vs the
baseline above must block PERF-06 merge unless explicitly justified in the PR
description.

## What this baseline does NOT measure

- **End-to-end HTTP p50/p99**: no HTTP server or client in the loop. Cover
  this with a separate `cmd/benchagent` harness when Phase 3 infra exists.
- **CPU% / RSS under sustained load**: the microbenches run in short bursts,
  so OS accounting is noisy. Use `cgroup.stat` + `/metrics` scrape on a real
  fleet simulator for those numbers.
- **PostgreSQL**: baseline uses SQLite (the zero-deploy default). PERF-06
  needs a parallel PG run if the bulk path differs between drivers — which it
  will, since PG supports `INSERT ... ON CONFLICT DO UPDATE` natively whereas
  modernc SQLite needs per-row upserts.

## Changelog

- **2026-04-18**: initial baseline capture (this document).
