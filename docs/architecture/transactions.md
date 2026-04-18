# Transactions, side effects, and the point-of-no-return

Status: Normative. All new control-plane mutations must follow the patterns
in this document. Existing code that violates these patterns is tracked and
refactored incrementally (see P2-LOG-01..P2-LOG-05, P3-LOG-01).

This document complements the storage contract in
`internal/controlplane/storage/store.go` and the transaction helper in
`storage.Store.Transact`. It covers:

- The "point of no return" pattern for mutations that combine persistence
  with an external side effect (job dispatch, third-party API call, event
  broadcast).
- When and how to use `Store.Transact`, including isolation level and retry
  semantics.
- The composition rule for nested transactions (`ErrNestedTransact`) and how
  internal helpers opt into an outer transaction via `beginInternalTx`.
- A code-review checklist distilled from the audit of P2 remediation items.

## 1. The "point of no return" pattern

A mutation frequently combines two steps:

1. A durable change to our database (inserts/updates/deletes in Postgres or
   SQLite).
2. An externally observable side effect: queueing a rollout job, calling a
   third-party API, pushing a WebSocket event, writing a file.

The canonical invariant is:

> Persist **before** you dispatch. Never dispatch **before** you persist.

### Why

If you dispatch first and then fail to persist, the side effect has already
happened with no durable record — retries will double-apply it, audit
history will be inconsistent, and agents may act on state the control-plane
denies ever creating.

If you persist first and then fail to dispatch, the state of the world is
recoverable: the row is there, and any reconciler or retry can redrive the
side effect from the persisted record.

### Examples in this codebase

- `adoptDiscoveredClient` (P2-LOG-03): the adoption flow writes the new
  `client` row and the `adoption_audit` row inside one `Store.Transact`,
  then — only after commit — queues the rollout job and emits the WebSocket
  notification. The in-memory serialization lock ensures two operators
  racing on the same discovered client do not double-insert before the
  transaction starts. Dispatch lives strictly outside the Transact closure.

- `deleteClient` (P2-LOG-01): the pre-fix code cancelled outstanding jobs
  for a client **before** deleting the DB row. On failure midway, the jobs
  were gone but the client remained — a classic inverted order. The fix
  rewrote the flow to delete (and cascade) inside the transaction, then
  fire the "clients.deleted" event only after commit.

- `http_recovery` agent certificate recovery (P1-SEC-07): grants are
  issued inside a `Store.Transact` that atomically marks the grant
  consumed and writes the new agent certificate. Only after the
  transaction commits does the handler write the signed bundle back in
  the response body. A client that crashes mid-response may retry, but
  the grant is already consumed and the handler refuses reissue —
  preferable to double-issuing two live certificates.

### Common anti-patterns

- Persisting, then broadcasting, then adding a second write inside the same
  function. The second write is no longer protected by the transaction and
  is prone to partial-success bugs.
- Dispatching a job before commit on the assumption that "the job is
  idempotent, so it is fine." The job may execute before your commit lands
  and observe a row that does not yet exist.
- Using `defer` to fire the side effect: `defer queueJob()` runs even on
  error. Always gate dispatch on the Transact's return value.

## 2. `Store.Transact` usage

`Store.Transact(ctx, fn)` executes `fn` inside a DB transaction with these
semantics:

- **Isolation:** `READ COMMITTED` on Postgres, default on SQLite. If you
  need stronger isolation (for example, a read-modify-write against a
  counter that must never lose updates), route through one of the
  explicitly-named helpers (e.g. `TransactSerializable`) or take an
  application-level lock before entering the closure.
- **Retry on serialization failure:** on Postgres, a `40001` (serialization
  failure) or `40P01` (deadlock) aborts the transaction and the helper
  retries up to 3 times with exponential backoff (50ms, 100ms, 200ms).
  SQLite uses `SQLITE_BUSY` handling with the same retry budget.
- **No nested transactions:** calling `Transact` from inside a closure that
  is already running inside a `Transact` returns `ErrNestedTransact`. Do
  not catch this error — it indicates a bug in the caller.
- **Context cancellation:** if `ctx` is cancelled during the closure, the
  helper rolls back and returns `ctx.Err()`. Do not catch and retry on
  cancellation.

### When to use Transact

Use `Transact` when a single logical operation must be all-or-nothing
across two or more writes. The typical shape is "write row A, write row B,
and if either fails neither must exist."

### When NOT to use Transact

