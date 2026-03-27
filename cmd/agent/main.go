package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/panvex/panvex/internal/agent/runtime"
	agentstate "github.com/panvex/panvex/internal/agent/state"
	"github.com/panvex/panvex/internal/agent/telemt"
	"github.com/panvex/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	runtimeCertificateRenewWindow = 24 * time.Hour
	runtimeCertificateRenewRetry  = time.Minute
	gatewayStreamConnectTimeout   = 15 * time.Second
	certificateRefreshTimeout     = 15 * time.Second
	jobExecutionTimeout           = 45 * time.Second
	runtimeOperationTimeout       = 20 * time.Second
	jobQueueCapacity              = 16
)

var errRuntimeCredentialsRefreshed = errors.New("runtime credentials refreshed")

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "bootstrap" {
		return runBootstrapCommand(args[1:], nil)
	}

	return runRuntime(args)
}

func runRuntime(args []string) error {
	flags := flag.NewFlagSet("agent", flag.ContinueOnError)
	gatewayAddr := flags.String("gateway-addr", "127.0.0.1:8443", "Control-plane gRPC address")
	gatewayServerName := flags.String("gateway-server-name", "control-plane.panvex.internal", "Expected control-plane TLS server name")
	stateFile := flags.String("state-file", "data/agent-state.json", "Agent credential state file")
	nodeName := flags.String("node-name", hostName(), "Node name reported to the control-plane")
	fleetGroupID := flags.String("fleet-group-id", "", "Fleet group identifier reported by the agent")
	version := flags.String("version", "dev", "Agent version")
	telemtURL := flags.String("telemt-url", "http://127.0.0.1:8080", "Local Telemt API URL")
	telemtMetricsURL := flags.String("telemt-metrics-url", "http://127.0.0.1:8081", "Local Telemt metrics URL")
	telemtAuth := flags.String("telemt-auth", "", "Local Telemt authorization value")
	heartbeat := flags.Duration("heartbeat-interval", 15*time.Second, "Heartbeat interval")
	runtimeSnapshot := flags.Duration("snapshot-interval", time.Minute, "Runtime snapshot interval")
	usageSnapshot := flags.Duration("usage-interval", 3*time.Minute, "Client usage snapshot interval")
	ipPoll := flags.Duration("ip-poll-interval", 25*time.Second, "Client IP polling interval")
	ipUpload := flags.Duration("ip-upload-interval", 3*time.Minute, "Client IP upload interval")
	if err := flags.Parse(args); err != nil {
		return err
	}

	credentialsState, err := loadRuntimeCredentials(*stateFile)
	if err != nil {
		return err
	}
	if credentialsState.GRPCEndpoint != "" {
		*gatewayAddr = credentialsState.GRPCEndpoint
	}
	if credentialsState.GRPCServerName != "" {
		*gatewayServerName = credentialsState.GRPCServerName
	}

	telemtClient, err := telemt.NewClient(telemt.Config{
		BaseURL:       *telemtURL,
		MetricsURL:    *telemtMetricsURL,
		Authorization: *telemtAuth,
	}, nil)
	if err != nil {
		return err
	}

	agent := runtime.New(runtime.Config{
		AgentID:      credentialsState.AgentID,
		NodeName:     *nodeName,
		FleetGroupID: *fleetGroupID,
		Version:      *version,
	}, telemtClient)

	schedule := newConnectionSchedule(*heartbeat, *runtimeSnapshot, *usageSnapshot, *ipPoll, *ipUpload)

	reconnectAttempt := 0
	for {
		refreshCtx, cancelRefresh := context.WithTimeout(context.Background(), certificateRefreshTimeout)
		credentialsState, err = renewRuntimeCredentialsIfNeeded(refreshCtx, *stateFile, *gatewayAddr, *gatewayServerName, credentialsState, time.Now())
		cancelRefresh()
		if err != nil {
			reconnectAttempt++
			log.Printf("agent certificate refresh failed: %v", err)
			time.Sleep(reconnectDelay(reconnectAttempt))
			continue
		}

		credentialsState, err = runConnection(*gatewayAddr, *gatewayServerName, *stateFile, credentialsState, agent, schedule)
		if err == nil || errors.Is(err, errRuntimeCredentialsRefreshed) {
			reconnectAttempt = 0
			continue
		}
		reconnectAttempt++
		log.Printf("agent connection ended: %v", err)
		time.Sleep(reconnectDelay(reconnectAttempt))
	}
}

