package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	agentTransport "github.com/lost-coder/panvex/internal/agent/transport"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// transportReloadState coordinates transport mode switches requested via a
// switch_transport_mode job. The job handler writes new state to disk, sets
// pending=true, and calls cancel() to drop the current connection; the loop
// then reloads credentials from disk before establishing the next connection.
type transportReloadState struct {
	mu      sync.Mutex
	pending bool
	cancel  func()
}

// panelClientCN mirrors server.PanelClientCN — the protocol-fixed CN of the
// control-plane's outbound client certificate. Duplicated as a literal
// because cmd/agent must not import the control-plane server package.
const panelClientCN = "control-plane.panvex.internal"

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
			Addr:    creds.ListenAddr,
			Cert:    cert,
			CAPEM:   creds.CAPEM,
			PanelCN: panelClientCN,
		}), nil
	}
	return agentTransport.NewDialTransport(dialCfg), nil
}

// runConnectionParams bundles the non-context inputs of runConnection.
// Grouping them keeps the call site readable and the signature within the
// parameter limit (SonarQube go:S107); ctx stays an explicit first parameter
// per Go convention (contextcheck). The connection logic below is unchanged —
// the fields are unpacked into the original local names so the body reads
// identically.
type runConnectionParams struct {
	gatewayAddr           string
	serverName            string
	stateFile             string
	credentialsState      agentstate.Credentials
	agent                 *runtime.Agent
	schedule              connectionSchedule
	clientDataConcurrency int
	tr                    *transportReloadState
	reporter              *enrollmentReporter
	jobInflight           *jobInflightTracker
	transportProbation    time.Duration
}

