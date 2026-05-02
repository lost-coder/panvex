package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/config"
)

func runBootstrapAdmin(args []string) error {
	flags := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	username := flags.String("username", "admin", "Admin username")
	passwordFile := flags.String("password-file", os.Getenv("PANVEX_BOOTSTRAP_PASSWORD_FILE"),
		"Read admin password from file (preferred over -password for systemd LoadCredential and Docker secrets)")
	password := flags.String("password", os.Getenv("PANVEX_BOOTSTRAP_PASSWORD"),
		"Admin password (use -password-file in production)")
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	if err := flags.Parse(args); err != nil {
		return err
	}

	resolvedPassword := strings.TrimSpace(*password)
	if pf := strings.TrimSpace(*passwordFile); pf != "" {
		data, err := os.ReadFile(pf)
		if err != nil {
			return fmt.Errorf("read password-file %q: %w", pf, err)
		}
		resolvedPassword = strings.TrimRight(string(data), " \t\r\n")
	}

	if resolvedPassword == "" {
		return errors.New("password is required through -password / PANVEX_BOOTSTRAP_PASSWORD or -password-file / PANVEX_BOOTSTRAP_PASSWORD_FILE")
	}

	// Bind ctx to SIGINT/SIGTERM so a wedged DB lookup can be cancelled
	// with Ctrl-C instead of hanging the operator's terminal.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}

	store, err := openStore(storageConfig)
	if err != nil {
		return err
	}
	defer store.Close()

	existingUsers, err := store.ListUsers(ctx)
	if err != nil {
		return err
	}
	if len(existingUsers) > 0 {
		// S13: an operator running bootstrap-admin against a store that
		// already has users is either a misconfiguration (wrong DSN, wrong
		// flag) or an attempt to plant a privileged account on a live
		// system. Surface it loudly so operators paging on
		// alert=bootstrap_on_nonempty_db in their log pipeline notice it,
		// and return an error so no account is created.
		slog.Error(
			"bootstrap-admin invoked on non-empty storage",
			"alert", "bootstrap_on_nonempty_db",
			"storage_driver", storageConfig.Driver,
			"existing_user_count", len(existingUsers),
		)
		return errors.New("storage already contains users; refusing to bootstrap (see alert=bootstrap_on_nonempty_db)")
	}

	service := auth.NewServiceWithStore(store)
	_, _, err = service.BootstrapUser(ctx, auth.BootstrapInput{
		Username: *username,
		Password: resolvedPassword,
		Role:     auth.RoleAdmin,
	}, time.Now())
	if err != nil {
		return err
	}

	fmt.Printf("Admin user %q created.\n", *username)
	fmt.Printf("Storage driver: %s\n", storageConfig.Driver)
	if parsed, err := url.Parse(storageConfig.DSN); err == nil {
		fmt.Printf("Storage DSN: %s\n", parsed.Redacted())
	} else {
		fmt.Printf("Storage DSN: ***\n")
	}
	return nil
}
