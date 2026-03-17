package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
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

type serveConfig struct {
	HTTPAddr    string
	GRPCAddr    string
	RestartMode string
	Storage     config.StorageConfig
}

const (
	restartModeDisabled   = "disabled"
	restartModeSupervised = "supervised"
	restartExitCode       = 78
)

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
	if len(args) > 0 && args[0] == "bootstrap-admin" {
		return runBootstrapAdmin(args[1:])
	}
	if len(args) > 0 && args[0] == "migrate-storage" {
		return runMigrateStorage(args[1:])
	}
	if len(args) > 0 && args[0] == "reset-user-totp" {
		return runResetUserTotp(args[1:])
	}

	return runServe(args)
}

func runServe(args []string) error {
	options, err := parseServeConfig(args)
	if err != nil {
		return err
	}

	store, err := openStore(options.Storage)
	if err != nil {
		return err
	}
	defer store.Close()

	panelRuntime, err := resolvePanelRuntime(store, options)
	if err != nil {
		return err
	}
	restartRequests := make(chan struct{}, 1)

	api := server.New(server.Options{
		Now:          time.Now,
		Store:        store,
		UIFiles:      embeddedUIFiles(),
		PanelRuntime: panelRuntime,
		RequestRestart: func() error {
			select {
			case restartRequests <- struct{}{}:
			default:
			}
			return nil
		},
	})

	httpServer := &http.Server{
		Addr:    panelRuntime.HTTPListenAddress,
		Handler: api.Handler(),
	}

	grpcListener, err := net.Listen("tcp", panelRuntime.GRPCListenAddress)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(api.GRPCTLSConfig())))
	gatewayrpc.RegisterGatewayServer(grpcServer, api)

	httpErrors := make(chan error, 2)
	go func() {
		log.Printf("control-plane http listening on %s", panelRuntime.HTTPListenAddress)
		if panelRuntime.TLSMode == "direct" {
			httpErrors <- httpServer.ListenAndServeTLS(panelRuntime.TLSCertFile, panelRuntime.TLSKeyFile)
			return
		}
		httpErrors <- httpServer.ListenAndServe()
	}()

	log.Printf("control-plane grpc listening on %s", panelRuntime.GRPCListenAddress)
	go func() {
		httpErrors <- grpcServer.Serve(grpcListener)
	}()

	for {
		select {
		case <-restartRequests:
			shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_ = httpServer.Shutdown(shutdownContext)
			grpcServer.GracefulStop()
			_ = grpcListener.Close()
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
	httpAddr := flags.String("http-addr", ":8080", "HTTP listen address")
	grpcAddr := flags.String("grpc-addr", ":8443", "gRPC listen address")
	restartMode := flags.String("restart-mode", restartModeDisabled, "Panel restart mode (disabled or supervised)")
	storageDriver := flags.String("storage-driver", "", "Persistent storage backend driver")
	storageDSN := flags.String("storage-dsn", "", "Persistent storage backend DSN")
	if err := flags.Parse(args); err != nil {
		return serveConfig{}, err
	}
	if *restartMode != restartModeDisabled && *restartMode != restartModeSupervised {
		return serveConfig{}, fmt.Errorf("unsupported restart mode %q", *restartMode)
	}

	storage, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return serveConfig{}, err
	}

	return serveConfig{
		HTTPAddr:    *httpAddr,
		GRPCAddr:    *grpcAddr,
		RestartMode: *restartMode,
		Storage:     storage,
	}, nil
}

func resolvePanelRuntime(store storage.Store, configuration serveConfig) (server.PanelRuntime, error) {
	runtime := server.PanelRuntime{
		HTTPListenAddress: configuration.HTTPAddr,
		GRPCListenAddress: configuration.GRPCAddr,
		TLSMode:           "proxy",
		RestartSupported:  configuration.RestartMode == restartModeSupervised,
	}

	record, err := store.GetPanelSettings(context.Background())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return runtime, nil
		}
		return server.PanelRuntime{}, err
	}

	if record.HTTPListenAddress != "" {
		runtime.HTTPListenAddress = record.HTTPListenAddress
	}
	runtime.HTTPRootPath = record.HTTPRootPath
	if record.GRPCListenAddress != "" {
		runtime.GRPCListenAddress = record.GRPCListenAddress
	}
	if record.TLSMode != "" {
		runtime.TLSMode = record.TLSMode
	}
	runtime.TLSCertFile = record.TLSCertFile
	runtime.TLSKeyFile = record.TLSKeyFile
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
	fmt.Printf("Storage DSN: %s\n", storageConfig.DSN)
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
