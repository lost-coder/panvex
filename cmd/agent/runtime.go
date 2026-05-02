package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/controlplane/agentrevocation"
)

// runtimeFlags holds the parsed CLI options for the agent runtime. Pulling
// them off runRuntime keeps the entrypoint short enough to fall under the
// cognitive-complexity threshold.
type runtimeFlags struct {
	gatewayAddr           string
	gatewayServerName     string
	stateFile             string
	nodeName              string
	fleetGroupID          string
	version               string
	telemtURL             string
	telemtMetricsURL      string
	telemtAuth            string
	telemtConfigPath      string
	heartbeat             time.Duration
	runtimePoll           time.Duration
	runtimeUpload         time.Duration
	runtimeSnapshot       time.Duration
	usageSnapshot         time.Duration
	ipPoll                time.Duration
	ipUpload              time.Duration
	logLevel              string
	clientDataConcurrency int
}

// parseRuntimeFlags binds the agent CLI flags and parses the supplied args.
func parseRuntimeFlags(args []string) (runtimeFlags, error) {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	cfg := runtimeFlags{}
	flags.StringVar(&cfg.gatewayAddr, "gateway-addr", "127.0.0.1:8443", "Control-plane gRPC address")
	flags.StringVar(&cfg.gatewayServerName, "gateway-server-name", "control-plane.panvex.internal", "Expected control-plane TLS server name")
	flags.StringVar(&cfg.stateFile, "state-file", "data/agent-state.json", "Agent credential state file")
	flags.StringVar(&cfg.nodeName, "node-name", hostName(), "Node name reported to the control-plane")
	flags.StringVar(&cfg.fleetGroupID, "fleet-group-id", "", "Fleet group identifier reported by the agent")
	flags.StringVar(&cfg.version, "version", AgentVersion, "Agent version reported to control-plane")
	flags.StringVar(&cfg.telemtURL, "telemt-url", "http://127.0.0.1:9091", "Local Telemt API URL")
	flags.StringVar(&cfg.telemtMetricsURL, "telemt-metrics-url", "http://127.0.0.1:9090", "Local Telemt metrics URL")
	flags.StringVar(&cfg.telemtAuth, "telemt-auth", "", "Local Telemt authorization value")
	flags.StringVar(&cfg.telemtConfigPath, "telemt-config-path", "", "Path to Telemt config file (optional, auto-detected via API if empty)")
	flags.DurationVar(&cfg.heartbeat, "heartbeat-interval", 15*time.Second, "Heartbeat interval")
	flags.DurationVar(&cfg.runtimePoll, "runtime-poll-interval", 15*time.Second, "How often the agent polls Telemt for runtime data")
	flags.DurationVar(&cfg.runtimeUpload, "runtime-upload-interval", time.Minute, "How often aggregated runtime snapshots are sent to the control-plane")
	flags.DurationVar(&cfg.runtimeSnapshot, "snapshot-interval", 0, "Deprecated: use -runtime-poll-interval and -runtime-upload-interval")
	flags.DurationVar(&cfg.usageSnapshot, "usage-interval", 2*time.Minute, "Client usage snapshot interval")
	flags.DurationVar(&cfg.ipPoll, "ip-poll-interval", 15*time.Second, "Client IP polling interval")
	flags.DurationVar(&cfg.ipUpload, "ip-upload-interval", time.Minute, "Client IP upload interval")
	flags.StringVar(&cfg.logLevel, "log-level", "info", "Log level: debug, info, warn, error")
	flags.IntVar(&cfg.clientDataConcurrency, "client-data-concurrency", clientDataConcurrencyDefault(), "Max concurrent in-flight ClientDataRequest goroutines (env: PANVEX_AGENT_CLIENT_DATA_CONCURRENCY)")
	if err := flags.Parse(args); err != nil {
		return runtimeFlags{}, err
	}
	// Backward compatibility: if deprecated --snapshot-interval is set, use it for both poll and upload.
	if cfg.runtimeSnapshot > 0 {
		cfg.runtimePoll = cfg.runtimeSnapshot
		cfg.runtimeUpload = cfg.runtimeSnapshot
	}
	return cfg, nil
}

