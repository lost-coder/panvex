package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/config"
	"github.com/panvex/panvex/internal/controlplane/server"
	"github.com/panvex/panvex/internal/controlplane/storage"
	storagemigrate "github.com/panvex/panvex/internal/controlplane/storage/migrate"
	"github.com/panvex/panvex/internal/controlplane/storage/postgres"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
	"github.com/panvex/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// Build-time version information, injected via -ldflags.
var (
	Version   = "dev"
	CommitSHA = "unknown"
	BuildTime = "unknown"
)

type serveConfig struct {
	ConfigPath           string
	ConfigManagedRuntime bool
	HTTPAddr             string
	HTTPRootPath         string
	GRPCAddr             string
	RestartMode          string
	TLSMode              string
	TLSCertFile          string
	TLSKeyFile           string
	Storage              config.StorageConfig
	TrustedProxyCIDRs    []*net.IPNet
	EncryptionKey        string
	LogLevel             string
}

const restartExitCode = 78

var errPanelRestartRequested = errors.New("panel restart requested")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errPanelRestartRequested) {
			os.Exit(restartExitCode)
		}
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "-version" {
		fmt.Printf("panvex-control-plane %s (commit: %s, built: %s)\n", Version, CommitSHA, BuildTime)
		os.Exit(0)
	}
	if len(args) > 0 && args[0] == "bootstrap-admin" {
		return runBootstrapAdmin(args[1:])
	}
	if len(args) > 0 && args[0] == "migrate-storage" {
		return runMigrateStorage(args[1:])
	}
	if len(args) > 0 && args[0] == "reset-user-totp" {
		return runResetUserTotp(args[1:])
	}
	if len(args) > 0 && args[0] == "self-update" {
		return runSelfUpdate(args[1:])
	}

	return runServe(args)
}

func runServe(args []string) error {
	options, err := parseServeConfig(args)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLogLevel(options.LogLevel)}))
	slog.SetDefault(logger)

	store, err := openStore(options.Storage)
	if err != nil {
		return err
	}
	defer store.Close() // closed after api.Close() stops background workers

	panelRuntime, err := resolvePanelRuntime(options)
	if err != nil {
		return err
	}
	restartRequests := make(chan struct{}, 1)

	api := server.New(server.Options{
		Now:          time.Now,
		Store:        store,
		Logger:       logger,
		UIFiles:      embeddedUIFiles(),
		PanelRuntime: panelRuntime,
		TrustedProxyCIDRs: options.TrustedProxyCIDRs,
		EncryptionKey: options.EncryptionKey,
		Version:   Version,
		CommitSHA: CommitSHA,
		BuildTime: BuildTime,
		RequestRestart: func() error {
			select {
			case restartRequests <- struct{}{}:
			default:
			}
			return nil
		},
	})
	defer api.Close()
	if err := api.StartupError(); err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              panelRuntime.HTTPListenAddress,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	grpcListener, err := net.Listen("tcp", panelRuntime.GRPCListenAddress)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(api.GRPCTLSConfig())))
	gatewayrpc.RegisterAgentGatewayServer(grpcServer, api)

	httpErrors := make(chan error, 4)
	go func() {
		slog.Info("http server listening", "address", panelRuntime.HTTPListenAddress)
		if panelRuntime.TLSMode == "direct" {
			httpErrors <- httpServer.ListenAndServeTLS(panelRuntime.TLSCertFile, panelRuntime.TLSKeyFile)
			return
		}
		httpErrors <- httpServer.ListenAndServe()
	}()

	slog.Info("grpc server listening", "address", panelRuntime.GRPCListenAddress)
	go func() {
		httpErrors <- grpcServer.Serve(grpcListener)
	}()

	for {
		select {
		case <-restartRequests:
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)

			_ = httpServer.Shutdown(shutdownContext)
			grpcServer.GracefulStop()
			_ = grpcListener.Close()
			cancel()
			return errPanelRestartRequested
		case err := <-httpErrors:
			if errors.Is(err, http.ErrServerClosed) {
				continue
			}
			return err
		}
	}
}

