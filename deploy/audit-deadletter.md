# Audit dead-letter spool

Audit events are normally appended to the `audit_events` table as part of a
SHA-256 hash chain (see `verify-audit-chain`). When the store write for a
batch of audit events **permanently fails** (a non-retriable error — not a
transient DB hiccup that the batch writer's own retry logic already
absorbs), the batch writer spools the affected events to a local file
instead of dropping them silently:

```
data/audit-deadletter/audit-events.jsonl
```

Each line is a JSON envelope: the wall-clock time the event was spooled,
plus the original audit record (id, actor, action, target, timestamp,
details).

## Reading the spool

Nothing outside tests reads this file automatically. Use the CLI
subcommand to triage it:

```bash
panvex-control-plane audit-deadletter \
  -path data/audit-deadletter/audit-events.jsonl \
  -format table   # or: -format json
```

- `-path` defaults to `data/audit-deadletter/audit-events.jsonl`.
- A missing file is not an error — it means nothing was ever
  dead-lettered — and prints a friendly message.
- `table` prints one row per event (dead-lettered-at, id, action, actor,
  subject) plus a summary count.
- `json` re-emits the parsed envelopes one per line for scripting
  (`jq`, log shipping, etc.).
- A torn or corrupted line (the spool is appended to mid-degradation, so a
  crash can leave a partial last line) is reported as
  `line N: unparseable: <err>` and skipped — it does not abort reading the
  rest of the file.

## Why there is no auto-import

Records in this spool are **outside** the audit hash chain that
`verify-audit-chain` checks: they never reached the store, so they were
never chained in the first place. There is deliberately no tool to replay
them back into `audit_events`, because:

- Audit event sequence IDs restart from the persisted maximum on boot.
  Replaying a spooled event would either collide with an ID that has
  since been reissued, or get inserted out of chronological order —
  either way breaking chain continuity for every event that follows it.
- The dead-letter spool is a forensic/triage artifact, not a durable
  queue. If store writes are permanently failing, that is an operational
  incident to fix (storage outage, disk full, permissions) — the fix is
  to restore write capability, not to backfill the gap after the fact.

Treat entries in this file as: "these audit events happened, but could not
be durably recorded and are not part of the verifiable chain." Investigate
why the store rejected them, and reference the spooled record manually
(ticket, incident log) if you need to reconstruct what occurred.