func runRuntime(args []string) error {
	cfg, err := parseRuntimeFlags(args)
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLogLevel(cfg.logLevel)})))

	credentialsState, err := loadRuntimeCredentials(cfg.stateFile)
	if err != nil {
		return err
	}
	if credentialsState.GRPCEndpoint != "" {
		cfg.gatewayAddr = credentialsState.GRPCEndpoint
	}
	if credentialsState.GRPCServerName != "" {
		cfg.gatewayServerName = credentialsState.GRPCServerName
	}

	telemtClient, err := telemt.NewClient(telemt.Config{
		BaseURL:       cfg.telemtURL,
		MetricsURL:    cfg.telemtMetricsURL,
		Authorization: cfg.telemtAuth,
	}, nil)
	if err != nil {
		return err
	}

	statePath := cfg.stateFile

	// transportReload coordinates a transport mode switch requested via a
	// switch_transport_mode job. The job handler writes the new state to disk
	// and sets the flag; runRuntimeReconnectLoop reloads state from disk at
	// the top of the next iteration before establishing a new connection.
	transportReload := &transportReloadState{
		cancel: func() {}, // safe no-op until first connection
	}

	agent := runtime.New(runtime.Config{
		AgentID:          credentialsState.AgentID,
		NodeName:         cfg.nodeName,
		FleetGroupID:     cfg.fleetGroupID,
		Version:          cfg.version,
		TelemtConfigPath: cfg.telemtConfigPath,
		// Resume snapshot sequence across restarts so the control-plane can
		// dedup duplicate deltas. See P2-LOG-06 / L-07.
		InitialUsageSeq: credentialsState.UsageSeq,
		PersistUsageSeq: func(seq uint64) error {
			return agentstate.SaveUsageSeq(statePath, seq)
		},
		UpdateTransport: func(mode, listenAddr, panelURL string) error {
			// Load current state, patch transport fields, and save back to disk.
			// The reconnect loop re-reads disk at the top of its next iteration
			// (guarded by transportReload.pending) so the new mode takes effect
			// on the subsequent connection without requiring a process restart.
			current, err := agentstate.Load(statePath)
			if err != nil {
				return fmt.Errorf("switch_transport_mode: load state: %w", err)
			}
			current.TransportMode = mode
			current.ListenAddr = listenAddr
			if panelURL != "" {
				current.PanelURL = panelURL
			}
			if err := agentstate.Save(statePath, current); err != nil {
				return fmt.Errorf("switch_transport_mode: save state: %w", err)
			}
			slog.Info("transport mode updated; reconnecting to apply",
				"mode", mode, "listen_addr", listenAddr)
			transportReload.mu.Lock()
			transportReload.pending = true
			cancel := transportReload.cancel
			transportReload.mu.Unlock()
			// Defer the cancel so the worker that is invoking us has time to
			// flush the JobResult onto the outbound stream before the
			// connection goes away. Cancelling synchronously here races with
			// the worker's `select` for sending the result and routinely
			// drops it (~50% over the closed Done channel), which then
			// causes the panel to re-dispatch the same job after the
			// retry-after timeout. 50ms is comfortably more than the local
			// channel send + gRPC client-side buffer write under normal
			// conditions. Caveat: if criticalOutbound is full (32 buffered
			// messages) AND the gRPC stream.Send is blocked on remote
			// flow-control, the worker's send may not land within 50ms.
			// A more robust fix would persist JobResult to disk and replay
			// after reconnect — out of scope for this fix.
			time.AfterFunc(50*time.Millisecond, cancel)
			return nil
		},
	}, telemtClient)

	schedule := newConnectionSchedule(cfg.heartbeat, cfg.runtimePoll, cfg.runtimeUpload, cfg.usageSnapshot, cfg.ipPoll, cfg.ipUpload)
	slog.Info("agent starting",
		"agent_id", credentialsState.AgentID,
		"node", cfg.nodeName,
		"gateway", cfg.gatewayAddr,
		"telemt_api", cfg.telemtURL,
		"telemt_metrics", cfg.telemtMetricsURL,
	)

	return runRuntimeReconnectLoop(&cfg, &credentialsState, agent, schedule, transportReload)
}

