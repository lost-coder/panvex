# ADR-006: Migration framework — goose with embedded SQL

**Status:** Accepted
**Date:** 2026-04-18
**Task:** P2-DB-01

## Context

Phase 1 applied schema changes through ad-hoc Go code at startup: each
new table or column was expressed as a bare `CREATE TABLE IF NOT
EXISTS` or `ALTER TABLE … ADD COLUMN`, executed unconditionally on
every boot. There was no version tracking, no ordering guarantee
across concurrent writers, and no rollback path. P2-DB-01 documented
the consequences: `ALTER` statements that were not idempotent would
fail on second boot after a partial success, column additions in one
order worked but the reverse did not, and there was no principled way
to test a migration before it hit production. We needed a migration
framework with versioned up/down SQL, a dedicated migrations table,
and first-class support for both Postgres and SQLite (our two
supported backends).

## Decision

Adopt **goose** as the migration runner, configured to read embedded
SQL files via `embed.FS`. The layout is:

```
db/migrations/
  postgres/
    0001_initial_schema.sql
    0002_panel_settings.sql
    ...
  sqlite/
    0001_initial_schema.sql
    ...
```

Each file uses goose's `-- +goose Up` / `-- +goose Down` annotations
and, where needed, `-- +goose StatementBegin` for multi-statement
blocks. The control-plane binary runs `goose.Up(ctx, db, fsys,
dialect)` at startup before serving requests. A CLI subcommand
(`panvex migrate {up,down,status}`) exposes the same runner for
operators.

## Alternatives considered

- **golang-migrate.** Feature-equivalent for our needs and marginally
  more popular. Rejected because goose's embedded-FS ergonomics and
  single-dialect-per-file layout matched our existing split between
  `postgres/` and `sqlite/` SQL more naturally.
- **Hand-rolled migration table.** Rejected — this is essentially the
  broken status quo plus some bookkeeping. Every production-grade
  project eventually reinvents goose; starting there saves the cost.
- **ORM auto-migrate (GORM, ent, etc.).** Rejected: the project is
  deliberately SQL-forward via sqlc, and an ORM that rewrites schemas
  from Go structs would undermine that.

## Consequences

- All future schema changes require a new numbered migration file in
  both `postgres/` and `sqlite/` directories (or a guarded skip for
  dialect-specific features). CI enforces numbering monotonicity.
- Downgrade is supported in principle, but we do not guarantee data
  preservation across a `goose down` — migrations that drop columns
  cannot be reversed losslessly.
- The migration runner must complete before the HTTP server starts,
  which adds a small amount of boot latency on fresh databases. The
  no-op case (already migrated) is sub-millisecond.
