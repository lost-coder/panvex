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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lost-coder/panvex/internal/agent/conn"
	"github.com/lost-coder/panvex/internal/agent/creds"
	"github.com/lost-coder/panvex/internal/agent/probation"
	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/agent/runtimeevents"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/logutil"
	"github.com/lost-coder/panvex/internal/updatehosts"
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
	usageSnapshot         time.Duration
	ipPoll                time.Duration
	ipUpload              time.Duration
	logLevel              string
	logFormat             string
	clientDataConcurrency int
	transportProbation    time.Duration
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
	flags.DurationVar(&cfg.usageSnapshot, "usage-interval", 2*time.Minute, "Client usage snapshot interval")
	flags.DurationVar(&cfg.ipPoll, "ip-poll-interval", 15*time.Second, "Client IP polling interval")
	flags.DurationVar(&cfg.ipUpload, "ip-upload-interval", time.Minute, "Client IP upload interval")
	flags.StringVar(&cfg.logLevel, "log-level", "info", "Log level: debug, info, warn, error")
	flags.StringVar(&cfg.logFormat, "log-format", os.Getenv("PANVEX_LOG_FORMAT"),
		"Log output format (text or json). Env: PANVEX_LOG_FORMAT.")
	flags.IntVar(&cfg.clientDataConcurrency, "client-data-concurrency", clientDataConcurrencyDefault(), "Max concurrent in-flight ClientDataRequest goroutines (env: PANVEX_AGENT_CLIENT_DATA_CONCURRENCY)")
	flags.DurationVar(&cfg.transportProbation, "transport-probation", probation.DefaultWindow, "How long a transport-mode switch may go without a panel session before the agent reverts to the previous mode (0 = default 10m)")
	if err := flags.Parse(args); err != nil {
		return runtimeFlags{}, err
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
	// pusher goroutine started by the connection layer drains this buffer and
	// ships batches to the panel via the Connect bidi-stream. The ring, its
	// urgent-notify channel and the push cursor are handed to conn.Loop as a
	// conn.RuntimeEvents value: all three must outlive an individual
	// connection (the ring survives a reconnect, so a per-connection cursor
	// would replay up to 200 already-delivered events — audit #9b).
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
	if updatehosts.PolicyFromEnv().Disabled() {
		slog.Warn("update host allow-list DISABLED via PANVEX_UPDATE_ALLOWED_HOSTS=* — agent self-update accepts any https host")
	}
	runtimeEvents := conn.RuntimeEvents{
		Buf:    runtimeBuf,
		Notify: runtimeNotify,
		Cursor: new(atomic.Uint64),
	}

	credentialsState, err := creds.LoadCredentials(cfg.stateFile)
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
	// and sets the flag; conn.Loop reloads state from disk at the top of the
	// next iteration before establishing a new connection.
	transportReload := conn.NewTransportReload()

	agent := runtime.New(runtime.Config{
		AgentID:          credentialsState.AgentID,
		NodeName:         cfg.nodeName,
		FleetGroupID:     cfg.fleetGroupID,
		Version:          cfg.version,
		TelemtConfigPath: cfg.telemtConfigPath,
		TelemtRestart:    cfg.telemtRestart,
		UpdateTransport: func(mode, listenAddr, panelURL string) error {
			// Patch transport fields onto fresh disk state under the state
			// package's write lock (audit #7) — a concurrent usage-seq tick
			// or renewal must not interleave its own Load→Save. The
			// reconnect loop re-reads disk at the top of its next iteration
			// (guarded by the pending flag SetPending raises) so the new mode
			// takes effect on the subsequent connection without a process restart.
			if _, err := agentstate.Update(statePath, func(current *agentstate.Credentials) {
				if current.TransportMode != mode {
					// A2: snapshot the pre-switch state so the reconnect loop
					// can roll back if the panel never reaches us in the new mode.
					current.PrevTransport = &agentstate.TransportSnapshot{
						Mode:           current.TransportMode,
						ListenAddr:     current.ListenAddr,
						GRPCEndpoint:   current.GRPCEndpoint,
						GRPCServerName: current.GRPCServerName,
					}
					current.TransportSwitchedAtUnix = time.Now().Unix()
				}
				current.TransportMode = mode
				current.ListenAddr = listenAddr
				if panelURL != "" {
					current.PanelURL = panelURL
				}
			}); err != nil {
				return fmt.Errorf("switch_transport_mode: update state: %w", err)
			}
			slog.Info("transport mode updated; reconnecting to apply",
				"mode", mode, "listen_addr", listenAddr)
			cancel := transportReload.SetPending()
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

	schedule := conn.NewSchedule(cfg.heartbeat, cfg.runtimePoll, cfg.runtimeUpload, cfg.usageSnapshot, cfg.ipPoll, cfg.ipUpload)
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
	reporter := conn.NewEnrollmentReporter()
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
	// promptly instead of waiting out the full ~45s backoff window.
	supervisorCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	loop := &conn.Loop{
		StateFile:             cfg.stateFile,
		GatewayAddr:           cfg.gatewayAddr,
		ServerName:            cfg.gatewayServerName,
		Credentials:           credentialsState,
		Agent:                 agent,
		Schedule:              schedule,
		Reload:                transportReload,
		Reporter:              reporter,
		Events:                runtimeEvents,
		ClientDataConcurrency: cfg.clientDataConcurrency,
		TransportProbation:    cfg.transportProbation,
	}
	err = loop.Run(supervisorCtx)
	if errors.Is(err, context.Canceled) && supervisorCtx.Err() != nil {
		// Shutdown signalled — treat as clean exit.
		slog.Info("agent shutting down on signal")
		return nil
	}
	return err
}