// runRuntimeReconnectLoop is the agent's outer main-loop: refresh certs,
// run the gRPC stream, and reconnect with backoff on failure. Extracted so
// runRuntime stays under the CC threshold.
//
// Returns agentrevocation.ErrAgentRevoked when the panel signals (via the
// AGENT_REVOKED ErrorInfo on a PermissionDenied status) that this agent
// has been deregistered. In that case there is no point reconnecting —
// the propagated sentinel maps to exit code 78 in main, paired with
// systemd's RestartPreventExitStatus=78 in the unit file written by
// install-agent.sh. Any other error keeps the historic forever-retry
// behaviour.
func runRuntimeReconnectLoop(cfg *runtimeFlags, credentialsState *agentstate.Credentials, agent *runtime.Agent, schedule connectionSchedule, tr *transportReloadState) error {
	reconnectAttempt := 0
	for {
		// If a switch_transport_mode job fired during the last connection, reload
		// state from disk so the new mode and listen address take effect.
		tr.mu.Lock()
		if tr.pending {
			tr.pending = false
			tr.mu.Unlock()
			if reloaded, loadErr := agentstate.Load(cfg.stateFile); loadErr == nil {
				*credentialsState = reloaded
				if reloaded.GRPCEndpoint != "" {
					cfg.gatewayAddr = reloaded.GRPCEndpoint
				}
				if reloaded.GRPCServerName != "" {
					cfg.gatewayServerName = reloaded.GRPCServerName
				}
			} else {
				slog.Error("transport reload: failed to reload state from disk", "error", loadErr)
			}
		} else {
			tr.mu.Unlock()
		}

		// In listen mode the agent has no outbound dial route, so the
		// pre-connection unary RenewCertificate RPC cannot be used. In-stream
		// renewal (via RenewalRequest/RenewalResponse over the Connect bidi-
		// stream) handles cert refresh for listen-mode agents. Skip the dial-
		// only pre-connection renewal when in listen mode; the in-stream path
		// in runConnectionMainLoop covers that case.
		if credentialsState.TransportMode != "listen" {
			refreshCtx, cancelRefresh := context.WithTimeout(context.Background(), certificateRefreshTimeout)
			refreshed, err := renewRuntimeCredentialsIfNeeded(refreshCtx, cfg.stateFile, cfg.gatewayAddr, cfg.gatewayServerName, *credentialsState, time.Now())
			cancelRefresh()
			if err != nil {
				if agentrevocation.IsAgentRevoked(err) {
					return agentrevocation.ErrAgentRevoked
				}
				reconnectAttempt++
				slog.Error("certificate refresh failed", "error", err)
				time.Sleep(reconnectDelay(reconnectAttempt))
				continue
			}
			*credentialsState = refreshed
		}

		afterConn, connErr := runConnection(cfg.gatewayAddr, cfg.gatewayServerName, cfg.stateFile, *credentialsState, agent, schedule, cfg.clientDataConcurrency, tr)
		*credentialsState = afterConn
		if connErr == nil || errors.Is(connErr, errRuntimeCredentialsRefreshed) {
			reconnectAttempt = 0
			continue
		}
		if agentrevocation.IsAgentRevoked(connErr) {
			return agentrevocation.ErrAgentRevoked
		}
		reconnectAttempt++
		slog.Error("connection ended", "error", connErr)
		time.Sleep(reconnectDelay(reconnectAttempt))
	}
}

func reconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << min(attempt-1, 4)
	if delay > 15*time.Second {
		return 15 * time.Second
	}
	return delay
}
