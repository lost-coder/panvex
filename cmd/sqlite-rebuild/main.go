// Command sqlite-rebuild печатает готовый goose-файл пересборки SQLite-таблицы
// (create/copy/drop/rename/index в crash-safe транзакционных рамках).
//
// Использование (одна таблица за вызов; для нескольких таблиц в одной
// миграции — объединить блоки между PRAGMA-строками вручную):
//
//	go run ./cmd/sqlite-rebuild \
//	    -table jobs \
//	    -create new_jobs.sql \
//	    -columns id,action,payload_json \
//	    -index 'CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);' \
//	    > db/migrations/sqlite/0059_jobs_add_check.sql
//
// Флаг -copy file.sql заменяет дефолтный INSERT..SELECT кастомным (backfill).
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, "; ") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func main() {
	table := flag.String("table", "", "имя пересобираемой таблицы (обязателен)")
	createFile := flag.String("create", "", "файл с CREATE TABLE <table>_new (...) (обязателен)")
	columns := flag.String("columns", "", "общие колонки через запятую (обязателен без -copy)")
	copyFile := flag.String("copy", "", "файл с кастомным INSERT..SELECT (вместо -columns)")
	var indexes stringSlice
	flag.Var(&indexes, "index", "CREATE INDEX statement (повторяемый флаг)")
	flag.Parse()

	if err := run(*table, *createFile, *columns, *copyFile, indexes); err != nil {
		fmt.Fprintln(os.Stderr, "sqlite-rebuild:", err)
		os.Exit(1)
	}
}

func run(table, createFile, columns, copyFile string, indexes []string) error {
	if table == "" || createFile == "" {
		return fmt.Errorf("-table and -create are required")
	}
	createSQL, err := os.ReadFile(createFile)
	if err != nil {
		return err
	}
	spec := Spec{Table: table, CreateSQL: string(createSQL), Indexes: indexes}
	switch {
	case copyFile != "":
		copySQL, err := os.ReadFile(copyFile)
		if err != nil {
			return err
		}
		spec.CopySQL = string(copySQL)
	case columns != "":
		for _, c := range strings.Split(columns, ",") {
			spec.Columns = append(spec.Columns, strings.TrimSpace(c))
		}
	default:
		return fmt.Errorf("either -columns or -copy is required")
	}

	script, err := Script([]Spec{spec})
	if err != nil {
		return err
	}
	_, err = os.Stdout.WriteString(script)
	return err
}
