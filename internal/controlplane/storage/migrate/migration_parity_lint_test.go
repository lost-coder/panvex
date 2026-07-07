package migrate_test

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	pgmigrations "github.com/lost-coder/panvex/db/migrations/postgres"
	sqlitemigrations "github.com/lost-coder/panvex/db/migrations/sqlite"
)

// squashedHistoryCeiling — последняя goose-версия, вошедшая в P9-squash
// (0001_init.sql консолидирует 0001..0058). БД, созданные ДО squash, несут
// в goose_db_version версии 1..58, а goose применяет только версии выше
// текущего максимума: новый файл с номером из диапазона 2..58 на такой БД
// молча НЕ применился бы. Поэтому новые миграции обязаны нумероваться
// строго выше ceiling — этот линт делает ошибку нумерации падением CI, а
// не продовым сюрпризом.
const squashedHistoryCeiling = 58

// dialectOnlyMarker помечает миграцию, осознанно существующую только в
// одном дереве (пример до squash: json_valid-CHECKи нужны только SQLite,
// JSONB валидирует сам). Номер при этом РЕЗЕРВИРУЕТСЯ в обоих деревьях —
// второе дерево не может занять его другой миграцией.
const dialectOnlyMarker = "-- dialect-only:"

var migrationNameRE = regexp.MustCompile(`^(\d{4})_(.+)\.sql$`)

type migrationFile struct {
	version int
	title   string // имя файла без NNNN_ и .sql
	body    string
}

// TestMigrationTreesAreParityLocked (закрывает класс дрейфа C9/H6 и
// «0052-коллизию»: до squash оба дерева держали РАЗНЫЕ миграции под одним
// номером). Правила:
//  1. 0001_init.sql существует в обоих деревьях;
//  2. номера 2..squashedHistoryCeiling запрещены (см. константу);
//  3. одинаковый номер в обоих деревьях => одинаковое название файла;
//  4. номер только в одном дереве => файл обязан содержать строку
//     `-- dialect-only: <причина>`.
func TestMigrationTreesAreParityLocked(t *testing.T) {
	sqliteTree := readMigrationTree(t, sqlitemigrations.FS, "sqlite")
	pgTree := readMigrationTree(t, pgmigrations.FS, "postgres")
	for _, problem := range checkMigrationParity(sqliteTree, pgTree) {
		t.Error(problem)
	}
}

func readMigrationTree(t *testing.T, fsys fs.FS, label string) map[int]migrationFile {
	t.Helper()
	tree := map[int]migrationFile{}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("%s: read dir: %v", label, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		m := migrationNameRE.FindStringSubmatch(e.Name())
		if m == nil {
			t.Errorf("%s: %s does not match NNNN_title.sql", label, e.Name())
			continue
		}
		// The regexp guarantees m[1] is exactly four digits, so Atoi cannot fail.
		version, _ := strconv.Atoi(m[1])
		if prev, dup := tree[version]; dup {
			t.Errorf("%s: duplicate version %04d (%s vs %s)", label, version, prev.title, m[2])
			continue
		}
		raw, err := fs.ReadFile(fsys, e.Name())
		if err != nil {
			t.Fatalf("%s: read %s: %v", label, e.Name(), err)
		}
		tree[version] = migrationFile{version: version, title: m[2], body: string(raw)}
	}
	return tree
}

