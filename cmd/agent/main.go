package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

// Gateway client tuning constants. These mirror the control-plane server side
// so NAT/middleboxes cannot silently drop an idle TCP connection and so that
// large discovery snapshots are not truncated by the default 4 MiB cap.
const (
	gatewayKeepaliveTime    = 30 * time.Second
	gatewayKeepaliveTimeout = 10 * time.Second
	gatewayMaxMessageSize   = 16 * 1024 * 1024
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
	}, telemtClient)

	schedule := newConnectionSchedule(cfg.heartbeat, cfg.runtimePoll, cfg.runtimeUpload, cfg.usageSnapshot, cfg.ipPoll, cfg.ipUpload)
	slog.Info("agent starting",
		"agent_id", credentialsState.AgentID,
		"node", cfg.nodeName,
		"gateway", cfg.gatewayAddr,
		"telemt_api", cfg.telemtURL,
		"telemt_metrics", cfg.telemtMetricsURL,
	)

	runRuntimeReconnectLoop(&cfg, &credentialsState, agent, schedule)
	return nil
}

// runRuntimeReconnectLoop is the agent's outer main-loop: refresh certs,
// run the gRPC stream, and reconnect with backoff on failure. Extracted so
// runRuntime stays under the CC threshold.
func runRuntimeReconnectLoop(cfg *runtimeFlags, credentialsState *agentstate.Credentials, agent *runtime.Agent, schedule connectionSchedule) {
	reconnectAttempt := 0
	for {
		refreshCtx, cancelRefresh := context.WithTimeout(context.Background(), certificateRefreshTimeout)
		refreshed, err := renewRuntimeCredentialsIfNeeded(refreshCtx, cfg.stateFile, cfg.gatewayAddr, cfg.gatewayServerName, *credentialsState, time.Now())
		cancelRefresh()
		if err != nil {
			reconnectAttempt++
			slog.Error("certificate refresh failed", "error", err)
			time.Sleep(reconnectDelay(reconnectAttempt))
			continue
		}
		*credentialsState = refreshed

		afterConn, connErr := runConnection(cfg.gatewayAddr, cfg.gatewayServerName, cfg.stateFile, *credentialsState, agent, schedule, cfg.clientDataConcurrency)
		*credentialsState = afterConn
		if connErr == nil || errors.Is(connErr, errRuntimeCredentialsRefreshed) {
			reconnectAttempt = 0
			continue
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
	pollHeartbeat      pollingGroup = "heartbeat"
	pollRuntime        pollingGroup = "runtime"
	pollRuntimeUpload  pollingGroup = "runtime_upload"
	pollUsage          pollingGroup = "usage"
	pollIPPoll         pollingGroup = "ip_poll"
	pollIPUpload       pollingGroup = "ip_upload"
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

func newConnectionSchedule(heartbeat, runtimePoll, runtimeUpload, usageSnapshot, ipPoll, ipUpload time.Duration) connectionSchedule {
	return connectionSchedule{
		groups: map[pollingGroup]pollingGroupConfig{
			pollHeartbeat:     {Enabled: heartbeat > 0, Interval: heartbeat},
			pollRuntime:       {Enabled: runtimePoll > 0, Interval: runtimePoll},
			pollRuntimeUpload: {Enabled: runtimeUpload > 0, Interval: runtimeUpload},
			pollUsage:         {Enabled: usageSnapshot > 0, Interval: usageSnapshot},
			pollIPPoll:        {Enabled: ipPoll > 0, Interval: ipPoll},
			pollIPUpload:      {Enabled: ipUpload > 0, Interval: ipUpload},
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
	case "telemetry.refresh_diagnostics":
		return jobPipelineRuntimeReload
	case "client.create", "client.update", "client.rotate_secret", "client.delete":
		return jobPipelineClientMutation
	default:
		return jobPipelineDefault
	}
}

func shouldSendRuntimeSnapshotAfterJob(action string, success bool) bool {
	if !success {
		return false
	}

	return action == "telemetry.refresh_diagnostics"
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
		slog.Debug("job completed", "job_id", jobID, "action", job.GetAction(), "success", result.Success)

		if shouldSendRuntimeSnapshotAfterJob(job.GetAction(), result.Success) {
			runtimeCtx, cancelRuntime := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
			snapshot, err := agent.BuildRuntimeSnapshot(runtimeCtx, time.Now())
			cancelRuntime()
			if err != nil {
				result.Success = false
				result.Message = "diagnostics refresh failed: " + err.Error()
			} else {
				select {
				case <-connectionCtx.Done():
					tracker.release(jobID)
					return
				case criticalOutbound <- &gatewayrpc.ConnectClientMessage{
					Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
				}:
				}
			}
		}
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
	startPeriodicPollingWorker(connectionCtx, schedule.config(pollHeartbeat),
		makeHeartbeatTick(connectionCtx, agent, telemetryOutbound))

	runtimeBuffer := runtime.NewRuntimeRingBuffer(8)
	startRuntimePollWorker(connectionCtx, schedule.config(pollRuntime), agent, runtimeBuffer)
	startRuntimeUploadWorker(connectionCtx, schedule.config(pollRuntimeUpload), runtimeBuffer, telemetryOutbound)

	startPeriodicPollingWorker(connectionCtx, schedule.config(pollUsage),
		makeUsageSnapshotTick(connectionCtx, agent, telemetryOutbound))
	startPeriodicPollingWorker(connectionCtx, schedule.config(pollIPPoll),
		makeIPPollTick(connectionCtx, agent))
	startPeriodicPollingWorker(connectionCtx, schedule.config(pollIPUpload),
		makeIPUploadTick(connectionCtx, agent, telemetryOutbound))
}

func makeHeartbeatTick(connectionCtx context.Context, agent *runtime.Agent, telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage) func(time.Time) {
	return func(observedAt time.Time) {
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, heartbeatMessage(agent, observedAt)) {
			slog.Debug("heartbeat sent", "agent_id", agent.AgentID())
			return
		}
		if connectionCtx.Err() == nil {
			slog.Warn("heartbeat dropped due to outbound backpressure")
		}
	}
}

func makeUsageSnapshotTick(connectionCtx context.Context, agent *runtime.Agent, telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage) func(time.Time) {
	return func(observedAt time.Time) {
		usageCtx, cancelUsage := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
		snapshot, err := agent.BuildUsageSnapshot(usageCtx, observedAt)
		cancelUsage()
		if err != nil {
			slog.Error("usage snapshot failed", "error", err)
			return
		}
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
		}) {
			slog.Debug("usage snapshot enqueued", "agent_id", agent.AgentID())
			return
		}
		if connectionCtx.Err() == nil {
			slog.Warn("usage snapshot dropped due to outbound backpressure")
		}
	}
}

func makeIPPollTick(connectionCtx context.Context, agent *runtime.Agent) func(time.Time) {
	return func(observedAt time.Time) {
		ipPollCtx, cancelIPPoll := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
		err := agent.PollActiveIPs(ipPollCtx)
		cancelIPPoll()
		if err != nil {
			slog.Error("ip poll failed", "error", err)
		}
	}
}

func makeIPUploadTick(connectionCtx context.Context, agent *runtime.Agent, telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage) func(time.Time) {
	return func(observedAt time.Time) {
		snapshot := agent.BuildIPSnapshot(observedAt)
		if len(snapshot.ClientIps) == 0 {
			return
		}
		if enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
		}) {
			slog.Debug("ip snapshot enqueued", "agent_id", agent.AgentID(), "client_ips", len(snapshot.ClientIps))
			return
		}
		if connectionCtx.Err() == nil {
			slog.Warn("ip snapshot dropped due to outbound backpressure")
		}
	}
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