func loadRuntimeCredentials(stateFile string) (agentstate.Credentials, error) {
	credentialsState, err := agentstate.Load(stateFile)
	if err == nil {
		return credentialsState, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return agentstate.Credentials{}, fmt.Errorf("agent state file %q not found: bootstrap the agent first", stateFile)
	}
	return agentstate.Credentials{}, err
}

type pollingGroup string

const (
	pollHeartbeat pollingGroup = "heartbeat"
	pollRuntime   pollingGroup = "runtime"
	pollUsage     pollingGroup = "usage"
	pollIPPoll    pollingGroup = "ip_poll"
	pollIPUpload  pollingGroup = "ip_upload"
)

type jobPipeline string

const (
	jobPipelineRuntimeReload jobPipeline = "runtime_reload"
	jobPipelineClientMutation jobPipeline = "client_mutation"
	jobPipelineDefault       jobPipeline = "default"
)

type pollingGroupConfig struct {
	Enabled  bool
	Interval time.Duration
}

type connectionSchedule struct {
	groups map[pollingGroup]pollingGroupConfig
}

func newConnectionSchedule(heartbeat time.Duration, runtimeSnapshot time.Duration, usageSnapshot time.Duration, ipPoll time.Duration, ipUpload time.Duration) connectionSchedule {
	return connectionSchedule{
		groups: map[pollingGroup]pollingGroupConfig{
			pollHeartbeat: {Enabled: heartbeat > 0, Interval: heartbeat},
			pollRuntime:   {Enabled: runtimeSnapshot > 0, Interval: runtimeSnapshot},
			pollUsage:     {Enabled: usageSnapshot > 0, Interval: usageSnapshot},
			pollIPPoll:    {Enabled: ipPoll > 0, Interval: ipPoll},
			pollIPUpload:  {Enabled: ipUpload > 0, Interval: ipUpload},
		},
	}
}

func (s connectionSchedule) config(group pollingGroup) pollingGroupConfig {
	return s.groups[group]
}

func jobPipelineForAction(action string) jobPipeline {
	switch action {
	case "runtime.reload":
		return jobPipelineRuntimeReload
	case "client.create", "client.update", "client.rotate_secret", "client.delete":
		return jobPipelineClientMutation
	default:
		return jobPipelineDefault
	}
}

func jobWorkerCountForPipeline(pipeline jobPipeline) int {
	switch pipeline {
	case jobPipelineRuntimeReload:
		return 2
	case jobPipelineClientMutation:
		return 1
	default:
		return 1
	}
}

type jobInflightTracker struct {
	mu    sync.Mutex
	jobIDs map[string]struct{}
}

func newJobInflightTracker() *jobInflightTracker {
	return &jobInflightTracker{
		jobIDs: make(map[string]struct{}),
	}
}

