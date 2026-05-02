package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver for migrate-schema
	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	_ "modernc.org/sqlite" // registers the "sqlite" driver for migrate-schema
)

// runMigrateSchema invokes goose against the configured storage backend.
//
//	migrate-schema          -> runs `goose up` (the default when opening the
//	                           store also does this, but the subcommand is
//	                           useful for first-time setup without booting the
//	                           full HTTP/gRPC servers).
//	migrate-schema status   -> prints the applied/pending migration list.
//
// The older `migrate-storage` subcommand handles cross-driver DATA migration
// and is unrelated to schema versioning; it is preserved unchanged.
func runMigrateSchema(args []string) error {
	sub := "up"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}

	flags := flag.NewFlagSet("migrate-schema", flag.ContinueOnError)
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	if err := flags.Parse(rest); err != nil {
		return err
	}

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}

	ctx := context.Background()

	switch storageConfig.Driver {
	case config.StorageDriverSQLite:
		db, err := sql.Open("sqlite", storageConfig.DSN)
		if err != nil {
			return err
		}
		defer db.Close()
		db.SetMaxOpenConns(1)
		if _, err := db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
			return err
		}
		switch sub {
		case "up":
			return sqlite.Migrate(db)
		case "status":
			return sqlite.Status(ctx, db)
		default:
			return fmt.Errorf("migrate-schema: unknown subcommand %q (expected 'up' or 'status')", sub)
		}
	case config.StorageDriverPostgres:
		db, err := sql.Open("pgx", storageConfig.DSN)
		if err != nil {
			return err
		}
		defer db.Close()
		switch sub {
		case "up":
			return postgres.Migrate(db)
		case "status":
			return postgres.Status(ctx, db)
		default:
			return fmt.Errorf("migrate-schema: unknown subcommand %q (expected 'up' or 'status')", sub)
		}
	default:
		return fmt.Errorf("unsupported storage driver %q", storageConfig.Driver)
	}
}