// startRuntimePollWorker polls Telemt at a fast interval and stores samples in the ring buffer.
func startRuntimePollWorker(
	connectionCtx context.Context,
	config pollingGroupConfig,
	agent *runtime.Agent,
	buffer *runtime.RuntimeRingBuffer,
) {
	if !config.Enabled || config.Interval <= 0 {
		return
	}

	go runRuntimePollLoop(connectionCtx, config, agent, buffer)
}

func runRuntimePollLoop(
	connectionCtx context.Context,
	config pollingGroupConfig,
	agent *runtime.Agent,
	buffer *runtime.RuntimeRingBuffer,
) {
	consecutiveFailures := 0
	for {
		delay := nextRuntimePollDelay(agent, config, consecutiveFailures)
		observedAt, ok := waitRuntimePollTick(connectionCtx, delay)
		if !ok {
			return
		}
		if performRuntimePoll(connectionCtx, agent, buffer, observedAt, &consecutiveFailures) {
			continue
		}
	}
}

func nextRuntimePollDelay(agent *runtime.Agent, config pollingGroupConfig, consecutiveFailures int) time.Duration {
	delay := agent.RuntimeSnapshotInterval(config.Interval, runtimeInitializationFastInterval, time.Now())
	if consecutiveFailures > 0 {
		backoff := time.Duration(consecutiveFailures) * config.Interval
		if backoff > 5*time.Minute {
			backoff = 5 * time.Minute
		}
		delay = backoff
	}
	return delay
}