func checkMigrationParity(sqliteTree, pgTree map[int]migrationFile) []string {
	var problems []string
	if _, ok := sqliteTree[1]; !ok {
		problems = append(problems, "sqlite: 0001_init.sql is missing")
	}
	if _, ok := pgTree[1]; !ok {
		problems = append(problems, "postgres: 0001_init.sql is missing")
	}

	versions := map[int]bool{}
	for v := range sqliteTree {
		versions[v] = true
	}
	for v := range pgTree {
		versions[v] = true
	}
	sorted := make([]int, 0, len(versions))
	for v := range versions {
		sorted = append(sorted, v)
	}
	sort.Ints(sorted)

	for _, v := range sorted {
		if v != 1 && v <= squashedHistoryCeiling {
			problems = append(problems, fmt.Sprintf(
				"version %04d is inside the squashed range 2..%d: pre-squash DBs already record it as applied and goose would silently skip the file; renumber to >= %04d",
				v, squashedHistoryCeiling, squashedHistoryCeiling+1))
			continue
		}
		s, inSQLite := sqliteTree[v]
		p, inPG := pgTree[v]
		switch {
		case inSQLite && inPG:
			if s.title != p.title {
				problems = append(problems, fmt.Sprintf(
					"version %04d means different things per dialect: sqlite=%q postgres=%q — same number MUST be the same logical change; give each change its own number",
					v, s.title, p.title))
			}
		case inSQLite:
			if !strings.Contains(s.body, dialectOnlyMarker) {
				problems = append(problems, fmt.Sprintf(
					"sqlite %04d_%s.sql has no postgres twin and no %q marker: add the twin or mark it dialect-only with a reason",
					v, s.title, dialectOnlyMarker))
			}
		case inPG:
			if !strings.Contains(p.body, dialectOnlyMarker) {
				problems = append(problems, fmt.Sprintf(
					"postgres %04d_%s.sql has no sqlite twin and no %q marker: add the twin or mark it dialect-only with a reason",
					v, p.title, dialectOnlyMarker))
			}
		}
	}
	return problems
}

// --- юнит-тесты правил на синтетических деревьях -----------------------

func mapTree(t *testing.T, files map[string]string) map[int]migrationFile {
	t.Helper()
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}
	return readMigrationTree(t, fsys, "synthetic")
}

func TestParityRuleSquashedRangeForbidden(t *testing.T) {
	s := mapTree(t, map[string]string{"0001_init.sql": "x", "0042_oops.sql": "x"})
	p := mapTree(t, map[string]string{"0001_init.sql": "x", "0042_oops.sql": "x"})
	problems := checkMigrationParity(s, p)
	if len(problems) != 1 || !strings.Contains(problems[0], "squashed range") {
		t.Fatalf("expected exactly one squashed-range problem, got %v", problems)
	}
}

func TestParityRuleTitleMismatch(t *testing.T) {
	s := mapTree(t, map[string]string{"0001_init.sql": "x", "0059_add_foo.sql": "x"})
	p := mapTree(t, map[string]string{"0001_init.sql": "x", "0059_add_bar.sql": "x"})
	problems := checkMigrationParity(s, p)
	if len(problems) != 1 || !strings.Contains(problems[0], "different things per dialect") {
		t.Fatalf("expected exactly one title-mismatch problem, got %v", problems)
	}
}

func TestParityRuleMissingTwinNeedsMarker(t *testing.T) {
	s := mapTree(t, map[string]string{"0001_init.sql": "x", "0059_sqlite_only.sql": "no marker here"})
	p := mapTree(t, map[string]string{"0001_init.sql": "x"})
	problems := checkMigrationParity(s, p)
	if len(problems) != 1 || !strings.Contains(problems[0], "dialect-only") {
		t.Fatalf("expected exactly one missing-twin problem, got %v", problems)
	}
}

func TestParityRuleMarkerSatisfiesMissingTwin(t *testing.T) {
	s := mapTree(t, map[string]string{
		"0001_init.sql":        "x",
		"0059_json_checks.sql": "-- dialect-only: JSONB validates on PG, SQLite needs json_valid CHECKs\nSELECT 1;",
	})
	p := mapTree(t, map[string]string{"0001_init.sql": "x"})
	if problems := checkMigrationParity(s, p); len(problems) != 0 {
		t.Fatalf("marker must satisfy the rule, got %v", problems)
	}
}

func TestParityRuleCleanTreesPass(t *testing.T) {
	s := mapTree(t, map[string]string{"0001_init.sql": "x", "0059_add_foo.sql": "x"})
	p := mapTree(t, map[string]string{"0001_init.sql": "x", "0059_add_foo.sql": "x"})
	if problems := checkMigrationParity(s, p); len(problems) != 0 {
		t.Fatalf("clean trees must pass, got %v", problems)
	}
}
