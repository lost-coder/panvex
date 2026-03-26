package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
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
)

var errRuntimeCredentialsRefreshed = errors.New("runtime credentials refreshed")

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "bootstrap" {
		return runBootstrapCommand(args[1:], http.DefaultClient)
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
		credentialsState, err = renewRuntimeCredentialsIfNeeded(context.Background(), *stateFile, *gatewayAddr, *gatewayServerName, credentialsState, time.Now())
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
	stream, err := client.Connect(context.Background())
	if err != nil {
		return credentialsState, err
	}

	outbound := make(chan *gatewayrpc.ConnectClientMessage, 32)
	sendErrors := make(chan error, 1)
	go func() {
		for message := range outbound {
			if err := stream.Send(message); err != nil {
				sendError(sendErrors, err)
				return
			}
		}
	}()

	go func() {
		for {
			message, err := stream.Recv()
			if err != nil {
				sendError(sendErrors, err)
				return
			}
			if message.GetJob() == nil {
				continue
			}

			result := agent.HandleJob(context.Background(), message.GetJob(), time.Now())
			outbound <- &gatewayrpc.ConnectClientMessage{
				Body: &gatewayrpc.ConnectClientMessage_JobResult{JobResult: result},
			}
		}
	}()

	if err := sendInitialMessages(outbound, agent); err != nil {
		close(outbound)
		return credentialsState, err
	}

	heartbeatTicker := newTicker(schedule.config(pollHeartbeat))
	if heartbeatTicker != nil {
		defer heartbeatTicker.Stop()
	}
	runtimeTicker := newTicker(schedule.config(pollRuntime))
	if runtimeTicker != nil {
		defer runtimeTicker.Stop()
	}
	usageTicker := newTicker(schedule.config(pollUsage))
	if usageTicker != nil {
		defer usageTicker.Stop()
	}
	ipPollTicker := newTicker(schedule.config(pollIPPoll))
	if ipPollTicker != nil {
		defer ipPollTicker.Stop()
	}
	ipUploadTicker := newTicker(schedule.config(pollIPUpload))
	if ipUploadTicker != nil {
		defer ipUploadTicker.Stop()
	}
	credentialRefreshTimer := newRuntimeCredentialRefreshTimer(credentialsState, time.Now())
	if credentialRefreshTimer != nil {
		defer credentialRefreshTimer.Stop()
	}

	for {
		select {
		case err := <-sendErrors:
			close(outbound)
			return credentialsState, err
		case <-tickerChan(heartbeatTicker):
			outbound <- heartbeatMessage(agent, time.Now())
		case <-tickerChan(runtimeTicker):
			snapshot, err := agent.BuildRuntimeSnapshot(context.Background(), time.Now())
			if err != nil {
				log.Printf("agent runtime snapshot failed: %v", err)
				continue
			}
			outbound <- &gatewayrpc.ConnectClientMessage{
				Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
			}
		case <-tickerChan(usageTicker):
			snapshot, err := agent.BuildUsageSnapshot(context.Background(), time.Now())
			if err != nil {
				log.Printf("agent usage snapshot failed: %v", err)
				continue
			}
			outbound <- &gatewayrpc.ConnectClientMessage{
				Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
			}
		case <-timerChan(credentialRefreshTimer):
			updatedCredentials, err := refreshRuntimeCredentialsIfNeeded(context.Background(), stateFile, credentialsState, client, time.Now())
			if err != nil {
				log.Printf("agent certificate renewal failed: %v", err)
				resetRuntimeCredentialRefreshTimer(credentialRefreshTimer, runtimeCertificateRenewRetry)
				continue
			}
			if updatedCredentials != credentialsState {
				close(outbound)
				return updatedCredentials, errRuntimeCredentialsRefreshed
			}
			resetRuntimeCredentialRefreshTimer(credentialRefreshTimer, runtimeCredentialRefreshDelay(credentialsState, time.Now()))
		case <-tickerChan(ipPollTicker):
			if err := agent.PollActiveIPs(context.Background()); err != nil {
				log.Printf("agent ip poll failed: %v", err)
			}
		case <-tickerChan(ipUploadTicker):
			snapshot := agent.BuildIPSnapshot(time.Now())
			if len(snapshot.ClientIps) == 0 {
				continue
			}
			outbound <- &gatewayrpc.ConnectClientMessage{
				Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: snapshot},
			}
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
		return true
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

	runtimeSnapshot, err := agent.BuildRuntimeSnapshot(context.Background(), time.Now())
	if err != nil {
		return err
	}
	outbound <- &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: runtimeSnapshot},
	}

	usageSnapshot, err := agent.BuildUsageSnapshot(context.Background(), time.Now())
	if err != nil {
		return err
	}
	outbound <- &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: usageSnapshot},
	}

	if err := agent.PollActiveIPs(context.Background()); err == nil {
		ipSnapshot := agent.BuildIPSnapshot(time.Now())
		if len(ipSnapshot.ClientIps) > 0 {
			outbound <- &gatewayrpc.ConnectClientMessage{
				Body: &gatewayrpc.ConnectClientMessage_Snapshot{Snapshot: ipSnapshot},
			}
		}
	}

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