func waitRuntimePollTick(connectionCtx context.Context, delay time.Duration) (time.Time, bool) {
	timer := time.NewTimer(delay)
	select {
	case <-connectionCtx.Done():
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		return time.Time{}, false
	case observedAt := <-timer.C:
		return observedAt, true
	}
}

// performRuntimePoll executes one snapshot fetch, updates failure counters,
// and pushes a sample on success. Always returns true so the loop continues.
func performRuntimePoll(
	connectionCtx context.Context,
	agent *runtime.Agent,
	buffer *runtime.RuntimeRingBuffer,
	observedAt time.Time,
	consecutiveFailures *int,
) bool {
	runtimeCtx, cancelRuntime := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
	snapshot, err := agent.BuildRuntimeSnapshot(runtimeCtx, observedAt.UTC())
	cancelRuntime()
	if err != nil {
		*consecutiveFailures++
		if *consecutiveFailures <= 3 || *consecutiveFailures%10 == 0 {
			slog.Error("runtime poll failed", "attempt", *consecutiveFailures, "error", err)
		}
		return true
	}
	*consecutiveFailures = 0
	buffer.Push(runtime.RuntimeSample{
		ObservedAt: observedAt.UTC(),
		Snapshot:   snapshot,
	})
	return true
}

// startRuntimeUploadWorker drains the ring buffer, aggregates samples, and sends one snapshot.
func startRuntimeUploadWorker(
	connectionCtx context.Context,
	config pollingGroupConfig,
	buffer *runtime.RuntimeRingBuffer,
	telemetryOutbound chan<- *gatewayrpc.ConnectClientMessage,
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
			case <-ticker.C:
				snapshot := buffer.DrainAndAggregate()
				if snapshot == nil {
					continue
				}
				if enqueueOutboundMessage(connectionCtx, telemetryOutbound, &gatewayrpc.ConnectClientMessage{
					Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
				}) {
					slog.Debug("runtime snapshot enqueued")
					continue
				}
				if connectionCtx.Err() == nil {
					slog.Warn("runtime upload dropped due to outbound backpressure")
				}
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

// startOutboundPump spawns the goroutine that pulls messages off the
// critical and telemetry channels and forwards them on the gateway
// stream. Critical messages are drained before the telemetry channel
// is even consulted so a backed-up snapshot pipeline cannot starve a
// heartbeat.
func startOutboundPump(
	connectionCtx context.Context,
	streamWG *sync.WaitGroup,
	stream gatewayrpc.AgentGateway_ConnectClient,
	criticalOutbound, telemetryOutbound <-chan *gatewayrpc.ConnectClientMessage,
	sendErrorAndCancel func(error),
) {
	streamWG.Add(1)
	go func() {
		defer streamWG.Done()
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
}

// startInboundPump spawns the goroutine that consumes the gateway
// stream and routes job commands to the worker queues plus client-
// data requests to bounded handler goroutines.
func startInboundPump(
	connectionCtx context.Context,
	streamWG *sync.WaitGroup,
	stream gatewayrpc.AgentGateway_ConnectClient,
	agent *runtime.Agent,
	jobInflight *jobInflightTracker,
	jobQueues map[jobPipeline]chan *gatewayrpc.JobCommand,
	criticalOutbound chan *gatewayrpc.ConnectClientMessage,
	clientDataSem chan struct{},
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
	credentialRefreshTimer *time.Timer,
	sendErrors <-chan error,
) (agentstate.Credentials, error) {
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

func runConnection(gatewayAddr string, serverName string, stateFile string, credentialsState agentstate.Credentials, agent *runtime.Agent, schedule connectionSchedule, clientDataConcurrency int) (agentstate.Credentials, error) {
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
	stream, err := connectStreamWithSetupTimeout(gatewayStreamConnectTimeout, func(ctx context.Context) (gatewayrpc.AgentGateway_ConnectClient, error) {
		return client.Connect(ctx)
	})
	if err != nil {
		return credentialsState, err
	}
	// gosec G706: slog uses structured key/value attributes — neither agent
	// id nor gateway address is interpreted as a format string, so the
	// taint-analysis warning is a false positive.
	//nolint:gosec // G706: structured logging, no format-string injection vector
	slog.Info("connected to control-plane", "agent_id", agent.AgentID(), "gateway", gatewayAddr)

	connectionCtx, cancelConnection := context.WithCancel(stream.Context())
	defer cancelConnection()

	// Q4.U-P-08: graceful drain. Every goroutine spawned for this
	// connection adds 1 to streamWG and defers wg.Done(); runConnection
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
	sendErrorAndCancel := func(err error) {
		sendError(sendErrors, err)
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
	startInboundPump(connectionCtx, &streamWG, stream, agent, jobInflight, jobQueues, criticalOutbound, clientDataSem, sendErrorAndCancel)
	startJobWorkers(connectionCtx, agent, jobInflight, jobQueues, criticalOutbound)

	if err := sendInitialMessages(criticalOutbound, agent); err != nil {
		cancelConnection()
		return credentialsState, err
	}
	slog.Info("initial sync completed", "agent_id", agent.AgentID(), "node", agent.NodeName())

	credentialRefreshTimer := newRuntimeCredentialRefreshTimer(credentialsState, time.Now())
	if credentialRefreshTimer != nil {
		defer credentialRefreshTimer.Stop()
	}
	startPollingWorkers(connectionCtx, schedule, agent, telemetryOutbound)

	return runConnectionMainLoop(connectionCtx, cancelConnection, credentialsState, stateFile, client, credentialRefreshTimer, sendErrors)
}

type certificateRenewer interface {
	RenewCertificate(context.Context, *gatewayrpc.RenewCertificateRequest, ...grpc.CallOption) (*gatewayrpc.RenewCertificateResponse, error)
}

func renewRuntimeCredentialsIfNeeded(ctx context.Context, stateFile string, gatewayAddr string, serverName string, current agentstate.Credentials, now time.Time) (agentstate.Credentials, error) {
	if !runtimeCredentialsNeedRefresh(current, now) {
		return current, nil
	}
	if runtimeCredentialsNeedRecovery(current, now) {
		return recoverRuntimeCredentialsIfNeeded(ctx, stateFile, current, nil, now)
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

func runtimeCredentialsNeedRecovery(current agentstate.Credentials, now time.Time) bool {
	if strings.TrimSpace(current.PanelURL) == "" {
		return false
	}
	if current.ExpiresAt.IsZero() {
		return false
	}

	return !current.ExpiresAt.UTC().After(now.UTC())
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

func connectStreamWithSetupTimeout(
	timeout time.Duration,
	connect func(context.Context) (gatewayrpc.AgentGateway_ConnectClient, error),
) (gatewayrpc.AgentGateway_ConnectClient, error) {
	connectCtx, cancelConnect := context.WithCancel(context.Background())
	var setupTimer *time.Timer
	if timeout > 0 {
		setupTimer = time.AfterFunc(timeout, cancelConnect)
	}

	stream, err := connect(connectCtx)
	if setupTimer != nil {
		setupTimer.Stop()
	}
	if err != nil {
		cancelConnect()
		return nil, err
	}

	// On success the stream owns connectCtx — cancelling it would kill the
	// stream immediately because gRPC derives the stream context from the
	// one passed to Connect(). The context will be released when the stream
	// closes naturally.
	_ = cancelConnect //nolint:ineffassign // cancel is transferred to the stream lifecycle
	return stream, nil
}

func sendInitialMessages(outbound chan<- *gatewayrpc.ConnectClientMessage, agent *runtime.Agent) error {
	outbound <- heartbeatMessage(agent, time.Now())

	runtimeCtx, cancelRuntime := context.WithTimeout(context.Background(), runtimeOperationTimeout)
	runtimeSnapshot, err := agent.BuildRuntimeSnapshot(runtimeCtx, time.Now())
	cancelRuntime()
	if err != nil {
		return fmt.Errorf("initial runtime snapshot failed: %w", err)
	}
	outbound <- &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: runtimeSnapshot},
	}
	slog.Info("initial runtime snapshot sent", "agent_id", agent.AgentID(), "node", agent.NodeName())

	usageCtx, cancelUsage := context.WithTimeout(context.Background(), runtimeOperationTimeout)
	usageSnapshot, err := agent.BuildUsageSnapshot(usageCtx, time.Now())
	cancelUsage()
	if err == nil {
		outbound <- &gatewayrpc.ConnectClientMessage{
			Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: usageSnapshot},
		}
	} else {
		slog.Warn("initial usage snapshot unavailable, continuing without metrics", "error", err)
	}

	ipPollCtx, cancelIPPoll := context.WithTimeout(context.Background(), runtimeOperationTimeout)
	if err := agent.PollActiveIPs(ipPollCtx); err == nil {
		ipSnapshot := agent.BuildIPSnapshot(time.Now())
		slog.Info("initial ip snapshot built", "client_ips_count", len(ipSnapshot.ClientIps))
		if len(ipSnapshot.ClientIps) > 0 {
			outbound <- &gatewayrpc.ConnectClientMessage{
				Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: ipSnapshot},
			}
		}
	} else {
		slog.Warn("initial ip poll unavailable, continuing without active IPs", "error", err)
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
	if !pool.AppendCertsFromPEM([]byte(caPEM)) {
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
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                gatewayKeepaliveTime,
			Timeout:             gatewayKeepaliveTimeout,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(gatewayMaxMessageSize),
			grpc.MaxCallSendMsgSize(gatewayMaxMessageSize),
		),
	)
}

func handleClientDataRequest(
	connectionCtx context.Context,
	agent *runtime.Agent,
	criticalOutbound chan<- *gatewayrpc.ConnectClientMessage,
	req *gatewayrpc.ClientDataRequest,
) {
	reqCtx, cancel := context.WithTimeout(connectionCtx, runtimeOperationTimeout)
	response := agent.HandleClientDataRequest(reqCtx, req.GetRequestId())
	cancel()

	select {
	case <-connectionCtx.Done():
	case criticalOutbound <- &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_ClientDataResponse{ClientDataResponse: response},
	}:
	}
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
