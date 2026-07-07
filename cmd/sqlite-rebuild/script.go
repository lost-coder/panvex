package main

import (
	"errors"
	"fmt"
	"strings"
)

// Spec описывает пересборку одной таблицы по рецепту
// create/copy/drop/rename/index.
type Spec struct {
	// Table — имя пересобираемой таблицы (например "jobs").
	Table string
	// CreateSQL — полный CREATE TABLE <Table>_new (...) с новой схемой.
	CreateSQL string
	// Columns — общие колонки для дефолтного копирования
	// INSERT INTO <Table>_new (cols) SELECT cols FROM <Table>.
	// Игнорируется, если задан CopySQL.
	Columns []string
	// CopySQL — полный кастомный INSERT ... SELECT ... (с backfill'ом,
	// CASE-преобразованиями и т.п.), когда прямого копирования мало.
	CopySQL string
	// Indexes — CREATE INDEX statements, воссоздаваемые после RENAME
	// (DROP TABLE уносит индексы старой таблицы вместе с ней).
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

// Script собирает готовый goose-файл из одной или нескольких пересборок.
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
