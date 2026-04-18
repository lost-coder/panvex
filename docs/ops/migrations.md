# Schema migrations

The control-plane manages its database schema with
[`goose`](https://github.com/pressly/goose) and a set of versioned `.sql`
files under `db/migrations/{postgres,sqlite}/`. These files are the single
source of truth for the schema; the Go packages under
`internal/controlplane/storage/{postgres,sqlite}/` no longer contain any
inline DDL.

## How it works at runtime

1. The control-plane opens the database via `postgres.Open` or `sqlite.Open`.
2. Those functions call `Migrate(db)`, which:
   - Points goose at the per-dialect `embed.FS` exposed by
     `db/migrations/{postgres,sqlite}/embed.go`.
   - Calls `goose.SetDialect("postgres")` or `goose.SetDialect("sqlite3")`.
   - Runs `goose.UpContext` — this is a no-op when all versions are already
     recorded in the `goose_db_version` table.
3. The `PRAGMA foreign_keys = ON` pragma is still applied before `Migrate` on
   the SQLite side (per-connection; our pool size stays at 1).

Because goose records every applied version in `goose_db_version`, repeat
runs are idempotent and operators can audit exactly which DDL has run.

## Adding a new migration

1. Pick the next free four-digit prefix in the dialect's directory. Add one
   file per dialect; they share a version number so goose sees a single
   migration even though the SQL differs between Postgres and SQLite.

   ```
   db/migrations/postgres/0008_new_thing.sql
   db/migrations/sqlite/0008_new_thing.sql
   ```

2. Each file uses goose's annotation comments:

   ```sql
   -- +goose Up
   CREATE TABLE ...;
   CREATE INDEX ...;

   -- +goose Down
   DROP INDEX ...;
   DROP TABLE ...;
   ```

   For multi-statement DDL that contains embedded semicolons (PL/pgSQL
   functions, `DO` blocks, triggers), wrap the body in:

   ```sql
   -- +goose Up
   -- +goose StatementBegin
   CREATE FUNCTION ... $$ ... ; ... $$ LANGUAGE plpgsql;
   -- +goose StatementEnd
   ```

3. **Do**

   - Use `CREATE TABLE IF NOT EXISTS` / `CREATE INDEX IF NOT EXISTS` so a
     re-run (e.g. after a manual rollback) is tolerant.
   - Keep the Up/Down pair symmetric where possible.
   - Think about SQLite's column-type coercion when mirroring a Postgres
     migration — SQLite has no `TIMESTAMPTZ`; we use `INTEGER` unix epoch
     throughout.

4. **Don't**

   - Don't edit an already-released migration. Add a new one that alters
     the schema forward.
   - Don't mix DDL and large DML in the same file. Heavy data backfills
     belong in a dedicated `NNNN_backfill_*.sql` migration so Down is
     meaningful and retrying is clear.
   - Don't introduce cross-dialect drift: if you add a column in Postgres,
     also add it in SQLite with the same name (modulo `_unix` suffix for
     timestamps).

5. After saving the file, rebuild and run the schema tests:

   ```bash
   go test ./internal/controlplane/storage/...
   ```

   The SQLite migrate test verifies the `goose_db_version` ledger reaches
   the expected count; the PostgreSQL twin is gated on
   `PANVEX_POSTGRES_TEST_DSN` and runs in CI.

## Inspecting state

Use the `migrate-schema` subcommand on the control-plane binary:

```
panvex-control-plane migrate-schema status \
    -storage-driver sqlite -storage-dsn /var/lib/panvex/panvex.db
```

Output is goose's standard table of applied/pending versions, e.g.:

```
Applied At                  Migration
=======================================
Tue Apr 17 12:30:00 2026 -- 0001_init.sql
Tue Apr 17 12:30:00 2026 -- 0002_discovered_clients.sql
...
Pending                  -- 0008_new_thing.sql
```

`migrate-schema` (no subcommand) and `migrate-schema up` both run goose Up
— useful for one-off pre-deploy migration without booting the HTTP/gRPC
servers. The older `migrate-storage` subcommand handles cross-driver DATA
migration (copying rows from SQLite to Postgres); it is unrelated to
schema versioning.

## Migration stress testing (P2-TEST-03)

The fast unit tests in `internal/controlplane/storage/sqlite/migrate_test.go`
prove the chain runs on an *empty* database. That does not catch the class of
bug where an `ALTER TABLE` / `INSERT INTO ... SELECT` migration silently
drops data on a non-empty database, or where an index build blows up the
wall-clock budget when it has to scan millions of real rows.

Two tools cover that gap:

### 1. Go-level stress test (gated, runnable in-tree)

`migrate_stress_test.go` is compiled only when the `stress` build tag is set.
It seeds a fresh SQLite DB at schema 0001 with 10 000 agents and 100 000
metric_snapshots, runs the full `Migrate()` chain, then asserts:

- No row count drift on `fleet_groups`, `agents`, `audit_events`,
  `metric_snapshots` — every post-0001 migration is data-preserving.
- Migration 0011 renamed `audit_events.details_json` → `details` and
  `metric_snapshots.values_json` → `values` *and* carried the data across.
- The four P2-DB-02 indexes (`idx_jobs_status`,
  `idx_job_targets_agent_id`, `idx_metric_snapshots_captured_at`,
  `idx_enrollment_tokens_fleet_group_id`) exist after 0008.
- `goose_db_version` records all applied migrations (DF-20 regression guard).
- A second `Migrate()` is a no-op and does not touch row counts.

Run it:

```bash
go test -tags stress -count=1 -timeout 15m \
    ./internal/controlplane/storage/sqlite -run TestMigrateStress -v
```

Typical wall-clock on an idle laptop is under 60 seconds. If it takes
appreciably longer than previous runs on the same hardware, a migration
likely regressed to an O(N²) pattern — bisect against `git log db/migrations`.

### 2. Production-scale shell driver (off-tree)

`scripts/migration-test/` contains a heavier driver for pre-release
validation at true production scale (100k agents, 1M metric_snapshots by
default). Unlike the Go test it uses the actual `panvex-control-plane
migrate-schema up` subcommand, so it exercises the same code path operators
invoke:

```bash
bash scripts/migration-test/run.sh
```

Tunable via env vars (defaults shown):

```
SEED_AGENTS=100000
SEED_METRICS=1000000
SEED_CLIENTS=10000
SEED_JOBS=50000
SEED_AUDITS=500000
SEED_FLEET_GROUPS=32
SEED_DISCOVERED=0       # set non-zero to exercise 0010's partial unique index
```

Pass a path as the first argument to keep the seeded DB; otherwise the script
deletes it on exit:

```bash
KEEP_DB=1 bash scripts/migration-test/run.sh /tmp/panvex-migtest.db
```

When `sqlite3` CLI is on `$PATH` the script runs `PRAGMA integrity_check`,
`PRAGMA foreign_key_check`, prints per-table row counts, and asserts the
P2-DB-02 indexes plus the 0011 column rename. Without `sqlite3` the Go-level
test above is the authoritative check.

### When to run which

| Scenario | Run |
|---|---|
| New migration authored | Go stress test (takes <1 min, no external deps) |
| Release candidate, pre-deploy gate | Shell driver at full scale |
| Investigating a migration that took >1 min in production | Shell driver with `KEEP_DB=1` + `sqlite3` analysis |
| Routine CI | Neither — the default `migrate_test.go` is sufficient |

Both tools target SQLite only. PostgreSQL migration stress is covered by the
same-shaped tests in `internal/controlplane/storage/postgres/` gated on
`PANVEX_POSTGRES_TEST_DSN`; the production-scale driver for Postgres lives
in the deployment runbook, not this repo.
