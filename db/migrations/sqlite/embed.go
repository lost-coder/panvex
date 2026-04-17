// Package sqlitemigrations exposes the SQLite goose migrations as an embed.FS
// so the storage/sqlite package (and any tooling that wants to run the same
// migrations out-of-band) can call goose.SetBaseFS without duplicating the
// .sql files. See db/migrations/postgres/embed.go for the PostgreSQL twin.
package sqlitemigrations

import "embed"

//go:embed *.sql
var FS embed.FS
