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

	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/server"
)

// runRotateCA mints a fresh control-plane CA and overwrites the stored record.
// All enrolled agents will lose trust and MUST re-enroll after this operation.
// The --confirm flag is mandatory to prevent accidental fleet invalidation.
func runRotateCA(args []string) error {
	flags := flag.NewFlagSet("rotate-ca", flag.ContinueOnError)
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	encryptionKeyFile := flags.String("encryption-key-file", "", "Path to file containing PANVEX_ENCRYPTION_KEY (optional)")
	encryptionKeyStdin := flags.Bool("encryption-key-stdin", false, "Read PANVEX_ENCRYPTION_KEY from stdin (optional)")
	confirm := flags.Bool("confirm", false, "Required: acknowledge that all agents must re-enroll after CA rotation")
	flags.Usage = func() {
		_, _ = fmt.Fprintf(flags.Output(), "Usage: panvex-control-plane rotate-ca --confirm [flags]\n\n")
		_, _ = fmt.Fprintf(flags.Output(), "Mints a fresh control-plane CA certificate and overwrites the stored record.\n\n")
		_, _ = fmt.Fprintf(flags.Output(), "WARNING: every enrolled agent will lose trust and must re-enroll after rotation.\n\n")
		_, _ = fmt.Fprintf(flags.Output(), "Workflow:\n")
		_, _ = fmt.Fprintf(flags.Output(), "  1. Stop the panel.\n")
		_, _ = fmt.Fprintf(flags.Output(), "  2. Run this command with --confirm.\n")
		_, _ = fmt.Fprintf(flags.Output(), "  3. Restart the panel.\n")
		_, _ = fmt.Fprintf(flags.Output(), "  4. Re-enroll every agent (re-run the install command on each node).\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	if !*confirm {
		fmt.Fprintln(os.Stderr, "WARNING: rotating the CA certificate will invalidate every enrolled agent.")
		fmt.Fprintln(os.Stderr, "All agents must re-enroll after this operation.")
		fmt.Fprintln(os.Stderr, "Re-run with --confirm to proceed.")
		return errors.New("--confirm flag required to acknowledge fleet re-enrollment")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}
	store, err := openStore(ctx, storageConfig)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	encryptionKey, err := resolveEncryptionKey(*encryptionKeyFile, *encryptionKeyStdin)
	if err != nil {
		return err
	}

	if err := server.RotateCertificateAuthority(ctx, store, time.Now(), encryptionKey); err != nil {
		return fmt.Errorf("rotate CA: %w", err)
	}

	fmt.Println("CA certificate rotated successfully.")
	fmt.Println("IMPORTANT: every enrolled agent must re-enroll. Re-run the install command on each node.")
	return nil
}