func runConnection(supervisorCtx context.Context, p runConnectionParams) (agentstate.Credentials, error) {
	gatewayAddr := p.gatewayAddr
	serverName := p.serverName
	stateFile := p.stateFile
	credentialsState := p.credentialsState
	agent := p.agent
	schedule := p.schedule
	clientDataConcurrency := p.clientDataConcurrency
	tr := p.tr
	reporter := p.reporter
	jobInflight := p.jobInflight
	transportProbation := p.transportProbation

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

	// Record the dial intent ahead of RunOnce. We treat the successful
	// connect callback as evidence that both the TCP dial and the TLS
	// handshake succeeded — the transport layer hands us a live stream on
	// the same callback for both dial and listen modes. A dial failure
	// surfaces via the runErr below and is reported as an error-level
	// gateway_dialed step.
	if credentialsState.TransportMode != "listen" {
		reporter.Record(
			string(enrollment.StepGatewayDialed),
			string(enrollment.LevelInfo),
			"dialing panel",
			map[string]string{"endpoint": gatewayAddr},
		)
	}

	// Derive a cancellable ctx from supervisorCtx for the dial / accept
	// step so SIGTERM during connect tears it down promptly. Previously
	// this was context.Background(), which left the dial blocked until
	// gatewayStreamConnectTimeout.
	dialCtx, cancelDial := context.WithCancel(supervisorCtx)
	defer cancelDial()

	// A2: while a transport switch is on probation, bound the accept/dial by
	// the remaining probation window so the reconnect loop regains control
	// and can roll the switch back if the panel never connects.
	if credentialsState.PrevTransport != nil && credentialsState.TransportSwitchedAtUnix > 0 {
		window := transportProbation
		if window <= 0 {
			window = defaultTransportProbation
		}
		deadline := time.Unix(credentialsState.TransportSwitchedAtUnix, 0).Add(window)
		var cancelProbation context.CancelFunc
		dialCtx, cancelProbation = context.WithDeadline(dialCtx, deadline)
		defer cancelProbation()
	}

	var updatedCredentials agentstate.Credentials
	runErr := t.RunOnce(dialCtx, func(_ context.Context, stream agentTransport.BidiStream, client gatewayrpc.AgentGatewayClient) error {
		// gosec G706: slog uses structured key/value attributes — neither agent
		// id nor gateway address is interpreted as a format string, so the
		// taint-analysis warning is a false positive.
		//nolint:gosec // G706: structured logging, no format-string injection vector
		slog.Info("connected to control-plane", "agent_id", agent.AgentID(), "gateway", gatewayAddr)
		clearTransportProbation(stateFile, &credentialsState)

		// Derive the connection context from the supervisor ctx so SIGTERM
		// reaches every per-connection worker (outbound pump, inbound pump,
		// job workers, polling loops). The stream's own ctx (when
		// available) is composed in too so a server-side close still
		// tears the connection down.
		streamCtx, streamCancel := context.WithCancel(supervisorCtx)
		defer streamCancel()
		if cs, ok := stream.(interface{ Context() context.Context }); ok {
			// Propagate stream cancellation into streamCtx without
			// replacing the supervisor parentage.
			scStream := cs.Context()
			go func() {
				select {
				case <-scStream.Done():
					streamCancel()
				case <-streamCtx.Done():
				}
			}()
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

		criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 32)
		telemetryOutbound := make(chan *gatewayrpc.ConnectClientMessage, 64)
		jobQueues := map[jobPipeline]chan *gatewayrpc.JobCommand{
			jobPipelineRuntimeReload:  make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
			jobPipelineClientMutation: make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
			jobPipelineDefault:        make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
		}
		defer func() {
			streamWG.Wait()
			releaseQueuedJobs(jobInflight, jobQueues)
		}()
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
		startJobWorkers(connectionCtx, &streamWG, agent, jobInflight, jobQueues, criticalOutbound)

		// TLS handshake succeeded by the time RunOnce calls back — the
		// stream is live and authenticated. Record before the initial
		// sync so the panel timeline orders tls_handshake_ok before
		// first_sync_ok regardless of how fast sendInitialMessages
		// returns.
		reporter.Record(
			string(enrollment.StepTLSHandshakeOK),
			string(enrollment.LevelInfo),
			"panel reached",
			nil,
		)

		if initErr := sendInitialMessages(connectionCtx, criticalOutbound, agent); initErr != nil {
			cancelConnection()
			return initErr
		}
		slog.Info("initial sync completed", "agent_id", agent.AgentID(), "node", agent.NodeName())

		// First sync confirmed: outbound queue accepted heartbeat + snapshot.
		// Flush buffered events to the panel. Failure is non-fatal — events
		// stay in the buffer so the next reconnect retries.
		reporter.Record(
			string(enrollment.StepFirstSyncOK),
			string(enrollment.LevelInfo),
			"initial sync completed",
			nil,
		)
		if flushErr := reporter.Flush(connectionCtx, client); flushErr != nil {
			// Connection tear-down (server-close, supervisor cancel,
			// transport-mode switch) races the flush; the gRPC call
			// returns context.Canceled in that window. That is not
			// operator-actionable noise — surface it at Debug so a
			// real flush failure on a live connection still warns.
			if errors.Is(flushErr, context.Canceled) || connectionCtx.Err() != nil {
				slog.DebugContext(connectionCtx, "enrollment report flush skipped (context cancelled)")
			} else {
				slog.WarnContext(connectionCtx, "enrollment report flush failed", "error", flushErr)
			}
		} else {
			// Enrollment is one-shot. After the first successful flush, do not let
			// subsequent reconnect cycles pollute the same enrollment_attempts row.
			reporter.Disable()
		}

		credentialRefreshTimer := newRuntimeCredentialRefreshTimer(credentialsState, time.Now())
		if credentialRefreshTimer != nil {
			defer credentialRefreshTimer.Stop()
		}
		startPollingWorkers(connectionCtx, &streamWG, schedule, agent, criticalOutbound, telemetryOutbound)
		startRuntimeEventsPusher(connectionCtx, &streamWG, agent.AgentID(), telemetryOutbound)

		var loopErr error
		updatedCredentials, loopErr = runConnectionMainLoop(connectionCtx, cancelConnection, credentialsState, stateFile, client, criticalOutbound, renewalResponses, credentialRefreshTimer, sendErrors)
		return loopErr
	})
	if runErr != nil {
		// Record dial / handshake failure so the panel timeline carries
		// the cause. We cannot distinguish "TCP dial failed" from
		// "TLS handshake failed" from "first sync failed" here without
		// poking inside the transport package, so we log the error
		// string and rely on operators to read it. Flush is best-effort
		// from the supervisor ctx because connectionCtx is gone.
		if credentialsState.TransportMode != "listen" {
			reporter.Record(
				string(enrollment.StepGatewayDialed),
				string(enrollment.LevelError),
				runErr.Error(),
				nil,
			)
		}
		return credentialsState, runErr
	}
	return updatedCredentials, nil
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
				enqueueReceivedJob(connectionCtx, agent.AgentID(), agent, jobInflight, jobQueues, criticalOutbound, job)
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
					// A cancelled connection ctx (server-side close, supervisor
					// shutdown) propagates as context.Canceled here. The
					// reconnect loop will pick up cleanly — no operator action.
					if errors.Is(err, context.Canceled) || connectionCtx.Err() != nil {
						slog.Debug("in-stream certificate renewal aborted (context cancelled)")
					} else {
						slog.Error("in-stream certificate renewal failed", "error", err)
					}
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
				// Same demotion as the in-stream path: a cancelled
				// connectionCtx surfaces here as context.Canceled and is
				// expected lifecycle, not an Error.
				if errors.Is(err, context.Canceled) || connectionCtx.Err() != nil {
					slog.Debug("certificate renewal aborted (context cancelled)")
				} else {
					slog.Error("certificate renewal failed", "error", err)
				}
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
