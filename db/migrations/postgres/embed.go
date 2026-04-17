// Package pgmigrations exposes the PostgreSQL goose migrations as an embed.FS
// so the storage/postgres package (and any tooling that wants to run the same
// migrations out-of-band) can call goose.SetBaseFS without duplicating the
// .sql files. Keeping the migrations under db/migrations/ preserves their
// discoverability for humans and DBA tooling; this embed.go is the only Go
// bridge between the filesystem layout and the Go module.
package pgmigrations

import "embed"

//go:embed *.sql
var FS embed.FS
