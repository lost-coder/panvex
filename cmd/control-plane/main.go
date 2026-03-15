package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/config"
	"github.com/panvex/panvex/internal/controlplane/server"
	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/controlplane/storage/postgres"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
	"github.com/panvex/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type serveConfig struct {
	HTTPAddr string
	GRPCAddr string
	Storage config.StorageConfig
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "bootstrap-admin" {
		return runBootstrapAdmin(args[1:])
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

	api := server.New(server.Options{
		Now:   time.Now,
		Store: store,
	})

	httpServer := &http.Server{
		Addr:    options.HTTPAddr,
		Handler: api.Handler(),
	}

	grpcListener, err := net.Listen("tcp", options.GRPCAddr)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(api.GRPCTLSConfig())))
	gatewayrpc.RegisterGatewayServer(grpcServer, api)

	httpErrors := make(chan error, 1)
	go func() {
		log.Printf("control-plane http listening on %s", options.HTTPAddr)
		httpErrors <- httpServer.ListenAndServe()
	}()

	log.Printf("control-plane grpc listening on %s", options.GRPCAddr)
	go func() {
		httpErrors <- grpcServer.Serve(grpcListener)
	}()

	return <-httpErrors
}

func parseServeConfig(args []string) (serveConfig, error) {
	flags := flag.NewFlagSet("control-plane", flag.ContinueOnError)
	httpAddr := flags.String("http-addr", ":8080", "HTTP listen address")
	grpcAddr := flags.String("grpc-addr", ":8443", "gRPC listen address")
	storageDriver := flags.String("storage-driver", "", "Persistent storage backend driver")
	storageDSN := flags.String("storage-dsn", "", "Persistent storage backend DSN")
	if err := flags.Parse(args); err != nil {
		return serveConfig{}, err
	}

	storage, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return serveConfig{}, err
	}

	return serveConfig{
		HTTPAddr: *httpAddr,
		GRPCAddr: *grpcAddr,
		Storage: storage,
	}, nil
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
	_, secret, err := service.BootstrapUser(auth.BootstrapInput{
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
	fmt.Printf("TOTP secret: %s\n", secret)
	fmt.Printf("otpauth URL: %s\n", buildOTPAuthURL(*username, secret))
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

func buildOTPAuthURL(username string, secret string) string {
	return "otpauth://totp/Panvex:" + url.PathEscape(username) + "?secret=" + url.QueryEscape(secret) + "&issuer=Panvex"
}
