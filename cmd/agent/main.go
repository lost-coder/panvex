package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	agentstate "github.com/panvex/panvex/internal/agent/state"
	"github.com/panvex/panvex/internal/agent/runtime"
	"github.com/panvex/panvex/internal/agent/telemt"
	"github.com/panvex/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

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
	caFile := flags.String("ca-file", "", "CA file for initial enrollment")
	enrollmentToken := flags.String("enrollment-token", "", "One-time enrollment token")
	stateFile := flags.String("state-file", "data/agent-state.json", "Agent credential state file")
	nodeName := flags.String("node-name", hostName(), "Node name reported to the control-plane")
	fleetGroupID := flags.String("fleet-group-id", "", "Fleet group identifier reported by the agent")
	version := flags.String("version", "dev", "Agent version")
	telemtURL := flags.String("telemt-url", "http://127.0.0.1:8080", "Local Telemt API URL")
	telemtAuth := flags.String("telemt-auth", "", "Local Telemt authorization value")
	heartbeat := flags.Duration("heartbeat-interval", 15*time.Second, "Heartbeat interval")
	snapshot := flags.Duration("snapshot-interval", time.Minute, "Snapshot interval")
	if err := flags.Parse(args); err != nil {
		return err
	}

	credentialsState, err := loadOrEnroll(*stateFile, *gatewayAddr, *gatewayServerName, *caFile, *enrollmentToken, *nodeName, *version)
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

	for {
		if err := runConnection(*gatewayAddr, *gatewayServerName, credentialsState, agent, *fleetGroupID, *heartbeat, *snapshot); err != nil {
			log.Printf("agent connection ended: %v", err)
		}
		time.Sleep(5 * time.Second)
	}
}

func loadOrEnroll(stateFile string, gatewayAddr string, serverName string, caFile string, enrollmentToken string, nodeName string, version string) (agentstate.Credentials, error) {
	credentialsState, err := agentstate.Load(stateFile)
	if err == nil {
		return credentialsState, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return agentstate.Credentials{}, err
	}
	if enrollmentToken == "" || caFile == "" {
		return agentstate.Credentials{}, errors.New("initial enrollment requires both -enrollment-token and -ca-file")
	}

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return agentstate.Credentials{}, err
	}

	conn, err := dialGateway(context.Background(), gatewayAddr, serverName, string(caPEM), nil)
	if err != nil {
		return agentstate.Credentials{}, err
	}
	defer conn.Close()

	client := gatewayrpc.NewGatewayClient(conn)
	response, err := client.Enroll(context.Background(), &gatewayrpc.EnrollRequest{
		Token:    enrollmentToken,
		NodeName: nodeName,
		Version:  version,
	})
	if err != nil {
		return agentstate.Credentials{}, err
	}

	credentialsState = agentstate.Credentials{
		AgentID:        response.AgentID,
		CertificatePEM: response.CertificatePEM,
		PrivateKeyPEM:  response.PrivateKeyPEM,
		CAPEM:          response.CAPEM,
		GRPCEndpoint:   gatewayAddr,
		GRPCServerName: serverName,
		ExpiresAt:      time.Unix(response.ExpiresAtUnix, 0).UTC(),
	}
	if err := agentstate.Save(stateFile, credentialsState); err != nil {
		return agentstate.Credentials{}, err
	}

	return credentialsState, nil
}

func runConnection(gatewayAddr string, serverName string, credentialsState agentstate.Credentials, agent *runtime.Agent, fleetGroupID string, heartbeatInterval time.Duration, snapshotInterval time.Duration) error {
	certificate, err := tls.X509KeyPair([]byte(credentialsState.CertificatePEM), []byte(credentialsState.PrivateKeyPEM))
	if err != nil {
		return err
	}

	conn, err := dialGateway(context.Background(), gatewayAddr, serverName, credentialsState.CAPEM, &certificate)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := gatewayrpc.NewGatewayClient(conn)
	stream, err := client.Connect(context.Background())
	if err != nil {
		return err
	}

	outbound := make(chan *gatewayrpc.ConnectClientMessage, 32)
	sendErrors := make(chan error, 1)
	go func() {
		for message := range outbound {
			if err := stream.Send(message); err != nil {
				sendErrors <- err
				return
			}
		}
	}()

	go func() {
		for {
			message, err := stream.Recv()
			if err != nil {
				sendErrors <- err
				return
			}
			if message.Job == nil {
				continue
			}

			result := agent.HandleJob(context.Background(), message.Job, time.Now())
			outbound <- &gatewayrpc.ConnectClientMessage{
				JobResult: result,
			}
		}
	}()

	if err := sendHeartbeatAndSnapshot(outbound, agent, fleetGroupID); err != nil {
		return err
	}

	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()
	snapshotTicker := time.NewTicker(snapshotInterval)
	defer snapshotTicker.Stop()

	for {
		select {
		case err := <-sendErrors:
			close(outbound)
			return err
		case <-heartbeatTicker.C:
			outbound <- heartbeatMessage(agent, fleetGroupID, time.Now())
		case <-snapshotTicker.C:
			snapshot, err := agent.BuildSnapshot(context.Background(), time.Now())
			if err != nil {
				return err
			}
			outbound <- &gatewayrpc.ConnectClientMessage{
				Snapshot: snapshot,
			}
		}
	}
}

func sendHeartbeatAndSnapshot(outbound chan<- *gatewayrpc.ConnectClientMessage, agent *runtime.Agent, fleetGroupID string) error {
	outbound <- heartbeatMessage(agent, fleetGroupID, time.Now())

	snapshot, err := agent.BuildSnapshot(context.Background(), time.Now())
	if err != nil {
		return err
	}

	outbound <- &gatewayrpc.ConnectClientMessage{
		Snapshot: snapshot,
	}
	return nil
}

func heartbeatMessage(agent *runtime.Agent, fleetGroupID string, observedAt time.Time) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Heartbeat: &gatewayrpc.Heartbeat{
			AgentID:        agent.AgentID(),
			NodeName:       agent.NodeName(),
			FleetGroupID:   fleetGroupID,
			Version:        agent.Version(),
			ObservedAtUnix: observedAt.UTC().Unix(),
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
		grpc.WithDefaultCallOptions(grpc.CallContentSubtype(gatewayrpc.JSONCodecName)),
	)
}

func hostName() string {
	name, err := os.Hostname()
	if err != nil {
		return "unknown-node"
	}

	return name
}
