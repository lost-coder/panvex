package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net"
	"net/url"
	"os"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/server"
	"github.com/panvex/panvex/internal/controlplane/state"
	"github.com/panvex/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

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
	flags := flag.NewFlagSet("control-plane", flag.ContinueOnError)
	httpAddr := flags.String("http-addr", ":8080", "HTTP listen address")
	grpcAddr := flags.String("grpc-addr", ":8443", "gRPC listen address")
	stateFile := flags.String("state-file", "data/auth-state.json", "Path to the local auth state file")
	if err := flags.Parse(args); err != nil {
		return err
	}

	users, err := loadUsersIfExists(*stateFile)
	if err != nil {
		return err
	}

	api := server.New(server.Options{
		Now:   time.Now,
		Users: users,
	})

	httpServer := &http.Server{
		Addr:    *httpAddr,
		Handler: api.Handler(),
	}

	grpcListener, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		return err
	}

	grpcServer := grpc.NewServer(grpc.Creds(credentials.NewTLS(api.GRPCTLSConfig())))
	gatewayrpc.RegisterGatewayServer(grpcServer, api)

	httpErrors := make(chan error, 1)
	go func() {
		log.Printf("control-plane http listening on %s", *httpAddr)
		httpErrors <- httpServer.ListenAndServe()
	}()

	log.Printf("control-plane grpc listening on %s", *grpcAddr)
	go func() {
		httpErrors <- grpcServer.Serve(grpcListener)
	}()

	return <-httpErrors
}

func runBootstrapAdmin(args []string) error {
	flags := flag.NewFlagSet("bootstrap-admin", flag.ContinueOnError)
	stateFile := flags.String("state-file", "data/auth-state.json", "Path to the local auth state file")
	username := flags.String("username", "admin", "Admin username")
	password := flags.String("password", os.Getenv("PANVEX_BOOTSTRAP_PASSWORD"), "Admin password")
	if err := flags.Parse(args); err != nil {
		return err
	}

	if *password == "" {
		return errors.New("password is required through -password or PANVEX_BOOTSTRAP_PASSWORD")
	}

	existingUsers, err := loadUsersIfExists(*stateFile)
	if err != nil {
		return err
	}
	if len(existingUsers) > 0 {
		return fmt.Errorf("state file %s already contains users", *stateFile)
	}

	service := auth.NewService()
	_, secret, err := service.BootstrapUser(auth.BootstrapInput{
		Username: *username,
		Password: *password,
		Role:     auth.RoleAdmin,
	}, time.Now())
	if err != nil {
		return err
	}

	if err := state.SaveUsersFile(*stateFile, service.SnapshotUsers()); err != nil {
		return err
	}

	fmt.Printf("Admin user %q created.\n", *username)
	fmt.Printf("State file: %s\n", *stateFile)
	fmt.Printf("TOTP secret: %s\n", secret)
	fmt.Printf("otpauth URL: %s\n", buildOTPAuthURL(*username, secret))
	return nil
}

func loadUsersIfExists(path string) ([]auth.User, error) {
	users, err := state.LoadUsersFile(path)
	if err == nil {
		return users, nil
	}

	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}

	return nil, err
}

func buildOTPAuthURL(username string, secret string) string {
	return "otpauth://totp/Panvex:" + url.PathEscape(username) + "?secret=" + url.QueryEscape(secret) + "&issuer=Panvex"
}