func (t *jobInflightTracker) reserve(jobID string) bool {
	if jobID == "" {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.jobIDs[jobID]; exists {
		return false
	}
	t.jobIDs[jobID] = struct{}{}
	return true
}

func (t *jobInflightTracker) release(jobID string) {
	if jobID == "" {
		return
	}

	t.mu.Lock()
	delete(t.jobIDs, jobID)
	t.mu.Unlock()
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

func newTicker(config pollingGroupConfig) *time.Ticker {
	if !config.Enabled || config.Interval <= 0 {
		return nil
	}
	return time.NewTicker(config.Interval)
}

func sendError(sendErrors chan<- error, err error) {
	select {
	case sendErrors <- err:
	default:
	}
}

func enqueueReceivedJob(
	connectionCtx context.Context,
	agentID string,
	tracker *jobInflightTracker,
	jobQueues map[jobPipeline]chan *gatewayrpc.JobCommand,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
	job *gatewayrpc.JobCommand,
) bool {
	if job == nil {
		return false
	}

	jobID := job.GetId()
	if jobID != "" && !tracker.reserve(jobID) {
		select {
		case <-connectionCtx.Done():
			return false
		case criticalOutbound <- jobAcknowledgementMessage(agentID, jobID, time.Now()):
			return true
		}
	}

	targetQueue := jobQueues[jobPipelineForAction(job.GetAction())]
	select {
	case <-connectionCtx.Done():
		tracker.release(jobID)
		return false
	case targetQueue <- job:
	}

	select {
	case <-connectionCtx.Done():
		tracker.release(jobID)
		return false
	case criticalOutbound <- jobAcknowledgementMessage(agentID, jobID, time.Now()):
		return true
	}
}

func startJobWorkers(
	connectionCtx context.Context,
	agent *runtime.Agent,
	tracker *jobInflightTracker,
	jobQueues map[jobPipeline]chan *gatewayrpc.JobCommand,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	for pipeline, queue := range jobQueues {
		workerCount := jobWorkerCountForPipeline(pipeline)
		for workerIndex := 0; workerIndex < workerCount; workerIndex++ {
			go runJobWorker(connectionCtx, agent, tracker, queue, criticalOutbound)
		}
	}
}

func runJobWorker(
	connectionCtx context.Context,
	agent *runtime.Agent,
	tracker *jobInflightTracker,
	jobQueue <-chan *gatewayrpc.JobCommand,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	for {
		var job *gatewayrpc.JobCommand
		select {
		case <-connectionCtx.Done():
			return
		case job = <-jobQueue:
		}
		if job == nil {
			continue
		}
		jobID := job.GetId()

		jobCtx, cancelJob := context.WithTimeout(connectionCtx, jobExecutionTimeout)
		result := agent.HandleJob(jobCtx, job, time.Now())
		cancelJob()

		select {
		case <-connectionCtx.Done():
			tracker.release(jobID)
			return
		case criticalOutbound <- &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_JobResult{JobResult: result},
		}:
		}
		tracker.release(jobID)
	}
}

func startPollingWorkers(
	connectionCtx context.Context,
	schedule connectionSchedule,
	agent *runtime.Agent,
	telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage,
) {
	startPeriodicPollingWorker(connectionCtx, schedule.config(pollHeartbeat), func(observedAt time.Time) {
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, heartbeatMessage(agent, observedAt)) {
			return
		}
		if connectionCtx.Err() == nil {
			log.Printf("agent heartbeat dropped due to outbound backpressure")
		}
	})

	startPeriodicPollingWorker(connectionCtx, schedule.config(pollRuntime), func(observedAt time.Time) {
		runtimeCtx, cancelRuntime := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
		snapshot, err := agent.BuildRuntimeSnapshot(runtimeCtx, observedAt)
		cancelRuntime()
		if err != nil {
			log.Printf("agent runtime snapshot failed: %v", err)
			return
		}
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
		}) {
			return
		}
		if connectionCtx.Err() == nil {
			log.Printf("agent runtime snapshot dropped due to outbound backpressure")
		}
	})

	startPeriodicPollingWorker(connectionCtx, schedule.config(pollUsage), func(observedAt time.Time) {
		usageCtx, cancelUsage := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
		snapshot, err := agent.BuildUsageSnapshot(usageCtx, observedAt)
		cancelUsage()
		if err != nil {
			log.Printf("agent usage snapshot failed: %v", err)
			return
		}
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
		}) {
			return
		}
		if connectionCtx.Err() == nil {
			log.Printf("agent usage snapshot dropped due to outbound backpressure")
		}
	})

	startPeriodicPollingWorker(connectionCtx, schedule.config(pollIPPoll), func(observedAt time.Time) {
		ipPollCtx, cancelIPPoll := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
		err := agent.PollActiveIPs(ipPollCtx)
		cancelIPPoll()
		if err != nil {
			log.Printf("agent ip poll failed: %v", err)
		}
	})

	startPeriodicPollingWorker(connectionCtx, schedule.config(pollIPUpload), func(observedAt time.Time) {
		snapshot := agent.BuildIPSnapshot(observedAt)
		if len(snapshot.ClientIps) == 0 {
			return
		}
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
		}) {
			return
		}
		if connectionCtx.Err() == nil {
			log.Printf("agent ip snapshot dropped due to outbound backpressure")
		}
	})
}

func startPeriodicPollingWorker(
	connectionCtx context.Context,
	config pollingGroupConfig,
	run func(observedAt time.Time),
) {
	if !config.Enabled || config.Interval <= 0 {
		return
	}

	go func() {
		ticker := time.NewTicker(config.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-connectionCtx.Done():
				return
			case observedAt := <-ticker.C:
				run(observedAt.UTC())
			}
		}
	}()
}

func enqueueOutboundMessage(
	connectionCtx context.Context,
	outbound chan<- *gatewayrpc.ConnectClientMessage,
	message *gatewayrpc.ConnectClientMessage,
) bool {
	if message == nil {
		return false
	}
	if connectionCtx.Err() != nil {
		return false
	}

	select {
	case <-connectionCtx.Done():
		return false
	case outbound <- message:
		return true
	default:
		return false
	}
}