func parseServeConfig(args []string) (serveConfig, error) {
	flags := flag.NewFlagSet("control-plane", flag.ContinueOnError)
	configPath := flags.String("config", strings.TrimSpace(os.Getenv("PANVEX_CONFIG")), "Path to runtime config.toml")
	httpAddr := flags.String("http-addr", ":8080", "HTTP listen address")
	grpcAddr := flags.String("grpc-addr", ":8443", "gRPC listen address")
	restartMode := flags.String("restart-mode", config.RestartModeDisabled, "Panel restart mode (disabled or supervised)")
	storageDriver := flags.String("storage-driver", "", "Persistent storage backend driver")
	storageDSN := flags.String("storage-dsn", "", "Persistent storage backend DSN")
	trustedProxyCIDRs := flags.String("trusted-proxy-cidrs", "", "Comma-separated trusted proxy CIDRs for X-Forwarded-For (e.g. 172.16.0.0/12,10.0.0.0/8)")
	encryptionKey := flags.String("encryption-key", strings.TrimSpace(os.Getenv("PANVEX_ENCRYPTION_KEY")), "Passphrase for encrypting the CA private key at rest (env: PANVEX_ENCRYPTION_KEY)")
	logLevel := flags.String("log-level", "info", "Log level: debug, info, warn, error")
	if err := flags.Parse(args); err != nil {
		return serveConfig{}, err
	}

	parsedCIDRs, err := parseCIDRList(*trustedProxyCIDRs)
	if err != nil {
		return serveConfig{}, fmt.Errorf("invalid -trusted-proxy-cidrs: %w", err)
	}

	explicitLegacyFlags := make(map[string]bool)
	flags.Visit(func(currentFlag *flag.Flag) {
		explicitLegacyFlags[currentFlag.Name] = true
	})

	if strings.TrimSpace(*configPath) != "" {
		for _, flagName := range []string{"http-addr", "grpc-addr", "restart-mode", "storage-driver", "storage-dsn"} {
			if explicitLegacyFlags[flagName] {
				return serveConfig{}, fmt.Errorf("flag -%s cannot be used together with -config", flagName)
			}
		}

		configuration, err := config.LoadControlPlaneConfig(*configPath)
		if err != nil {
			return serveConfig{}, err
		}

		return serveConfig{
			ConfigPath:           strings.TrimSpace(*configPath),
			ConfigManagedRuntime: true,
			HTTPAddr:             configuration.HTTPListenAddress,
			HTTPRootPath:         configuration.HTTPRootPath,
			GRPCAddr:             configuration.GRPCListenAddress,
			RestartMode:          configuration.RestartMode,
			TLSMode:              configuration.TLSMode,
			TLSCertFile:          configuration.TLSCertFile,
			TLSKeyFile:           configuration.TLSKeyFile,
			Storage:              configuration.Storage,
			TrustedProxyCIDRs:    parsedCIDRs,
		EncryptionKey:        *encryptionKey,
		LogLevel:             *logLevel,
		}, nil
	}

	configuration, err := config.ResolveLegacyControlPlaneConfig(*httpAddr, *grpcAddr, *restartMode, "", *storageDriver, *storageDSN)
	if err != nil {
		return serveConfig{}, err
	}

	return serveConfig{
		ConfigPath:           "",
		ConfigManagedRuntime: false,
		HTTPAddr:             configuration.HTTPListenAddress,
		HTTPRootPath:         configuration.HTTPRootPath,
		GRPCAddr:             configuration.GRPCListenAddress,
		RestartMode:          configuration.RestartMode,
		TLSMode:              configuration.TLSMode,
		TLSCertFile:          configuration.TLSCertFile,
		TLSKeyFile:           configuration.TLSKeyFile,
		Storage:              configuration.Storage,
		TrustedProxyCIDRs:    parsedCIDRs,
		EncryptionKey:        *encryptionKey,
		LogLevel:             *logLevel,
	}, nil
}

