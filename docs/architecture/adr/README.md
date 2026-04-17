# Architecture Decision Records

This directory captures the significant architectural decisions made
during the Panvex Phase 1 and Phase 2 remediation work. Each ADR is
numbered, dated, and frozen once accepted; later revisions come as new
ADRs that supersede earlier ones rather than edits in place.

## Format

Every ADR follows the same template:

- **Status** — `Proposed`, `Accepted`, `Superseded by ADR-NNN`, or
  `Deprecated`.
- **Date** — the day the decision became binding.
- **Task** — the remediation task or audit finding that forced the
  decision (e.g. `P1-SEC-05`, `P2-REL-06`).
- **Context** — what problem / finding forced us to decide.
- **Decision** — what we chose.
- **Alternatives considered** — what else we evaluated and why we
  rejected each.
- **Consequences** — the new constraints, obligations, and
  follow-ups the decision creates.

## Index

| # | Title | Task(s) |
|---|-------|---------|
| [001](001-agent-id-uuid-v7.md) | Agent ID — UUID v7 (not monotonic counter) | P1-SEC-05 (C5) |
| [002](002-release-signing-cosign-keyless.md) | Release signing — cosign keyless (Sigstore) | P1-SEC-02 |
| [003](003-retention-configurable-ttl.md) | Retention — configurable TTL with default 90d audit / 30d metrics | P2-REL-03, P2-REL-04, P2-REL-05 |
| [004](004-batch-writer-error-semantics.md) | Batch writer error semantics — retry transient, drop persistent, counter alerts | P2-REL-06 |
| [005](005-session-id-rotation.md) | Session ID rotation on login + privilege change | P2-SEC-01 |
| [006](006-migration-framework-goose.md) | Migration framework — goose with embedded SQL | P2-DB-01 |
| [007](007-async-audit-via-batch-writer.md) | Async audit writes via batch_writer 8th stream | P2-LOG-10 |
| [008](008-frontend-401-handling.md) | Frontend 401 handling — global event + AuthProvider redirect | P2-FE-02 |
| [009](009-adopt-discovered-client-mutex.md) | Adopt discovered client — global adoptMu (deferring Transact) | P2-LOG-03, P2-LOG-04 |
| [010](010-ui-kit-toast-primitive.md) | UI-kit Toast exported as primitive, web wires provider | P2-FE-03 |

## Adding a new ADR

1. Pick the next free number (`NNN`) and a short kebab-case slug.
2. Copy an existing ADR as a starting template.
3. Fill every section — "N/A" is rarely the right answer, especially
   for *Alternatives considered*.
4. Open a PR; reviewers check that the *Consequences* section is
   honest about follow-up work and that the linked task IDs exist in
   the remediation plan.
5. On merge, add the row to the index above. Do not renumber
   existing ADRs; if a decision is revisited, add a new ADR with
   status `Supersedes ADR-NNN` and update the older entry's status
   to `Superseded by ADR-MMM`.