func runConnection(gatewayAddr string, serverName string, stateFile string, credentialsState agentstate.Credentials, agent *runtime.Agent, schedule connectionSchedule) (agentstate.Credentials, error) {
	certificate, err := tls.X509KeyPair([]byte(credentialsState.CertificatePEM), []byte(credentialsState.PrivateKeyPEM))
	if err != nil {
		return credentialsState, err
	}

	conn, err := dialGateway(context.Background(), gatewayAddr, serverName, credentialsState.CAPEM, &certificate)
	if err != nil {
		return credentialsState, err
	}
	defer conn.Close()

	client := gatewayrpc.NewAgentGatewayClient(conn)
	streamConnectCtx, cancelStreamConnect := context.WithTimeout(context.Background(), gatewayStreamConnectTimeout)
	stream, err := client.Connect(streamConnectCtx)
	cancelStreamConnect()
	if err != nil {
		return credentialsState, err
	}

	connectionCtx, cancelConnection := context.WithCancel(stream.Context())
	defer cancelConnection()

	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 32)
	telemetryOutbound := make(chan *gatewayrpc.ConnectClientMessage, 64)
	jobInflight := newJobInflightTracker()
	jobQueues := map[jobPipeline]chan *gatewayrpc.JobCommand{
		jobPipelineRuntimeReload: make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
		jobPipelineClientMutation: make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
		jobPipelineDefault:       make(chan *gatewayrpc.JobCommand, jobQueueCapacity),
	}
	sendErrors := make(chan error, 1)
	sendErrorAndCancel := func(err error) {
		sendError(sendErrors, err)
		cancelConnection()
	}

	go func() {
		for {
			var message *gatewayrpc.ConnectClientMessage
			select {
			case <-connectionCtx.Done():
				return
			case message = <-criticalOutbound:
			default:
				select {
				case <-connectionCtx.Done():
					return
				case message = <-criticalOutbound:
				case message = <-telemetryOutbound:
				}
			}

			if message == nil {
				continue
			}
			if err := stream.Send(message); err != nil {
				sendErrorAndCancel(err)
				return
			}
		}
	}()

	go func() {
		for {
			message, err := stream.Recv()
			if err != nil {
				sendErrorAndCancel(err)
				return
			}
			job := message.GetJob()
			if job == nil {
				continue
			}

			enqueueReceivedJob(connectionCtx, agent.AgentID(), jobInflight, jobQueues, criticalOutbound, job)
		}
	}()
	startJobWorkers(connectionCtx, agent, jobInflight, jobQueues, criticalOutbound)

	if err := sendInitialMessages(criticalOutbound, agent); err != nil {
		cancelConnection()
		return credentialsState, err
	}

	credentialRefreshTimer := newRuntimeCredentialRefreshTimer(credentialsState, time.Now())
	if credentialRefreshTimer != nil {
		defer credentialRefreshTimer.Stop()
	}
	startPollingWorkers(connectionCtx, schedule, agent, telemetryOutbound)

	for {
		select {
		case err := <-sendErrors:
			cancelConnection()
			return credentialsState, err
		case <-timerChan(credentialRefreshTimer):
			refreshCtx, cancelRefresh := context.WithTimeout(connectionCtx, certificateRefreshTimeout)
			updatedCredentials, err := refreshRuntimeCredentialsIfNeeded(refreshCtx, stateFile, credentialsState, client, time.Now())
			cancelRefresh()
			if err != nil {
				log.Printf("agent certificate renewal failed: %v", err)
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

type certificateRenewer interface {
	RenewCertificate(context.Context, *gatewayrpc.RenewCertificateRequest, ...grpc.CallOption) (*gatewayrpc.RenewCertificateResponse, error)
}

func renewRuntimeCredentialsIfNeeded(ctx context.Context, stateFile string, gatewayAddr string, serverName string, current agentstate.Credentials, now time.Time) (agentstate.Credentials, error) {
	if !runtimeCredentialsNeedRefresh(current, now) {
		return current, nil
	}

	certificate, err := tls.X509KeyPair([]byte(current.CertificatePEM), []byte(current.PrivateKeyPEM))
	if err != nil {
		return current, err
	}

	conn, err := dialGateway(ctx, gatewayAddr, serverName, current.CAPEM, &certificate)
	if err != nil {
		return current, err
	}
	defer conn.Close()

	return refreshRuntimeCredentialsIfNeeded(ctx, stateFile, current, gatewayrpc.NewAgentGatewayClient(conn), now)
}

func refreshRuntimeCredentialsIfNeeded(ctx context.Context, stateFile string, current agentstate.Credentials, renewer certificateRenewer, now time.Time) (agentstate.Credentials, error) {
	if !runtimeCredentialsNeedRefresh(current, now) {
		return current, nil
	}

	response, err := renewer.RenewCertificate(ctx, &gatewayrpc.RenewCertificateRequest{
		AgentId: current.AgentID,
	})
	if err != nil {
		return current, err
	}

	updated := current
	updated.CertificatePEM = response.GetCertificatePem()
	updated.PrivateKeyPEM = response.GetPrivateKeyPem()
	updated.CAPEM = response.GetCaPem()
	if response.GetExpiresAtUnix() > 0 {
		updated.ExpiresAt = time.Unix(response.GetExpiresAtUnix(), 0).UTC()
	} else {
		updated.ExpiresAt = time.Time{}
	}

	if err := agentstate.Save(stateFile, updated); err != nil {
		return current, err
	}

	return updated, nil
}

func runtimeCredentialsNeedRefresh(current agentstate.Credentials, now time.Time) bool {
	if current.AgentID == "" {
		return false
	}
	if current.ExpiresAt.IsZero() {
		return false
	}

	return !now.Add(runtimeCertificateRenewWindow).Before(current.ExpiresAt.UTC())
}

func runtimeCredentialRefreshDelay(current agentstate.Credentials, now time.Time) time.Duration {
	if runtimeCredentialsNeedRefresh(current, now) {
		return 0
	}

	refreshAt := current.ExpiresAt.UTC().Add(-runtimeCertificateRenewWindow)
	if !refreshAt.After(now) {
		return 0
	}

	return refreshAt.Sub(now)
}

func newRuntimeCredentialRefreshTimer(current agentstate.Credentials, now time.Time) *time.Timer {
	if current.ExpiresAt.IsZero() {
		return nil
	}

	return time.NewTimer(runtimeCredentialRefreshDelay(current, now))
}

func resetRuntimeCredentialRefreshTimer(timer *time.Timer, delay time.Duration) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(delay)
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}

func timerChan(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}
	return timer.C
}

func sendInitialMessages(outbound chan<- *gatewayrpc.ConnectClientMessage, agent *runtime.Agent) error {
	outbound <- heartbeatMessage(agent, time.Now())

	runtimeCtx, cancelRuntime := context.WithTimeout(context.Background(), runtimeOperationTimeout)
	runtimeSnapshot, err := agent.BuildRuntimeSnapshot(runtimeCtx, time.Now())
	cancelRuntime()
	if err != nil {
		return err
	}
	outbound <- &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: runtimeSnapshot},
	}

	usageCtx, cancelUsage := context.WithTimeout(context.Background(), runtimeOperationTimeout)
	usageSnapshot, err := agent.BuildUsageSnapshot(usageCtx, time.Now())
	cancelUsage()
	if err != nil {
		return err
	}
	outbound <- &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: usageSnapshot},
	}

	ipPollCtx, cancelIPPoll := context.WithTimeout(context.Background(), runtimeOperationTimeout)
	if err := agent.PollActiveIPs(ipPollCtx); err == nil {
		ipSnapshot := agent.BuildIPSnapshot(time.Now())
		if len(ipSnapshot.ClientIps) > 0 {
			outbound <- &gatewayrpc.ConnectClientMessage{
				Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: ipSnapshot},
			}
		}
	}
	cancelIPPoll()

	return nil
}

