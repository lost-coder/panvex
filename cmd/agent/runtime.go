package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/agent/runtimeevents"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/controlplane/agentrevocation"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/logutil"
)

// runtimeEventsBuf is the process-wide ring of recent Info+ slog records
// populated by the runtimeevents.Handler installed in runRuntime. It is
// drained by the pusher goroutine started in runConnection. We use a
// package-level handle (rather than a state struct) because cmd/agent
// already threads cross-cutting hooks through positional parameters and
// the slog default itself is process-global — see runtime.go comment
// near the wiring for the rationale.
var (
	runtimeEventsBuf    *runtimeevents.Buffer
	runtimeEventsNotify chan struct{}
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
	telemtRestart         string
	heartbeat             time.Duration
	runtimePoll           time.Duration
	runtimeUpload         time.Duration
	runtimeSnapshot       time.Duration
	usageSnapshot         time.Duration
	ipPoll                time.Duration
	ipUpload              time.Duration
	logLevel              string
	logFormat             string
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
	flags.StringVar(&cfg.telemtRestart, "telemt-restart", os.Getenv("PANVEX_TELEMT_RESTART"),
		"How the agent restarts Telemt for restart-required config changes: systemd:<unit> | docker:<container> | command:<argv>")
	flags.DurationVar(&cfg.heartbeat, "heartbeat-interval", 15*time.Second, "Heartbeat interval")
	flags.DurationVar(&cfg.runtimePoll, "runtime-poll-interval", 15*time.Second, "How often the agent polls Telemt for runtime data")
	flags.DurationVar(&cfg.runtimeUpload, "runtime-upload-interval", time.Minute, "How often aggregated runtime snapshots are sent to the control-plane")
	flags.DurationVar(&cfg.runtimeSnapshot, "snapshot-interval", 0, "Deprecated: use -runtime-poll-interval and -runtime-upload-interval")
	flags.DurationVar(&cfg.usageSnapshot, "usage-interval", 2*time.Minute, "Client usage snapshot interval")
	flags.DurationVar(&cfg.ipPoll, "ip-poll-interval", 15*time.Second, "Client IP polling interval")
	flags.DurationVar(&cfg.ipUpload, "ip-upload-interval", time.Minute, "Client IP upload interval")
	flags.StringVar(&cfg.logLevel, "log-level", "info", "Log level: debug, info, warn, error")
	flags.StringVar(&cfg.logFormat, "log-format", os.Getenv("PANVEX_LOG_FORMAT"),
		"Log output format (text or json). Env: PANVEX_LOG_FORMAT.")
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

	logFormat, err := logutil.ParseFormat(cfg.logFormat)
	if err != nil {
		return fmt.Errorf("agent: invalid log format: %w", err)
	}
	inner := logutil.NewHandler(logutil.Options{
		Format: logFormat,
		Level:  parseLogLevel(cfg.logLevel),
		Sink:   os.Stderr,
	})
	// runtimeBuf is the agent-side ring of recent Info+ slog records. The
	// pusher goroutine started in connection.go drains this buffer and
	// ships batches to the panel via the Connect bidi-stream. We park it
	// on a package-level handle (runtimeEventsBuf) because the existing
	// cmd/agent code threads cross-cutting hooks through positional
	// parameters and there is no aggregate state struct; introducing one
	// purely for the buffer would touch every connection.go callsite.
	runtimeBuf := runtimeevents.NewBuffer(200)
	runtimeHandler := runtimeevents.NewHandler(inner, runtimeBuf)
	// runtimeNotify wakes the pusher goroutine immediately whenever a Warn
	// or Error record is appended. Buffered cap=1 + select-default in the
	// callback guarantees the slog Handle path never blocks: if a notify
	// is already pending, the urgent record is still buffered and the
	// pusher will pick it up on the next iteration.
	runtimeNotify := make(chan struct{}, 1)
	runtimeHandler.SetUrgentCallback(func() {
		select {
		case runtimeNotify <- struct{}{}:
		default:
			// notify already pending; pusher will pick this event up next cycle.
		}
	})
	slog.SetDefault(slog.New(runtimeHandler))
	runtimeEventsBuf = runtimeBuf
	runtimeEventsNotify = runtimeNotify

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
		TelemtRestart:    cfg.telemtRestart,
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
		ScheduleSelfRestart: func() {
			// A3: the JobResult must reach the panel BEFORE this process
			// goes away, otherwise the panel re-dispatches the job to the
			// restarted agent (whose completedJobs cache is empty) in an
			// infinite update/restart loop. Delay the restart so the job
			// worker can flush the result onto the stream — same
			// flush-window pattern as UpdateTransport above, with a much
			// larger margin because systemd kills the whole process.
			time.AfterFunc(selfUpdateRestartDelay, func() {
				slog.Info("self-update: restarting via systemd")
				// On success systemd tears this process down. On failure
				// exit NON-ZERO so the unit's Restart=on-failure relaunches
				// the already-replaced binary — exit 0 would not be
				// restarted, and 78 is RestartPreventExitStatus.
				// Background ctx: this is a fire-and-forget restart from an
				// AfterFunc with no parent ctx; we never want to cancel it.
				if err := exec.CommandContext(context.Background(), "systemctl", "restart", "panvex-agent").Start(); err != nil {
					slog.Error("self-update: systemctl restart failed; exiting non-zero for on-failure restart", "error", err)
					os.Exit(1)
				}
			})
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

	// enrollment reporter collects local timeline steps (agent_persisted_cert,
	// gateway_dialed, tls_handshake_ok, first_sync_ok) and ships them to the
	// panel via ReportEnrollmentSteps after the first sync is up. Bind only
	// fires for state files persisted by a Phase-1+ bootstrap; older state
	// files carry an empty EnrollmentAttemptID and the reporter becomes a
	// no-op. We back-date agent_persisted_cert with the bootstrap's
	// disk-write timestamp so the panel timeline reflects when the cert
	// actually landed on disk, not when the agent runtime started.
	reporter := newEnrollmentReporter()
	if credentialsState.EnrollmentAttemptID != "" {
		reporter.Bind(credentialsState.EnrollmentAttemptID)
		reporter.RecordAt(
			string(enrollment.StepAgentPersistedCert),
			string(enrollment.LevelInfo),
			"cert saved",
			credentialsState.AgentPersistedCertAt,
			nil,
		)
	}

	// Supervisor context: cancelled on SIGINT/SIGTERM so the reconnect
	// backoff sleep, gRPC stream context, and all derived workers exit
	// promptly instead of waiting out the full ~15-30s backoff window.
	supervisorCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = runRuntimeReconnectLoop(supervisorCtx, &cfg, &credentialsState, agent, schedule, transportReload, reporter)
	if errors.Is(err, context.Canceled) && supervisorCtx.Err() != nil {
		// Shutdown signalled — treat as clean exit.
		slog.Info("agent shutting down on signal")
		return nil
	}
	return err
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
func runRuntimeReconnectLoop(supervisorCtx context.Context, cfg *runtimeFlags, credentialsState *agentstate.Credentials, agent *runtime.Agent, schedule connectionSchedule, tr *transportReloadState, reporter *enrollmentReporter) error {
	reconnectAttempt := 0
	// B4: the in-flight tracker outlives individual connections so a job
	// re-delivered right after a reconnect cannot run concurrently with its
	// still-draining first execution from the previous connection.
	jobInflight := newJobInflightTracker()
	for {
		// Honour shutdown before we begin another iteration.
		if err := supervisorCtx.Err(); err != nil {
			return err
		}

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
			// Derive the refresh ctx from supervisorCtx so SIGTERM during
			// the renewal RPC also unblocks promptly.
			refreshCtx, cancelRefresh := context.WithTimeout(supervisorCtx, certificateRefreshTimeout)
			refreshed, err := renewRuntimeCredentialsIfNeeded(refreshCtx, cfg.stateFile, cfg.gatewayAddr, cfg.gatewayServerName, *credentialsState, time.Now())
			cancelRefresh()
			if err != nil {
				if agentrevocation.IsAgentRevoked(err) {
					return agentrevocation.ErrAgentRevoked
				}
				if supervisorCtx.Err() != nil {
					return supervisorCtx.Err()
				}
				reconnectAttempt++
				// A wrapped context.Canceled at this point means the refresh
				// RPC was torn down by a near-simultaneous shutdown — the
				// outer ctx.Err() check above just missed it. Demote to Info
				// so shutdown does not produce a stray Error line.
				if errors.Is(err, context.Canceled) {
					slog.Info("certificate refresh aborted (context cancelled)")
				} else {
					slog.Error("certificate refresh failed", "error", err)
				}
				if waitErr := waitWithCancel(supervisorCtx, reconnectDelay(reconnectAttempt)); waitErr != nil {
					return waitErr
				}
				continue
			}
			*credentialsState = refreshed
		}

		afterConn, connErr := runConnection(supervisorCtx, cfg.gatewayAddr, cfg.gatewayServerName, cfg.stateFile, *credentialsState, agent, schedule, cfg.clientDataConcurrency, tr, reporter, jobInflight)
		*credentialsState = afterConn
		if connErr == nil || errors.Is(connErr, errRuntimeCredentialsRefreshed) {
			reconnectAttempt = 0
			continue
		}
		if agentrevocation.IsAgentRevoked(connErr) {
			return agentrevocation.ErrAgentRevoked
		}
		if supervisorCtx.Err() != nil {
			return supervisorCtx.Err()
		}
		reconnectAttempt++
		// A connection that ends because its own connection ctx was cancelled
		// (e.g. switch_transport_mode job dropping the stream, in-stream cert
		// renewal triggering a reconnect, or a server-side close that races a
		// shutdown signal) surfaces a wrapped context.Canceled here. That is
		// not an Error condition — it's expected lifecycle. Demote to Info so
		// the reconnect remains visible without polluting the Error stream.
		if errors.Is(connErr, context.Canceled) {
			slog.Info("connection ended (context cancelled); reconnecting")
		} else {
			slog.Error("connection ended", "error", connErr)
		}
		if waitErr := waitWithCancel(supervisorCtx, reconnectDelay(reconnectAttempt)); waitErr != nil {
			return waitErr
		}
	}
}

// waitWithCancel sleeps for d, returning early with ctx.Err() if the
// supervisor ctx is cancelled. Replaces bare time.Sleep so a SIGTERM
// during the reconnect backoff (up to 15s) does not hold up shutdown.
func waitWithCancel(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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
