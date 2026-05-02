package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	agentTransport "github.com/lost-coder/panvex/internal/agent/transport"
	"github.com/lost-coder/panvex/internal/controlplane/agentrevocation"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
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
	log.Fatal(err)
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "bootstrap" {
		return runBootstrapCommand(args[1:], nil)
	}

	return runRuntime(args)
}

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
// transportReloadState coordinates transport mode switches requested via a
// switch_transport_mode job. The job handler writes new state to disk, sets
// pending=true, and calls cancel() to drop the current connection; the loop
// then reloads credentials from disk before establishing the next connection.
type transportReloadState struct {
	mu      sync.Mutex
	pending bool
	cancel  func()
}

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


// startInboundPump spawns the goroutine that consumes the gateway
// stream and routes job commands to the worker queues, client-data
// requests to bounded handler goroutines, and renewal responses to
// the renewalResponses channel for runConnectionMainLoop.
func startInboundPump(
	connectionCtx context.Context,
	streamWG *sync.WaitGroup,
	stream agentTransport.BidiStream,
	agent *runtime.Agent,
	jobInflight *jobInflightTracker,
	jobQueues map[jobPipeline]chan *gatewayrpc.JobCommand,
	criticalOutbound chan *gatewayrpc.ConnectClientMessage,
	clientDataSem chan struct{},
	renewalResponses chan<- *gatewayrpc.RenewalResponse,
	sendErrorAndCancel func(error),
) {
	streamWG.Add(1)
	go func() {
		defer streamWG.Done()
		for {
			message, err := stream.Recv()
			if err != nil {
				sendErrorAndCancel(err)
				return
			}
			if job := message.GetJob(); job != nil {
				slog.Debug("job received", "job_id", job.GetId(), "action", job.GetAction())
				enqueueReceivedJob(connectionCtx, agent.AgentID(), jobInflight, jobQueues, criticalOutbound, job)
				continue
			}
			if req := message.GetClientDataRequest(); req != nil {
				select {
				case clientDataSem <- struct{}{}:
				case <-connectionCtx.Done():
					return
				}
				// Q4.U-P-08: track spawned client-data handlers so the
				// reconnect path waits for in-flight RPCs to finish.
				streamWG.Add(1)
				go func() {
					defer streamWG.Done()
					defer func() { <-clientDataSem }()
					handleClientDataRequest(connectionCtx, agent, criticalOutbound, req)
				}()
				continue
			}
			if resp := message.GetRenewalResponse(); resp != nil {
				// Non-blocking send: if runConnectionMainLoop is not
				// waiting (e.g. no pending request), drop it silently.
				select {
				case renewalResponses <- resp:
				default:
				}
				continue
			}
		}
	}()
}