func heartbeatMessage(agent *runtime.Agent, observedAt time.Time) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Heartbeat{
			Heartbeat: &gatewayrpc.Heartbeat{
				AgentId:        agent.AgentID(),
				NodeName:       agent.NodeName(),
				FleetGroupId:   agent.FleetGroupID(),
				Version:        agent.Version(),
				ObservedAtUnix: observedAt.UTC().Unix(),
			},
		},
	}
}

func jobAcknowledgementMessage(agentID string, jobID string, observedAt time.Time) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobAcknowledgement{
			JobAcknowledgement: &gatewayrpc.JobAcknowledgement{
				AgentId:        agentID,
				JobId:          jobID,
				ObservedAtUnix: observedAt.UTC().Unix(),
			},
		},
	}
}

func dialGateway(ctx context.Context, gatewayAddr string, serverName string, caPEM string, certificate *tls.Certificate) (*grpc.ClientConn, error) {
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM([]byte(caPEM)); !ok {
		return nil, errors.New("failed to append control-plane CA")
	}

	tlsConfig := &tls.Config{
		RootCAs:    pool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	}
	if certificate != nil {
		tlsConfig.Certificates = []tls.Certificate{*certificate}
	}

	return grpc.NewClient(gatewayAddr,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	)
}

func hostName() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown-node"
	}

	return name
}
