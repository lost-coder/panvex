package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	agentTransport "github.com/lost-coder/panvex/internal/agent/transport"
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

func runConnection(supervisorCtx context.Context, gatewayAddr string, serverName string, stateFile string, credentialsState agentstate.Credentials, agent *runtime.Agent, schedule connectionSchedule, clientDataConcurrency int, tr *transportReloadState) (agentstate.Credentials, error) {
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

	// Derive a cancellable ctx from supervisorCtx for the dial / accept
	// step so SIGTERM during connect tears it down promptly. Previously
	// this was context.Background(), which left the dial blocked until
	// gatewayStreamConnectTimeout.
	dialCtx, cancelDial := context.WithCancel(supervisorCtx)
	defer cancelDial()

	var updatedCredentials agentstate.Credentials
	runErr := t.RunOnce(dialCtx, func(_ context.Context, stream agentTransport.BidiStream, client gatewayrpc.AgentGatewayClient) error {
		// gosec G706: slog uses structured key/value attributes — neither agent
		// id nor gateway address is interpreted as a format string, so the
		// taint-analysis warning is a false positive.
		//nolint:gosec // G706: structured logging, no format-string injection vector
		slog.Info("connected to control-plane", "agent_id", agent.AgentID(), "gateway", gatewayAddr)

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

		if initErr := sendInitialMessages(connectionCtx, criticalOutbound, agent); initErr != nil {
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