// runConnectionMainLoop blocks until either the outbound pump signals
// a fatal send error or a credential refresh either fails or yields
// new credentials (which trigger a reconnect via
// errRuntimeCredentialsRefreshed). Splitting it out keeps
// runConnection's CC below the 15 threshold.
func runConnectionMainLoop(
	connectionCtx context.Context,
	cancelConnection context.CancelFunc,
	credentialsState agentstate.Credentials,
	stateFile string,
	client gatewayrpc.AgentGatewayClient,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
	renewalResponses <-chan *gatewayrpc.RenewalResponse,
	credentialRefreshTimer *time.Timer,
	sendErrors <-chan error,
) (agentstate.Credentials, error) {
	for {
		select {
		case err := <-sendErrors:
			cancelConnection()
			return credentialsState, err
		case <-timerChan(credentialRefreshTimer):
			if !runtimeCredentialsNeedRefresh(credentialsState, time.Now()) {
				resetRuntimeCredentialRefreshTimer(credentialRefreshTimer, runtimeCredentialRefreshDelay(credentialsState, time.Now()))
				continue
			}
			// In-stream renewal: works for both dial and listen modes because
			// the bidi Connect stream is symmetric. The old unary
			// RenewCertificate RPC (dial-only) is now only used in the outer
			// pre-connection path for dial-mode agents.
			if criticalOutbound != nil {
				updatedCredentials, err := renewCertificateInStream(connectionCtx, credentialsState, stateFile, criticalOutbound, renewalResponses)
				if err != nil {
					slog.Error("in-stream certificate renewal failed", "error", err)
					resetRuntimeCredentialRefreshTimer(credentialRefreshTimer, runtimeCertificateRenewRetry)
					continue
				}
				cancelConnection()
				return updatedCredentials, errRuntimeCredentialsRefreshed
			}
			// Fallback: dial-mode without a stream available (should not
			// normally happen when criticalOutbound is wired).
			if client == nil {
				resetRuntimeCredentialRefreshTimer(credentialRefreshTimer, runtimeCredentialRefreshDelay(credentialsState, time.Now()))
				continue
			}
			refreshCtx, cancelRefresh := context.WithTimeout(connectionCtx, certificateRefreshTimeout)
			updatedCredentials, err := refreshRuntimeCredentialsIfNeeded(refreshCtx, stateFile, credentialsState, client, time.Now())
			cancelRefresh()
			if err != nil {
				slog.Error("certificate renewal failed", "error", err)
				resetRuntimeCredentialRefreshTimer(credentialRefreshTimer, runtimeCertificateRenewRetry)
				continue
			}
			if updatedCredentials != credentialsState {
				cancelConnection()
				return updatedCredentials, errRuntimeCredentialsRefreshed
			}
			resetRuntimeCredentialRefreshTimer(credentialRefreshTimer, runtimeCredentialRefreshDelay(credentialsState, time.Now()))
		}
	}
}

// selectTransport returns either a listen-mode or dial-mode Transport based on
// the TransportMode field of the credentials state. It is extracted as a
// helper so it can be unit-tested independently of the full runConnection path.
func selectTransport(creds agentstate.Credentials, dialCfg agentTransport.DialConfig) (agentTransport.Transport, error) {
	if creds.TransportMode == "listen" {
		cert, err := tls.X509KeyPair([]byte(creds.CertificatePEM), []byte(creds.PrivateKeyPEM))
		if err != nil {
			return nil, fmt.Errorf("agent: load TLS keypair for listen mode: %w", err)
		}
		return agentTransport.NewListenTransport(agentTransport.ListenConfig{
			Addr:  creds.ListenAddr,
			Cert:  cert,
			CAPEM: creds.CAPEM,
		}), nil
	}
	return agentTransport.NewDialTransport(dialCfg), nil
}

