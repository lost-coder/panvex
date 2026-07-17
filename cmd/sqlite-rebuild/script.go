package main

import (
	"errors"
	"fmt"
	"strings"
)

// Spec describes the rebuild of one table following the
// create/copy/drop/rename/index recipe.
type Spec struct {
	// Table — the name of the table being rebuilt (e.g. "jobs").
	Table string
	// CreateSQL — the full CREATE TABLE <Table>_new (...) with the new schema.
	CreateSQL string
	// Columns — the shared columns for the default copy
	// INSERT INTO <Table>_new (cols) SELECT cols FROM <Table>.
	// Ignored if CopySQL is set.
	Columns []string
	// CopySQL — the full custom INSERT ... SELECT ... (with backfill,
	// CASE conversions, etc.) when a direct copy is not enough.
	CopySQL string
	// Indexes — CREATE INDEX statements, recreated after RENAME
	// (DROP TABLE takes the old table's indexes down with it).
	Indexes []string
}

const scriptHeader = `-- +goose Up
-- +goose NO TRANSACTION
-- Сгенерировано cmd/sqlite-rebuild. Рецепт пересборки таблицы (SQLite не
-- умеет ALTER TABLE ADD/DROP CONSTRAINT): create/copy/drop/rename/index.
-- Каждая пара DROP/RENAME — в собственном явном BEGIN/COMMIT, чтобы крэш
-- между ними не оставил таблицу удалённой-но-не-переименованной (guard:
-- migrate.TestSQLiteTableRebuildsAreTransactionWrapped). PRAGMA
-- foreign_keys переключается ВНЕ транзакций — SQLite запрещает менять его
-- внутри, поэтому весь файл идёт под NO TRANSACTION.

PRAGMA foreign_keys = OFF;
`

const scriptFooter = `
PRAGMA foreign_keys = ON;

-- +goose Down
-- Обратная пересборка не автоматизируется: напиши обратный rebuild вручную
-- или оставь no-op, если даунгрейд не поддерживается.
SELECT 1;
`

// Script assembles a ready-made goose file from one or several rebuilds.
func Script(specs []Spec) (string, error) {
	if len(specs) == 0 {
		return "", errors.New("at least one Spec is required")
	}
	var b strings.Builder
	b.WriteString(scriptHeader)
	for _, s := range specs {
		block, err := rebuildBlock(s)
		if err != nil {
			return "", err
		}
		b.WriteString(block)
	}
	b.WriteString(scriptFooter)
	return b.String(), nil
}

func rebuildBlock(s Spec) (string, error) {
	if strings.TrimSpace(s.Table) == "" {
		return "", errors.New("Spec.Table is required")
	}
	newName := s.Table + "_new"
	if !strings.Contains(s.CreateSQL, newName) {
		return "", fmt.Errorf("Spec.CreateSQL for %q must create %q (got: %.60s...)", s.Table, newName, s.CreateSQL)
	}
	copyStmt := strings.TrimSpace(s.CopySQL)
	if copyStmt == "" {
		if len(s.Columns) == 0 {
			return "", fmt.Errorf("Spec for %q needs Columns or CopySQL", s.Table)
		}
		cols := strings.Join(s.Columns, ", ")
		copyStmt = fmt.Sprintf("INSERT INTO %s (%s)\nSELECT %s FROM %s;", newName, cols, cols, s.Table)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n-- ─── %s ───\nBEGIN;\n\n", s.Table)
	b.WriteString(ensureSemicolon(s.CreateSQL))
	b.WriteString("\n\n")
	b.WriteString(ensureSemicolon(copyStmt))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "DROP TABLE %s;\n", s.Table)
	fmt.Fprintf(&b, "ALTER TABLE %s RENAME TO %s;\n", newName, s.Table)
	if len(s.Indexes) > 0 {
		b.WriteString("\n")
		for _, idx := range s.Indexes {
			b.WriteString(ensureSemicolon(idx))
			b.WriteString("\n")
		}
	}
	b.WriteString("\nCOMMIT;\n")
	return b.String(), nil
}

func ensureSemicolon(stmt string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(stmt), ";")
	return trimmed + ";"
}
