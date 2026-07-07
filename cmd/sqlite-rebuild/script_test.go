package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	// register the pure-Go SQLite driver under "sqlite" for database/sql
	_ "modernc.org/sqlite"
)

func TestScriptGolden(t *testing.T) {
	got, err := Script([]Spec{{
		Table: "jobs",
		CreateSQL: `CREATE TABLE jobs_new (
    id TEXT PRIMARY KEY,
    payload_json TEXT NOT NULL DEFAULT ''
        CHECK (payload_json = '' OR json_valid(payload_json))
);`,
		Columns: []string{"id", "payload_json"},
		Indexes: []string{"CREATE INDEX IF NOT EXISTS idx_jobs_payload ON jobs (payload_json);"},
	}})
	if err != nil {
		t.Fatalf("Script: %v", err)
	}

	want := `-- +goose Up
-- +goose NO TRANSACTION
-- Сгенерировано cmd/sqlite-rebuild. Рецепт пересборки таблицы (SQLite не
-- умеет ALTER TABLE ADD/DROP CONSTRAINT): create/copy/drop/rename/index.
-- Каждая пара DROP/RENAME — в собственном явном BEGIN/COMMIT, чтобы крэш
-- между ними не оставил таблицу удалённой-но-не-переименованной (guard:
-- migrate.TestSQLiteTableRebuildsAreTransactionWrapped). PRAGMA
-- foreign_keys переключается ВНЕ транзакций — SQLite запрещает менять его
-- внутри, поэтому весь файл идёт под NO TRANSACTION.

PRAGMA foreign_keys = OFF;

-- ─── jobs ───
BEGIN;

CREATE TABLE jobs_new (
    id TEXT PRIMARY KEY,
    payload_json TEXT NOT NULL DEFAULT ''
        CHECK (payload_json = '' OR json_valid(payload_json))
);

INSERT INTO jobs_new (id, payload_json)
SELECT id, payload_json FROM jobs;

DROP TABLE jobs;
ALTER TABLE jobs_new RENAME TO jobs;

CREATE INDEX IF NOT EXISTS idx_jobs_payload ON jobs (payload_json);

COMMIT;

PRAGMA foreign_keys = ON;

-- +goose Down
-- Обратная пересборка не автоматизируется: напиши обратный rebuild вручную
-- или оставь no-op, если даунгрейд не поддерживается.
SELECT 1;
`
	if got != want {
		t.Fatalf("golden mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestScriptValidation(t *testing.T) {
	if _, err := Script([]Spec{{Table: "jobs", CreateSQL: "CREATE TABLE wrong_name (id TEXT);", Columns: []string{"id"}}}); err == nil {
		t.Fatal("CreateSQL without <table>_new must be rejected")
	}
	if _, err := Script([]Spec{{Table: "jobs", CreateSQL: "CREATE TABLE jobs_new (id TEXT);"}}); err == nil {
		t.Fatal("Spec without Columns and without CopySQL must be rejected")
	}
	if _, err := Script(nil); err == nil {
		t.Fatal("empty spec list must be rejected")
	}
}

// TestScriptFunctional applies a generated script to a live DB with FK
// references, rows and an index, and proves the rebuild is lossless.
func TestScriptFunctional(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "rebuild.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	setup := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE parents (id TEXT PRIMARY KEY);`,
		`CREATE TABLE children (
			id TEXT PRIMARY KEY,
			parent_id TEXT NOT NULL REFERENCES parents (id) ON DELETE CASCADE,
			note TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE INDEX idx_children_parent ON children (parent_id);`,
		`INSERT INTO parents (id) VALUES ('p1');`,
		`INSERT INTO children (id, parent_id, note) VALUES ('c1', 'p1', 'keep'), ('c2', 'p1', 'also');`,
	}
	for _, stmt := range setup {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("setup %q: %v", stmt, err)
		}
	}

	script, err := Script([]Spec{{
		Table: "children",
		CreateSQL: `CREATE TABLE children_new (
    id TEXT PRIMARY KEY,
    parent_id TEXT NOT NULL REFERENCES parents (id) ON DELETE CASCADE,
    note TEXT NOT NULL DEFAULT '' CHECK (length(note) <= 64)
);`,
		Columns: []string{"id", "parent_id", "note"},
		Indexes: []string{"CREATE INDEX IF NOT EXISTS idx_children_parent ON children (parent_id);"},
	}})
	if err != nil {
		t.Fatalf("Script: %v", err)
	}
	// Отрезаем goose-аннотации: вне goose это обычный многостейтментный SQL.
	// Якорь — сам statement "PRAGMA foreign_keys = OFF", а не слово "PRAGMA"
	// из шапки-комментария, которое встречается раньше.
	body := script[strings.Index(script, "PRAGMA foreign_keys = OFF"):strings.Index(script, "-- +goose Down")]
	if _, err := db.ExecContext(ctx, body); err != nil {
		t.Fatalf("apply generated script: %v", err)
	}

	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM children`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("rows after rebuild: n=%d err=%v", n, err)
	}
	var ddl string
	if err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='children'`).Scan(&ddl); err != nil {
		t.Fatalf("read rebuilt DDL: %v", err)
	}
	if !strings.Contains(ddl, "length(note) <= 64") {
		t.Fatalf("rebuilt table lost the new CHECK: %s", ddl)
	}
	var idx string
	if err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='index' AND name='idx_children_parent'`).Scan(&idx); err != nil {
		t.Fatalf("index not recreated: %v", err)
	}
	rows, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign_key_check reported violations after rebuild")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("foreign_key_check rows: %v", err)
	}
}