// parseLogLevel maps a human-readable level name to the corresponding slog.Level.
func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func resolvePanelRuntime(configuration serveConfig) (server.PanelRuntime, error) {
	tlsMode := configuration.TLSMode
	if strings.TrimSpace(tlsMode) == "" {
		tlsMode = config.PanelTLSModeProxy
	}

	runtime := server.PanelRuntime{
		HTTPListenAddress: configuration.HTTPAddr,
		HTTPRootPath:      configuration.HTTPRootPath,
		GRPCListenAddress: configuration.GRPCAddr,
		TLSMode:           tlsMode,
		TLSCertFile:       configuration.TLSCertFile,
		TLSKeyFile:        configuration.TLSKeyFile,
		RestartSupported:  configuration.RestartMode == config.RestartModeSupervised,
		ConfigSource:      server.PanelRuntimeSourceLegacy,
		ConfigPath:        configuration.ConfigPath,
	}
	if configuration.ConfigManagedRuntime {
		runtime.ConfigSource = server.PanelRuntimeSourceConfigFile
	}
	return runtime, nil
}

func runBootstrapAdmin(args []string) error {
	flags := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	username := flags.String("username", "admin", "Admin username")
	password := flags.String("password", os.Getenv("PANVEX_BOOTSTRAP_PASSWORD"), "Admin password")
	storageDriver := flags.String("storage-driver", "", "Persistent storage backend driver")
	storageDSN := flags.String("storage-dsn", "", "Persistent storage backend DSN")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *password == "" {
		return errors.New("password is required through -password or PANVEX_BOOTSTRAP_PASSWORD")
	}

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}

	store, err := openStore(storageConfig)
	if err != nil {
		return err
	}
	defer store.Close()

	existingUsers, err := store.ListUsers(context.Background())
	if err != nil {
		return err
	}
	if len(existingUsers) > 0 {
		return errors.New("storage already contains users")
	}

	service := auth.NewServiceWithStore(store)
	_, _, err = service.BootstrapUser(auth.BootstrapInput{
		Username: *username,
		Password: *password,
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

func runResetUserTotp(args []string) error {
	flags := flag.NewFlagSet("reset-user-totp", flag.ContinueOnError)
	username := flags.String("username", "", "Username to reset TOTP for")
	storageDriver := flags.String("storage-driver", "", "Persistent storage backend driver")
	storageDSN := flags.String("storage-dsn", "", "Persistent storage backend DSN")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *username == "" {
		return errors.New("username is required")
	}

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}

	store, err := openStore(storageConfig)
	if err != nil {
		return err
	}
	defer store.Close()

	record, err := store.GetUserByUsername(context.Background(), *username)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return fmt.Errorf("user %q not found", *username)
		}
		return err
	}

	service := auth.NewServiceWithStore(store)
	user, err := service.ResetTotp(record.ID)
	if err != nil {
		return err
	}

	if err := store.AppendAuditEvent(context.Background(), storage.AuditEventRecord{
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

func runMigrateStorage(args []string) error {
	flags := flag.NewFlagSet("migrate-storage", flag.ContinueOnError)
	sourceDriver := flags.String("from-driver", "sqlite", "Source storage backend driver")
	sourceDSN := flags.String("from-dsn", "", "Source storage backend DSN")
	targetDriver := flags.String("to-driver", "postgres", "Target storage backend driver")
	targetDSN := flags.String("to-dsn", "", "Target storage backend DSN")
	if err := flags.Parse(args); err != nil {
		return err
	}

	summary, err := storagemigrate.Run(context.Background(), storagemigrate.Options{
		SourceDriver: *sourceDriver,
		SourceDSN:    *sourceDSN,
		TargetDriver: *targetDriver,
		TargetDSN:    *targetDSN,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Migration completed.\n")
	fmt.Printf("Users: %d\n", summary.Users)
	fmt.Printf("Fleet groups: %d\n", summary.FleetGroups)
	fmt.Printf("Agents: %d\n", summary.Agents)
	fmt.Printf("Instances: %d\n", summary.Instances)
	fmt.Printf("Jobs: %d\n", summary.Jobs)
	fmt.Printf("Job targets: %d\n", summary.JobTargets)
	fmt.Printf("Audit events: %d\n", summary.AuditEvents)
	fmt.Printf("Metric snapshots: %d\n", summary.MetricSnapshots)
	fmt.Printf("Enrollment tokens: %d\n", summary.EnrollmentTokens)
	return nil
}

// parseCIDRList splits a comma-separated string of CIDR notations and returns
// the parsed networks. An empty input returns nil without error.
func parseCIDRList(raw string) ([]*net.IPNet, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	parts := strings.Split(raw, ",")
	result := make([]*net.IPNet, 0, len(parts))
	for _, part := range parts {
		cidr := strings.TrimSpace(part)
		if cidr == "" {
			continue
		}
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
		}
		result = append(result, network)
	}
	return result, nil
}

func runSelfUpdate(args []string) error {
	flags := flag.NewFlagSet("self-update", flag.ContinueOnError)
	version := flags.String("version", "", "Target version to update to (e.g. 1.2.3)")
	repo := flags.String("repo", "panvex/panvex", "GitHub repository for release assets")
	token := flags.String("token", os.Getenv("GITHUB_TOKEN"), "GitHub token for private repos (env: GITHUB_TOKEN)")
	force := flags.Bool("force", false, "Force update even if versions match")
	if err := flags.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()

	panel, _, err := server.FetchLatestVersions(ctx, *repo, *token)
	if err != nil {
		return fmt.Errorf("fetch latest versions: %w", err)
	}

	if panel == nil {
		return errors.New("no control-plane release found")
	}

	_, latestVersion, ok := server.ParseReleaseTag(panel.TagName)
	if !ok {
		return fmt.Errorf("failed to parse release tag %q", panel.TagName)
	}

	targetVersion := latestVersion
	if *version != "" {
		targetVersion = strings.TrimPrefix(*version, "v")
	}

	currentVersion := strings.TrimPrefix(Version, "v")
	cmp := server.CompareVersions(targetVersion, currentVersion)
	if cmp == 0 && !*force {
		fmt.Printf("Already at version %s. Use --force to re-install.\n", currentVersion)
		return nil
	}
	if cmp < 0 && !*force {
		fmt.Printf("Target version %s is older than current version %s. Use --force to downgrade.\n", targetVersion, currentVersion)
		return nil
	}

	binaryURL, checksumURL := server.ResolveAssetURLs(panel, "control-plane")
	if binaryURL == "" {
		return errors.New("no binary download URL found for the current platform")
	}

	fmt.Printf("Updating from %s to %s ...\n", currentVersion, targetVersion)

	// Download and verify checksum.
	var expectedChecksum string
	if checksumURL != "" {
		expectedChecksum, err = server.DownloadChecksum(ctx, checksumURL, *token)
		if err != nil {
			return fmt.Errorf("download checksum: %w", err)
		}
		fmt.Println("Checksum downloaded.")
	}

	tmpPath, err := server.DownloadBinary(ctx, binaryURL, *token)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	defer os.Remove(tmpPath)
	fmt.Println("Binary downloaded.")

	if expectedChecksum != "" {
		if err := server.VerifyChecksum(tmpPath, expectedChecksum); err != nil {
			return fmt.Errorf("verify checksum: %w", err)
		}
		fmt.Println("Checksum verified.")
	}

	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve current binary: %w", err)
	}

	if err := server.AtomicReplaceBinary(currentBinary, tmpPath); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	fmt.Printf("Updated to v%s. Restart the service to apply.\n", targetVersion)
	return nil
}

func openStore(configuration config.StorageConfig) (storage.Store, error) {
	switch configuration.Driver {
	case config.StorageDriverSQLite:
		return sqlite.Open(configuration.DSN)
	case config.StorageDriverPostgres:
		return postgres.Open(configuration.DSN)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", configuration.Driver)
	}
}
