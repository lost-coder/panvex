# ADR-009: Adopt discovered client — global adoptMu (deferring Transact)

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P2-LOG-03, P2-LOG-04

## Context

When two agents registered near-simultaneously with the same
discovery fingerprint, the control-plane could interleave the
read-then-write steps of the adopt-or-merge path. P2-LOG-03 and
P2-LOG-04 identified the classic TOCTOU race: goroutine A reads
"no existing record," goroutine B reads "no existing record,"
both insert a new row, and the fingerprint-uniqueness invariant is
violated. The proper long-term fix is a storage-layer
`Store.Transact` abstraction that wraps the adopt decision in a
single serializable transaction (tracked under P2-ARCH-01), but
that work has a wide blast radius — it touches every storage path
— and we needed an immediate remediation that could ship in P2.

## Decision

Introduce `Server.adoptMu sync.Mutex` as a short-term process-wide
serialization point for the adopt and merge flows. Every entry
into `adoptDiscoveredClient`, `mergeClient`, and their call sites
takes the mutex for the duration of the decision. The critical
section is small (single-digit milliseconds on a healthy DB) and
contention is low in practice — agent registrations are bursty at
deploy time but otherwise sparse — so the mutex is an acceptable
hammer.

Call sites are documented with a comment pointing at this ADR and
at P2-ARCH-01 so the eventual `Store.Transact` migration has a
clear TODO list.

## Alternatives considered

- **Per-fingerprint mutex map** (`map[fingerprint]*sync.Mutex`
  guarded by a `sync.Map` or a top-level mutex). Rejected as
  premature optimisation: the contention profile does not justify
  the complexity of a mutex registry, and getting cleanup right
  (so the map does not grow unboundedly) adds yet another edge
  case.
- **Database-level advisory lock** (`pg_advisory_xact_lock`).
  Would work for Postgres but has no SQLite equivalent, and we
  would end up with two different implementations — the exact
  outcome the `Store.Transact` abstraction aims to avoid.
- **Optimistic retry with unique constraint.** The storage schema
  already enforces uniqueness, so a losing writer gets a
  constraint-violation error it can convert into a "merge" path.
  We rejected this approach for the interim: it makes the adopt
  path inherently branchy and log-noisy, and the proper fix
  (ADR-pending P2-ARCH-01) supersedes it.

## Consequences

- Adopt throughput is bounded by a single mutex. At current fleet
  sizes this is invisible; at >10k agents adopting per second it
  will become the bottleneck and `Store.Transact` must ship.
- The mutex is scoped to one control-plane process. A
  multi-replica deployment does not yet exist, but when it does
  the mutex becomes insufficient and `Store.Transact` again
  becomes a hard requirement.