- For single-statement writes: the driver already runs them atomically.
- For long-running work such as external HTTP calls or cryptographic
  operations on large inputs: do the work first, then open a short-lived
  transaction to commit the result. Holding a transaction open across
  network I/O starves connection pool capacity and increases the chance
  of serialization retries.
- For work that must remain observable even on partial failure (for
  example, an audit log that must record the attempt even if the mutation
  rolls back). Those writes live in a separate transaction or, for
  audit, go through the fire-and-forget `batch_writer`.

## 3. Nested transactions and `beginInternalTx`

`ErrNestedTransact` is returned when `Store.Transact` is called from a
caller already inside a Transact closure. This is unconditional: chi does
not grant us an escape hatch, and we do not want one — nested transactions
in PostgreSQL require SAVEPOINT which we deliberately avoid.

Lower-level storage methods that need to participate in an outer
transaction accept an optional `*sql.Tx` via `beginInternalTx`:

```go
tx, commit, err := s.beginInternalTx(ctx)
if err != nil {
    return err
}
defer commit(&err) // rolls back on non-nil err, commits otherwise
```

`beginInternalTx` returns the caller's transaction handle when one is
already in progress (via context inspection), otherwise it opens a fresh
transaction and returns a `commit` function that finalizes it. Callers
that are themselves invoked from within a Transact closure therefore
transparently compose without triggering `ErrNestedTransact`.

Top-level methods in `storage.Store` that accept a context should check if
the context already carries a transaction handle before opening a new one.
If you write a new top-level storage method and you expect it to be called
both standalone and as part of a Transact closure, use `beginInternalTx`.
If you expect it to be called only standalone, use `Store.Transact`
directly and document the restriction in the method comment.

## 4. Code-review checklist

When reviewing a PR that touches mutations in `internal/controlplane`,
verify:

1. **Order of operations.** Every DB write that has an external side
   effect is committed before the side effect is dispatched. No
   `queueJob`, `NotifyWS`, `http.Post`, or similar call appears before a
   `Store.Transact` return.
2. **Single transaction per logical operation.** Two writes that must be
   all-or-nothing are wrapped in one `Transact`, not two sequential
   `Transact` calls.
3. **No long-running work inside `Transact`.** Cryptographic key
   generation, outbound HTTP, and file I/O happen before or after the
   transaction, never inside.
4. **Serialization locks precede Transact, not replace it.** If two
   concurrent requests on the same key must be serialized, an in-memory
   lock (e.g. `singleflight` or a keyed mutex) is acquired *before*
   entering `Transact`. The lock is a correctness optimization, not a
   substitute for DB uniqueness constraints.
5. **Unique constraints exist for every business-unique field.** An
   in-memory check ("does this name already exist") is a race. The
   definitive check is a `UNIQUE` index, and the code handles
   `storage.ErrConflict` as a user-visible 409.
6. **Retry semantics are documented.** If a handler relies on
   `Transact`'s automatic retry on `40001`, that reliance is commented.
   If the handler must surface the retry to the user (for example,
   because 3 retries are not enough), it catches `storage.ErrRetry` and
   reports 503.
7. **Context propagation.** Every storage call inside the closure
   receives the same `ctx` that `Transact` received. No
   `context.Background()` inside a Transact closure.
8. **No deferred side effects.** `defer queueJob()` or
   `defer broadcast()` is forbidden because `defer` fires on error paths
   too. Side effects are explicit, gated on `Transact` returning `nil`.
9. **Audit events are durable.** Mutations append an audit event
   either inside the same transaction (when the audit is part of the
   business invariant, e.g. certificate recovery) or via the
   fire-and-forget batch writer after commit (for routine CRUD).
   Audit is never lost on restart — see P3-REL-01 for the shutdown
   ordering that guarantees the batch writer drains.
10. **Tests cover the partial-failure path.** For any new Transact, at
    least one test simulates a DB error mid-closure and asserts the
    side effect is *not* dispatched.

## 5. References

- `internal/controlplane/storage/store.go` — `Store.Transact` contract.
- `internal/controlplane/storage/errors.go` — `ErrNestedTransact`,
  `ErrConflict`, `ErrRetry`.
- `internal/controlplane/server/http_clients.go` —
  `handleAdoptDiscoveredClient`, `handleDeleteClient`.
- `internal/controlplane/server/http_recovery.go` —
  agent certificate recovery grant consumption.
- `docs/architecture/adr/` — architectural decision records for
  storage topology.
