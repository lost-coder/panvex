# Миграции схемы

Два дерева goose-миграций — по одному на диалект:

- `postgres/` — источник схемы для sqlc (`sqlc.yaml` читает эту директорию);
- `sqlite/` — зеркальное дерево для SQLite-бэкенда.

## Squash 2026-07 (P9)

Миграции 0001..0058 обоих деревьев консолидированы в `0001_init.sql`
(SQLite — дампом sqlite_master, PostgreSQL — `pg_dump --schema-only`;
эквивалентность доказана schema_sync-тестом и нулевым sqlc-диффом).
БД, созданные до squash, несут goose-версии 1..58 и пропускают 0001.

## Правила (проверяются `storage/migrate/migration_parity_lint_test.go`)

1. **Номер новой миграции >= 0059** (`squashedHistoryCeiling+1`). Номера
   2..58 запрещены: на до-squash БД goose молча пропустил бы такой файл.
2. **Один номер = одно логическое изменение в обоих деревьях**, файлы
   называются одинаково: `NNNN_the_change.sql` и там и там.
3. Миграция, нужная только одному диалекту, содержит строку
   `-- dialect-only: <причина>`; её номер зарезервирован в обоих деревьях.
4. PostgreSQL: `CREATE INDEX` на существующей таблице — только
   `CONCURRENTLY` + `-- +goose NO TRANSACTION`
   (`TestPostgresIndexesUseConcurrently`).
5. SQLite: пересборка таблицы (ADD CONSTRAINT и прочее, чего SQLite не
   умеет через ALTER) — генерируй файл инструментом:

   ```bash
   go run ./cmd/sqlite-rebuild -table jobs -create new_jobs.sql \
       -columns id,action,payload_json \
       -index 'CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);' \
       > db/migrations/sqlite/0059_jobs_add_check.sql
   ```

   Рецепт (BEGIN до DROP, COMMIT после RENAME, PRAGMA вне транзакций)
   стережёт `TestSQLiteTableRebuildsAreTransactionWrapped`.
6. После любой правки `postgres/` — `sqlc generate` (+ коммит диффа
   `internal/dbsqlc/`); после любой правки схемы — зелёный
   `TestSchemaSyncPostgresMatchesSQLite` (нужен `PANVEX_POSTGRES_TEST_DSN`,
   в CI поднят сервис-контейнер).
7. Деструктивная миграция (DROP/TRUNCATE/DELETE по живым таблицам) —
   регистрируется в `storage/migrateguard.DestructiveMigrations` со своим
   goose-номером.
