# Loadtest harness

The `internal/loadtest` package exercises the control-plane hot paths
under representative agent-fleet contention. It catches latency and
throughput regressions that don't surface in unit tests.

Two parallel modes:

| Mode | What runs | When | Wall-clock |
|---|---|---|---|
| **Correctness** | `go test ./internal/loadtest/...` (the `Test*` variants) | every PR via the `go-test` CI job | ~25s |
| **Bench smoke** | `go test -run '^$' -bench . -benchtime=1x ./internal/loadtest/...` | every PR via the `load-bench` CI job | ~25s |
| **Bench full** | `go test -run '^$' -bench . -benchtime=10s ./internal/loadtest/...` | operator-run, locally or nightly | ~5–10 min |

The correctness mode is a sanity gate (no panics, no errors, no audit
drops, lockouts fire when expected). The bench mode is the comparable
throughput / latency number you cite in PR descriptions.

## Reading benchmark output

`go test -bench` prints one line per benchmark, e.g.

```
BenchmarkAgentBurst-4   1   1140229714 ns/op   37608 enroll-p99-us   22144 heartbeat-p99-us   10974728 B/op   50925 allocs/op
BenchmarkJobFanout-4    1    111712784 ns/op    143.0 ingest-p99-us    1068 ack-p99-us   41.00 completion-ms   ...
BenchmarkLoginStorm-4   1  14698438950 ns/op   14246 login-p99-ms   ...
```

Columns left to right:

* **Iteration count**: with `-benchtime=1x` always 1; with `-benchtime=10s`
  Go runs as many iterations as fit in the budget — bigger = better
  statistical confidence.
* **`ns/op`**: total wall-clock time per scenario invocation. This is the
  rough "how long does one fleet-burst take" number.
* **Custom metrics** (emitted via `b.ReportMetric`):
    * `enroll-p99-us` / `heartbeat-p99-us` — agent burst latency.
    * `ingest-p99-us` / `ack-p99-us` / `completion-ms` — job fan-out.
    * `login-p99-ms` — login storm.
* **`B/op` / `allocs/op`**: bytes and allocations per scenario. Watch
  these for memory regressions; an Argon2id parameter bump is the
  expected reason for `BenchmarkLoginStorm` to balloon, anything else
  is suspect.

To compare two branches, run the bench with `-benchtime=10s -count=5`
on both and feed the output to
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat).

## Expected wall-clock budget

`go test -run '^$' -bench . -benchtime=1x ./internal/loadtest/...` on
a 4-core developer laptop or a `ubuntu-latest` GitHub runner:

* `BenchmarkAgentBurst` — ~1.1 s
* `BenchmarkJobFanout` — ~0.1 s
* `BenchmarkLoginStorm` — ~15 s (Argon2id-bound; the verify is the
  cost, not the harness)
* The other four (`BenchmarkClientsPutSequential`,
  `BenchmarkTelemetryEventsBulk`, `BenchmarkEventBusFanout`,
  `BenchmarkMigrateSQLite`) — sub-second each.

Total smoke run: **under 30 seconds**, well inside the existing
`load-bench` CI job's wall-clock budget.

The `Test*` correctness mode runs in the same shape but without the
inner-loop iteration: ~25 s end-to-end, the bulk again being the login
storm's 100 parallel Argon2id verifies.

## Adding a scenario

1. Pick a hot path the panel hits at fleet scale that isn't already
   covered. Examples not yet covered: presence GC under load,
   websocket fan-out, telemetry rollup query under N agents.
2. Create `internal/loadtest/<scenario>_test.go` with:
    * A package-doc header documenting **what's measured**, **how to
      run**, and **what's a regression** (mirror the existing files).
    * One private `runX(tb testing.TB) (...)` helper that does the
      work and is safe to call from both `*testing.T` and `*testing.B`.
    * `func TestX...(t *testing.T)` — assert correctness invariants.
      No latency budget — the assertion is "did it complete without
      errors / no audit drops / no rule violations".
    * `func BenchmarkX(b *testing.B)` — the load mode. Record the
      headline metric via `b.ReportMetric(value, "name-unit")`.
3. Reuse helpers in `harness.go` (SQLite store, fleet-group seed) and
   `sync.go` (`eventually`, `latencySamples`). Don't reimplement them.
4. Avoid hard-coded `time.Sleep` for synchronization — use
   `eventually(tb, timeout, cond)`.
5. CI picks up new `Benchmark*` automatically (the `load-bench` job
   uses `-bench .`). Same for new `Test*` (the loadtest correctness
   step uses `-count=1`).

## Constraints

* **No new dependencies.** The harness is pure Go + the existing
  control-plane packages. No vegeta, no k6, no http load tools.
* **No real network.** The control-plane HTTP/gRPC layers carry a
  lot of bootstrap weight (CSRF, mTLS, secret vault) that is exercised
  by the `internal/controlplane/server` test suite. The loadtest
  scenarios run against the publicly-exported services
  (`auth.Service`, `jobs.Service`) and the SQLite store directly —
  the same hot paths every HTTP route ultimately drives. This matches
  the existing `load_bench_test.go` pattern.
* **Deterministic sizing.** Scenario sizes are constants, not flags,
  so bench results compare across runs. Bump them in a branch when you
  need bigger numbers; don't add a `--fleet-size` knob.