func runConnection(gatewayAddr string, serverName string, stateFile string, credentialsState agentstate.Credentials, agent *runtime.Agent, schedule connectionSchedule, clientDataConcurrency int, tr *transportReloadState) (agentstate.Credentials, error) {
	certificate, err := tls.X509KeyPair([]byte(credentialsState.CertificatePEM), []byte(credentialsState.PrivateKeyPEM))
	if err != nil {
		return credentialsState, err
	}

	cfg := agentTransport.DialConfig{
		GatewayAddr:    gatewayAddr,
		ServerName:     serverName,
		CAPEM:          credentialsState.CAPEM,
		Cert:           certificate,
		ConnectTimeout: gatewayStreamConnectTimeout,
	}

	t, err := selectTransport(credentialsState, cfg)
	if err != nil {
		return credentialsState, err
	}

	var updatedCredentials agentstate.Credentials
	runErr := t.RunOnce(context.Background(), func(_ context.Context, stream agentTransport.BidiStream, client gatewayrpc.AgentGatewayClient) error {
		// gosec G706: slog uses structured key/value attributes — neither agent
		// id nor gateway address is interpreted as a format string, so the
		// taint-analysis warning is a false positive.
		//nolint:gosec // G706: structured logging, no format-string injection vector
		slog.Info("connected to control-plane", "agent_id", agent.AgentID(), "gateway", gatewayAddr)

		// Derive the connection context from the stream. The concrete stream
		// returned by client.Connect implements grpc.ClientStream which exposes
		// Context(); we type-assert to obtain it and fall back to Background().
		streamCtx := context.Background()
		if cs, ok := stream.(interface{ Context() context.Context }); ok {
			streamCtx = cs.Context()
		}
		connectionCtx, cancelConnection := context.WithCancel(streamCtx)
		defer cancelConnection()

		// Register the cancel func so a switch_transport_mode job can drop this
		// connection promptly, causing the reconnect loop to re-iterate and pick
		// up the new transport state written to disk by UpdateTransport.
		tr.mu.Lock()
		tr.cancel = cancelConnection
		tr.mu.Unlock()

		// Q4.U-P-08: graceful drain. Every goroutine spawned for this
		// connection adds 1 to streamWG and defers wg.Done(); RunOnce
		// blocks on wg.Wait() before returning so a quick reconnect cannot
		// outpace the previous connection's drain. The defer ordering is
		// reverse-source: cancelConnection runs FIRST (closing connectionCtx),
		// then wg.Wait runs and joins the spawned goroutines.
		var streamWG sync.WaitGroup
		defer streamWG.Wait()

		criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 32)
		telemetryOutbound := make(chan *gatewayrpc.ConnectClientMessage, 64)
		jobInflight := newJobInflightTracker()
		jobQueues := map[jobPipeline]chan *gatewayrpc.JobCommand{
			jobPipelineRuntimeReload:  make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
			jobPipelineClientMutation: make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
			jobPipelineDefault:        make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
		}
		sendErrors := make(chan error, 1)
		sendErrorAndCancel := func(sendErr error) {
			sendError(sendErrors, sendErr)
			cancelConnection()
		}

		startOutboundPump(connectionCtx, &streamWG, stream, criticalOutbound, telemetryOutbound, sendErrorAndCancel)

		// Limit concurrent ClientDataRequest goroutines to prevent unbounded
		// growth if the control-plane sends many requests in rapid succession.
		// Configurable via -client-data-concurrency / PANVEX_AGENT_CLIENT_DATA_CONCURRENCY
		// for fleets where the default 8 throttles legitimate burst traffic.
		cdConc := clientDataConcurrency
		if cdConc <= 0 {
			cdConc = 8
		}
		clientDataSem := make(chan struct{}, cdConc)
		// Buffered 1: the inbound pump does a non-blocking send; the main loop
		// reads with a timeout. A buffer of 1 prevents a missed response if the
		// main loop is momentarily not yet waiting.
		renewalResponses := make(chan *gatewayrpc.RenewalResponse, 1)
		startInboundPump(connectionCtx, &streamWG, stream, agent, jobInflight, jobQueues, criticalOutbound, clientDataSem, renewalResponses, sendErrorAndCancel)
		startJobWorkers(connectionCtx, agent, jobInflight, jobQueues, criticalOutbound)

		if initErr := sendInitialMessages(criticalOutbound, agent); initErr != nil {
			cancelConnection()
			return initErr
		}
		slog.Info("initial sync completed", "agent_id", agent.AgentID(), "node", agent.NodeName())

		credentialRefreshTimer := newRuntimeCredentialRefreshTimer(credentialsState, time.Now())
		if credentialRefreshTimer != nil {
			defer credentialRefreshTimer.Stop()
		}
		startPollingWorkers(connectionCtx, schedule, agent, telemetryOutbound)

		var loopErr error
		updatedCredentials, loopErr = runConnectionMainLoop(connectionCtx, cancelConnection, credentialsState, stateFile, client, criticalOutbound, renewalResponses, credentialRefreshTimer, sendErrors)
		return loopErr
	})
	if runErr != nil {
		return credentialsState, runErr
	}
	return updatedCredentials, nil
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
