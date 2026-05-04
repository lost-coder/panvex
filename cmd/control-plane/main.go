package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver for migrate-schema
	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/server"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	_ "modernc.org/sqlite" // registers the "sqlite" driver for migrate-schema
)

// Build-time version information, injected via -ldflags.
// SelfUpdateRepo identifies the GitHub repo the self-update path
// fetches release assets from. L-15: stays overridable via
// `-ldflags '-X main.SelfUpdateRepo=fork/panvex'` so a fork can ship
// its own binaries without patching this file.
var (
	Version        = "dev"
	CommitSHA      = "unknown"
	BuildTime      = "unknown"
	SelfUpdateRepo = "lost-coder/panvex"
)

const restartExitCode = 78

// Storage flag names and help strings shared across the serve, bootstrap-admin,
// reset-user-totp, and migrate-schema subcommands. Hoisted to package scope so
// the same literal does not appear five times across flags.String calls
// (Sonar S1192).
const (
	flagStorageDriver = "storage-driver"
	flagStorageDSN    = "storage-dsn"
	helpStorageDriver = "Persistent storage backend driver"
	helpStorageDSN    = "Persistent storage backend DSN"
)

var errPanelRestartRequested = errors.New("panel restart requested")

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, errPanelRestartRequested) {
			os.Exit(restartExitCode)
		}
		slog.Error("control-plane fatal", slog.Any("error", err))
		os.Exit(1)
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
	if len(args) > 0 && args[0] == "migrate-schema" {
		return runMigrateSchema(args[1:])
	}
	if len(args) > 0 && args[0] == "reset-user-totp" {
		return runResetUserTotp(args[1:])
	}
	if len(args) > 0 && args[0] == "self-update" {
		return runSelfUpdate(args[1:])
	}

	return runServe(args)
}

// openLogSink returns the io.Writer used by the slog text handler
// together with an optional close function. When path is empty the
// sink is os.Stderr alone. When set, the file is opened append-only
// and tee'd with stderr so operators keep getting live console output
// while a persistent file builds up for post-mortem analysis.
func openLogSink(path string) (io.Writer, func() error, error) {
	if strings.TrimSpace(path) == "" {
		return os.Stderr, nil, nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		// Best-effort mkdir — surfaces as an OpenFile error if the path
		// is still unreachable after the call. 0o750 keeps log dirs out
		// of world-readable territory (gosec G301).
		_ = os.MkdirAll(dir, 0o750)
	}
	// 0o600: owner-only rw. Logs may contain operator usernames or
	// agent IDs we don't want world-readable (gosec G302).
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return io.MultiWriter(os.Stderr, f), f.Close, nil
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

// resolveEncryptionKey returns the CA private-key passphrase from the most specific
// source provided. Priority: --encryption-key-stdin > --encryption-key-file >
// PANVEX_ENCRYPTION_KEY env. The plaintext value is never accepted on the command
// line because argv leaks via /proc/<pid>/cmdline and ps output.
func resolveEncryptionKey(keyFile string, keyFromStdin bool) (string, error) {
	if keyFromStdin {
		// Accept a single line or a full stream; trim trailing whitespace/newlines.
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read encryption key from stdin: %w", err)
		}
		return strings.TrimRight(line, "\r\n\t "), nil
	}
	if strings.TrimSpace(keyFile) != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return "", fmt.Errorf("read -encryption-key-file: %w", err)
		}
		// Accept the file content verbatim but strip trailing whitespace so an
		// operator-edited file with a trailing newline still produces the correct key.
		return strings.TrimRight(string(data), "\r\n\t "), nil
	}
	return strings.TrimSpace(os.Getenv("PANVEX_ENCRYPTION_KEY")), nil
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

// installScriptURL derives the public URL for the agent install script from
// the panel's HTTP listen address. The script is embedded into the
// control-plane binary and served by the panel itself at /install-agent.sh
// (see internal/controlplane/server/install_script.go), so there is no
// external CDN dependency — the panel is its own distribution channel.
//
// Operators behind a reverse proxy / CDN with a custom hostname should set
// PANVEX_INSTALL_SCRIPT_URL to the fully-qualified URL agents will reach.
// (Q-05.)
func installScriptURL(rt server.PanelRuntime) string {
	if v := strings.TrimSpace(os.Getenv("PANVEX_INSTALL_SCRIPT_URL")); v != "" {
		return v
	}
	// Fall back to a relative path on the panel itself. Operators who serve
	// the panel behind a reverse proxy can set PANVEX_INSTALL_SCRIPT_URL to
	// the fully-qualified URL instead.
	host := rt.HTTPListenAddress
	scheme := "http"
	if rt.TLSMode == "direct" {
		scheme = "https"
	}
	return scheme + "://" + host + "/install-agent.sh"
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
