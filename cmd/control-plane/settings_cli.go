package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"syscall"

	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/settings"
)

func runSettings(args []string) error { return runSettingsOut(os.Stdout, args) }

func runSettingsOut(out io.Writer, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: settings <list|get|set|reset> [args] -storage-driver <d> -storage-dsn <dsn>")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		return settingsList(out, rest)
	case "get":
		return settingsGet(out, rest)
	case "set":
		return settingsSet(out, rest)
	case "reset":
		return settingsReset(out, rest)
	default:
		return fmt.Errorf("unknown settings subcommand %q (want list|get|set|reset)", sub)
	}
}

// openSettingsStore builds an offline OperationalStore over the configured
// backend, mirroring the server's wiring. Caller must call the returned closer
// to release the store and the signal context. It returns the positional
// (non-flag) remainder so subcommands can read their <key>/<value> args.
func openSettingsStore(args []string) (*settings.OperationalStore, func() error, []string, error) {
	flags := flag.NewFlagSet("settings", flag.ContinueOnError)
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	if err := flags.Parse(args); err != nil {
		return nil, nil, nil, err
	}
	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	store, err := openStore(ctx, storageConfig)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	rawDBer, ok := store.(interface{ DB() *sql.DB })
	if !ok || rawDBer.DB() == nil {
		_ = store.Close()
		cancel()
		return nil, nil, nil, fmt.Errorf("settings: storage backend does not expose a *sql.DB")
	}
	ph := settings.PlaceholderDollar
	if storageConfig.Driver == config.StorageDriverSQLite {
		ph = settings.PlaceholderQ
	}
	op := settings.NewOperationalStoreRW(settings.NewDBStore(rawDBer.DB(), ph), settings.NewDBStore(rawDBer.DB(), ph))
	op.UseEnv(os.Environ())
	if err := op.Reload(ctx); err != nil {
		_ = store.Close()
		cancel()
		return nil, nil, nil, err
	}
	closer := func() error { cancel(); return store.Close() }
	return op, closer, flags.Args(), nil
}

func fieldByName(name string) (settings.FieldMeta, bool) {
	for _, f := range settings.AllFields() {
		if f.Name == name {
			return f, true
		}
	}
	return settings.FieldMeta{}, false
}

func settingsList(out io.Writer, args []string) error {
	op, closer, _, err := openSettingsStore(args)
	if err != nil {
		return err
	}
	defer func() { _ = closer() }()
	fields := settings.AllFields()
	sort.Slice(fields, func(i, j int) bool { return fields[i].Name < fields[j].Name })
	for _, f := range fields {
		if f.Class != settings.ClassOperational {
			// config/CLI-managed (env/config.toml) — show guidance, not a value.
			fmt.Fprintf(out, "%-40s [%s] managed via env/config (%s)\n", f.Name, f.Apply, f.Env)
			continue
		}
		val := op.RawByName(f.Name)
		if f.Secret && val != "" {
			val = "***"
		}
		src := op.Source(f.Name)
		over := ""
		if op.OverriddenByEnv(f.Name) {
			over = " (overridden by env " + f.Env + ")"
		}
		fmt.Fprintf(out, "%-40s [%s] = %q  source=%s%s\n", f.Name, f.Apply, val, src, over)
	}
	return nil
}

func settingsGet(out io.Writer, args []string) error {
	op, closer, positional, err := openSettingsStore(args)
	if err != nil {
		return err
	}
	defer func() { _ = closer() }()
	if len(positional) != 1 {
		return fmt.Errorf("usage: settings get -storage-driver <d> -storage-dsn <dsn> <key>")
	}
	key := positional[0]
	f, ok := fieldByName(key)
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	if f.Class != settings.ClassOperational {
		return fmt.Errorf("%q is managed via env/config (%s); not stored in the DB", key, f.Env)
	}
	val := op.RawByName(key)
	if f.Secret && val != "" {
		val = "***"
	}
	fmt.Fprintln(out, val)
	return nil
}

func settingsSet(out io.Writer, args []string) error {
	return fmt.Errorf("settings set: not implemented")
}

func settingsReset(out io.Writer, args []string) error {
	return fmt.Errorf("settings reset: not implemented")
}
