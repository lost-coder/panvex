package main

import (
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agentrevocation"
)

// Build-time version information, injected via -ldflags.
var (
	AgentVersion = "dev"
	CommitSHA    = "unknown"
	BuildTime    = "unknown"
)

const (
	runtimeCertificateRenewWindow = 24 * time.Hour
	runtimeCertificateRenewRetry  = time.Minute
	runtimeInitializationFastInterval = 3 * time.Second
	gatewayStreamConnectTimeout   = 15 * time.Second
	certificateRefreshTimeout     = 15 * time.Second
	jobExecutionTimeout           = 30 * time.Second
	runtimeOperationTimeout       = 20 * time.Second
	jobQueueCapacity              = 16
)

var errRuntimeCredentialsRefreshed = errors.New("runtime credentials refreshed")

// agentDeregisteredExitCode signals to systemd that the panel has
// removed our agent record. The install-script's unit file pairs this
// with RestartPreventExitStatus=78 so the service stays stopped instead
// of looping in an "agent has been deregistered" → reconnect → reject
// cycle. 78 is the conventional sysexits.h EX_CONFIG.
const agentDeregisteredExitCode = 78

func main() {
	err := run(os.Args[1:])
	if err == nil {
		return
	}
	if errors.Is(err, agentrevocation.ErrAgentRevoked) {
		slog.Error("agent has been deregistered on the panel; uninstall the service or re-enroll with a new token", "error", err)
		os.Exit(agentDeregisteredExitCode)
	}
	// runRuntime / runBootstrapCommand have already initialised slog by
	// the time we reach here, so route the fatal through the structured
	// logger instead of the legacy log package.
	slog.Error("agent fatal", "error", err)
	os.Exit(1)
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "bootstrap" {
		return runBootstrapCommand(args[1:], nil)
	}

	return runRuntime(args)
}

// clientDataConcurrencyDefault returns the default for -client-data-concurrency.
// Reading the env var here keeps the flag default visible in -help output
// (golang's flag package prints the value at registration time) instead of
// silently overriding it after parse.
func clientDataConcurrencyDefault() int {
	if raw := strings.TrimSpace(os.Getenv("PANVEX_AGENT_CLIENT_DATA_CONCURRENCY")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return 8
}

func hostName() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown-node"
	}

	return name
}

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
