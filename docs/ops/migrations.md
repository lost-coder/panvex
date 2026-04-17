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
