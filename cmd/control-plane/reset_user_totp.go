package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

func runResetUserTotp(args []string) error {
	flags := flag.NewFlagSet("reset-user-totp", flag.ContinueOnError)
	username := flags.String("username", "", "Username to reset TOTP for")
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *username == "" {
		return errors.New("username is required")
	}

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

	record, err := store.GetUserByUsername(ctx, *username)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("user %q not found", *username)
		}
		return err
	}

	service := auth.NewServiceWithStore(store)
	user, err := service.ResetTotp(ctx, record.ID)
	if err != nil {
		return err
	}

	if err := store.AppendAuditEvent(ctx, storage.AuditEventRecord{
		ID:        fmt.Sprintf("audit-cli-%d", time.Now().UTC().UnixNano()),
		ActorID:   "system",
		Action:    "auth.totp.reset_by_cli",
		TargetID:  user.ID,
		CreatedAt: time.Now().UTC(),
		Details: map[string]any{
			"username": user.Username,
		},
	}); err != nil {
		return err
	}

	fmt.Printf("TOTP reset for user %q.\n", user.Username)
	return nil
}
