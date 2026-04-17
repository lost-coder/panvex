# ADR-001: Agent ID — UUID v7 (not monotonic counter)

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P1-SEC-05 (audit finding C5)

## Context

The original control-plane assigned agent IDs from a process-local monotonic
counter initialized to zero at startup. A server restart reset the counter to
zero, so newly-adopted agents after the restart would receive IDs that had
previously been issued to earlier (now-disconnected) agents. The audit flagged
this under finding C5 as a correctness and security hazard: cross-session ID
collision could cause metrics/audit entries to be misattributed, session
binding to be silently stolen on reconnect, and replay-style confusion where a
disconnected agent's residual state was merged into a new agent's stream.
Persistence of the counter would have worked but introduced disk-write hot
paths, crash-recovery edge cases (torn writes, fsync semantics across
Postgres and SQLite backends), and yet another schema migration.

## Decision

Agent IDs are UUID v7 values generated in-process at registration time.
UUID v7 encodes a millisecond Unix timestamp in the high bits and
cryptographically-random bits in the low portion, giving us:

- Time-sortable keys that still index well in B-trees.
- Collision resistance without coordinating across restarts or replicas.
- No persistent "next-id" state, no fsync, no migration.

The generator lives in `internal/agent/id.go` and is used uniformly by
adopt and merge paths.

## Alternatives considered

- **Persisted monotonic counter.** Rejected: added a new coupling between
  every adopt and the storage layer, complicated the bootstrap path, and
  required per-backend semantics (Postgres sequence vs SQLite row). The
  benefit (small integer IDs) was not worth the complexity.
- **Random UUID v4.** Rejected: loses time-ordering, which we rely on for
  audit pagination and TTL sweeps. B-tree locality is measurably better
  with v7.
- **Snowflake / ULID.** Rejected: ULID is close to UUID v7 but less
  widely supported in Go tooling; Snowflake requires machine-ID
  coordination that UUID v7 sidesteps entirely.

## Consequences

- Agent IDs are 128-bit strings in logs, URLs, and the dashboard. We
  accept the display length in exchange for safety.
- Any caller that previously assumed integer IDs (test helpers,
  grep-friendly fixtures) was updated. New call sites must treat
  the ID as opaque.
- When we later add cross-region replication, UUID v7 is already
  collision-safe across writers; no additional allocation scheme is
  needed.
