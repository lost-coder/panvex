package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/agent/runtime"
	"github.com/panvex/panvex/internal/agent/telemt"
	agentstate "github.com/panvex/panvex/internal/agent/state"
	"github.com/panvex/panvex/internal/gatewayrpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc"
)

func TestJobPipelineForActionRoutesRuntimeReload(t *testing.T) {
	pipeline := jobPipelineForAction("runtime.reload")
	if pipeline != jobPipelineRuntimeReload {
		t.Fatalf("jobPipelineForAction(runtime.reload) = %q, want %q", pipeline, jobPipelineRuntimeReload)
	}
}

func TestJobPipelineForActionRoutesDiagnosticsRefreshToRuntimePipeline(t *testing.T) {
	pipeline := jobPipelineForAction("telemetry.refresh_diagnostics")
	if pipeline != jobPipelineRuntimeReload {
		t.Fatalf("jobPipelineForAction(telemetry.refresh_diagnostics) = %q, want %q", pipeline, jobPipelineRuntimeReload)
	}
}

func TestJobPipelineForActionRoutesClientMutations(t *testing.T) {
	clientActions := []string{
		"client.create",
		"client.update",
		"client.rotate_secret",
		"client.delete",
	}
	for _, action := range clientActions {
		pipeline := jobPipelineForAction(action)
		if pipeline != jobPipelineClientMutation {
			t.Fatalf("jobPipelineForAction(%q) = %q, want %q", action, pipeline, jobPipelineClientMutation)
		}
	}
}

func TestJobPipelineForActionRoutesUnknownActionsToDefault(t *testing.T) {
	pipeline := jobPipelineForAction("users.create")
	if pipeline != jobPipelineDefault {
		t.Fatalf("jobPipelineForAction(users.create) = %q, want %q", pipeline, jobPipelineDefault)
	}
}

func TestJobWorkerCountForPipelineMatchesConcurrencyPolicy(t *testing.T) {
	if count := jobWorkerCountForPipeline(jobPipelineRuntimeReload); count != 2 {
		t.Fatalf("jobWorkerCountForPipeline(runtime_reload) = %d, want %d", count, 2)
	}
	if count := jobWorkerCountForPipeline(jobPipelineClientMutation); count != 1 {
		t.Fatalf("jobWorkerCountForPipeline(client_mutation) = %d, want %d", count, 1)
	}
	if count := jobWorkerCountForPipeline(jobPipelineDefault); count != 1 {
		t.Fatalf("jobWorkerCountForPipeline(default) = %d, want %d", count, 1)
	}
}

func TestShouldSendRuntimeSnapshotAfterJobOnlyForSuccessfulDiagnosticsRefresh(t *testing.T) {
	if !shouldSendRuntimeSnapshotAfterJob("telemetry.refresh_diagnostics", true) {
		t.Fatal("shouldSendRuntimeSnapshotAfterJob(refresh, true) = false, want true")
	}
	if shouldSendRuntimeSnapshotAfterJob("telemetry.refresh_diagnostics", false) {
		t.Fatal("shouldSendRuntimeSnapshotAfterJob(refresh, false) = true, want false")
	}
	if shouldSendRuntimeSnapshotAfterJob("runtime.reload", true) {
		t.Fatal("shouldSendRuntimeSnapshotAfterJob(runtime.reload, true) = true, want false")
	}
}

func TestSendInitialMessagesContinuesWhenUsageMetricsAreUnavailable(t *testing.T) {
	telemtClient := &fakeInitialSyncTelemtClient{
		state: telemt.RuntimeState{
			Version: "2026.03",
			Gates: telemt.RuntimeGates{
				AcceptingNewConnections: true,
				MERuntimeReady:          true,
				StartupStatus:           "ready",
				StartupStage:            "steady_state",
				StartupProgressPct:      100,
			},
			Initialization: telemt.RuntimeInitialization{
				Status:        "ready",
				CurrentStage:  "steady_state",
				ProgressPct:   100,
				TransportMode: "direct",
			},
			ConnectionTotals: telemt.RuntimeConnectionTotals{
				CurrentConnections: 3,
				ActiveUsers:        2,
			},
			Diagnostics: telemt.RuntimeDiagnostics{
				State:          "fresh",
				SystemInfoJSON: `{"version":"2026.03"}`,
			},
		},
		metricsErr: errors.New("telemt metrics request failed with status 503"),
	}
	agent := runtime.New(runtime.Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "test",
	}, telemtClient)

	outbound := make(chan *gatewayrpc.ConnectClientMessage, 4)
	var logs strings.Builder
	originalWriter := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(originalWriter)

	err := sendInitialMessages(outbound, agent)
	if err != nil {
		t.Fatalf("sendInitialMessages() error = %v, want nil when only usage metrics are unavailable", err)
	}
	if len(outbound) != 2 {
		t.Fatalf("len(outbound) = %d, want %d (heartbeat + runtime snapshot)", len(outbound), 2)
	}
	first := <-outbound
	second := <-outbound
	if first.GetHeartbeat() == nil {
		t.Fatal("first outbound message = nil heartbeat, want heartbeat")
	}
	if second.GetSnapshot() == nil {
		t.Fatal("second outbound message = nil snapshot, want runtime snapshot")
	}
	if !strings.Contains(logs.String(), "initial usage snapshot unavailable") {
		t.Fatalf("logs = %q, want initial usage snapshot warning", logs.String())
	}
}

func TestConnectStreamWithSetupTimeoutKeepsStreamContextAliveAfterSuccessfulConnect(t *testing.T) {
	stream, err := connectStreamWithSetupTimeout(20*time.Millisecond, func(ctx context.Context) (gatewayrpc.AgentGateway_ConnectClient, error) {
		return &fakeAgentGatewayConnectClient{ctx: ctx}, nil
	})
	if err != nil {
		t.Fatalf("connectStreamWithSetupTimeout() error = %v", err)
	}

	select {
	case <-stream.Context().Done():
		t.Fatal("stream context canceled immediately after successful connect")
	default:
	}

	time.Sleep(50 * time.Millisecond)

	select {
	case <-stream.Context().Done():
		t.Fatal("stream context canceled after setup timeout elapsed")
	default:
	}
}

func TestRunJobWorkerSendsDiagnosticsSnapshotBeforeSuccessResult(t *testing.T) {
	connectionCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	telemtClient := &fakeDiagnosticsRefreshTelemtClient{
		state: telemt.RuntimeState{
			Version: "2026.03",
			Gates: telemt.RuntimeGates{
				AcceptingNewConnections: true,
				MERuntimeReady:          true,
				StartupStatus:           "ready",
				StartupStage:            "steady_state",
				StartupProgressPct:      100,
			},
			Initialization: telemt.RuntimeInitialization{
				Status:        "ready",
				CurrentStage:  "steady_state",
				ProgressPct:   100,
				TransportMode: "direct",
			},
			ConnectionTotals: telemt.RuntimeConnectionTotals{
				CurrentConnections: 4,
				ActiveUsers:        2,
			},
			Diagnostics: telemt.RuntimeDiagnostics{
				State:          "fresh",
				SystemInfoJSON: `{"version":"2026.03"}`,
			},
		},
	}
	agent := runtime.New(runtime.Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "test",
	}, telemtClient)

	tracker := newJobInflightTracker()
	jobQueue := make(chan *gatewayrpc.JobCommand, 1)
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 4)

	go runJobWorker(connectionCtx, agent, tracker, jobQueue, criticalOutbound)

	jobQueue <- &gatewayrpc.JobCommand{
		Id:     "job-refresh",
		Action: "telemetry.refresh_diagnostics",
	}

	first := <-criticalOutbound
	second := <-criticalOutbound

	if first.GetSnapshot() == nil {
		t.Fatal("first outbound message = nil snapshot, want diagnostics snapshot first")
	}
	if second.GetJobResult() == nil {
		t.Fatal("second outbound message = nil job result, want success result after snapshot")
	}
	if !second.GetJobResult().GetSuccess() {
		t.Fatalf("job result success = false, want true: %s", second.GetJobResult().GetMessage())
	}
}

func TestRunJobWorkerMarksDiagnosticsRefreshFailedWhenSnapshotBuildFails(t *testing.T) {
	connectionCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	telemtClient := &fakeDiagnosticsRefreshTelemtClient{
		fetchErrAfterInvalidation: true,
	}
	agent := runtime.New(runtime.Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "default",
		Version:      "test",
	}, telemtClient)

	tracker := newJobInflightTracker()
	jobQueue := make(chan *gatewayrpc.JobCommand, 1)
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 2)

	go runJobWorker(connectionCtx, agent, tracker, jobQueue, criticalOutbound)

	jobQueue <- &gatewayrpc.JobCommand{
		Id:     "job-refresh-fail",
		Action: "telemetry.refresh_diagnostics",
	}

	message := <-criticalOutbound
	if message.GetJobResult() == nil {
		t.Fatal("outbound message = nil job result, want failure result")
	}
	if message.GetJobResult().GetSuccess() {
		t.Fatal("job result success = true, want false when snapshot build fails")
	}
	if !strings.Contains(message.GetJobResult().GetMessage(), "diagnostics refresh failed") {
		t.Fatalf("job result message = %q, want diagnostics refresh failure", message.GetJobResult().GetMessage())
	}
}

func TestEnqueueOutboundMessageReturnsTrueWhenQueued(t *testing.T) {
	connectionCtx := context.Background()
	outbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	message := heartbeatMessageForTest("node-a")

	queued := enqueueOutboundMessage(connectionCtx, outbound, message)
	if !queued {
		t.Fatal("enqueueOutboundMessage() = false, want true")
	}
	if len(outbound) != 1 {
		t.Fatalf("len(outbound) = %d, want %d", len(outbound), 1)
	}
}

func TestEnqueueOutboundMessageReturnsFalseWhenQueueFull(t *testing.T) {
	connectionCtx := context.Background()
	outbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	outbound <- heartbeatMessageForTest("stale")

	queued := enqueueOutboundMessage(connectionCtx, outbound, heartbeatMessageForTest("latest"))
	if queued {
		t.Fatal("enqueueOutboundMessage() = true, want false")
	}
	if len(outbound) != 1 {
		t.Fatalf("len(outbound) = %d, want %d", len(outbound), 1)
	}
}

func TestEnqueueOutboundMessageReturnsFalseWhenContextCancelled(t *testing.T) {
	connectionCtx, cancel := context.WithCancel(context.Background())
	cancel()

	outbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	queued := enqueueOutboundMessage(connectionCtx, outbound, heartbeatMessageForTest("node-a"))
	if queued {
		t.Fatal("enqueueOutboundMessage() = true, want false")
	}
	if len(outbound) != 0 {
		t.Fatalf("len(outbound) = %d, want %d", len(outbound), 0)
	}
}

func TestJobInflightTrackerReserveRelease(t *testing.T) {
	tracker := newJobInflightTracker()

	if !tracker.reserve("job-1") {
		t.Fatal("reserve(job-1) = false, want true")
	}
	if tracker.reserve("job-1") {
		t.Fatal("reserve(job-1) = true, want false for duplicate")
	}

	tracker.release("job-1")

	if !tracker.reserve("job-1") {
		t.Fatal("reserve(job-1) after release = false, want true")
	}
}

func TestEnqueueReceivedJobQueuesAndAcknowledges(t *testing.T) {
	connectionCtx := context.Background()
	tracker := newJobInflightTracker()
	jobQueues := map[jobPipeline]chan *gatewayrpc.JobCommand{
		jobPipelineRuntimeReload: make(chan *gatewayrpc.JobCommand, 1),
		jobPipelineClientMutation: make(chan *gatewayrpc.JobCommand, 1),
		jobPipelineDefault:       make(chan *gatewayrpc.JobCommand, 1),
	}
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	job := &gatewayrpc.JobCommand{
		Id:     "job-1",
		Action: "runtime.reload",
	}

	queued := enqueueReceivedJob(connectionCtx, "agent-1", tracker, jobQueues, criticalOutbound, job)
	if !queued {
		t.Fatal("enqueueReceivedJob() = false, want true")
	}
	if len(jobQueues[jobPipelineRuntimeReload]) != 1 {
		t.Fatalf("len(runtime reload queue) = %d, want %d", len(jobQueues[jobPipelineRuntimeReload]), 1)
	}
	if len(criticalOutbound) != 1 {
		t.Fatalf("len(criticalOutbound) = %d, want %d", len(criticalOutbound), 1)
	}

	ack := <-criticalOutbound
	if ack.GetJobAcknowledgement() == nil {
		t.Fatal("ack body = nil, want job acknowledgement")
	}
	if ack.GetJobAcknowledgement().GetJobId() != "job-1" {
		t.Fatalf("ack job id = %q, want %q", ack.GetJobAcknowledgement().GetJobId(), "job-1")
	}
}

func TestEnqueueReceivedJobSkipsDuplicateQueueEntry(t *testing.T) {
	connectionCtx := context.Background()
	tracker := newJobInflightTracker()
	jobQueues := map[jobPipeline]chan *gatewayrpc.JobCommand{
		jobPipelineRuntimeReload: make(chan *gatewayrpc.JobCommand, 2),
		jobPipelineClientMutation: make(chan *gatewayrpc.JobCommand, 1),
		jobPipelineDefault:       make(chan *gatewayrpc.JobCommand, 1),
	}
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 2)
	job := &gatewayrpc.JobCommand{
		Id:     "job-dup",
		Action: "runtime.reload",
	}

	firstQueued := enqueueReceivedJob(connectionCtx, "agent-1", tracker, jobQueues, criticalOutbound, job)
	secondQueued := enqueueReceivedJob(connectionCtx, "agent-1", tracker, jobQueues, criticalOutbound, job)

	if !firstQueued {
		t.Fatal("first enqueueReceivedJob() = false, want true")
	}
	if !secondQueued {
		t.Fatal("second enqueueReceivedJob() = false, want true")
	}
	if len(jobQueues[jobPipelineRuntimeReload]) != 1 {
		t.Fatalf("len(runtime reload queue) = %d, want %d", len(jobQueues[jobPipelineRuntimeReload]), 1)
	}
	if len(criticalOutbound) != 2 {
		t.Fatalf("len(criticalOutbound) = %d, want %d", len(criticalOutbound), 2)
	}
}

func TestEnqueueReceivedJobQueuesCommandWithoutIdentifier(t *testing.T) {
	connectionCtx := context.Background()
	tracker := newJobInflightTracker()
	jobQueues := map[jobPipeline]chan *gatewayrpc.JobCommand{
		jobPipelineRuntimeReload: make(chan *gatewayrpc.JobCommand, 1),
		jobPipelineClientMutation: make(chan *gatewayrpc.JobCommand, 1),
		jobPipelineDefault:       make(chan *gatewayrpc.JobCommand, 1),
	}
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	job := &gatewayrpc.JobCommand{
		Action: "runtime.reload",
	}

	queued := enqueueReceivedJob(connectionCtx, "agent-1", tracker, jobQueues, criticalOutbound, job)
	if !queued {
		t.Fatal("enqueueReceivedJob() = false, want true")
	}
	if len(jobQueues[jobPipelineRuntimeReload]) != 1 {
		t.Fatalf("len(runtime reload queue) = %d, want %d", len(jobQueues[jobPipelineRuntimeReload]), 1)
	}
	if len(criticalOutbound) != 1 {
		t.Fatalf("len(criticalOutbound) = %d, want %d", len(criticalOutbound), 1)
	}
}

func TestLoadRuntimeCredentialsReturnsSavedState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	expected := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "cert-pem",
		PrivateKeyPEM:  "key-pem",
		CAPEM:          "ca-pem",
		GRPCEndpoint:   "grpc.panel.example.com:443",
		GRPCServerName: "grpc.panel.example.com",
	}
	if err := agentstate.Save(statePath, expected); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	credentials, err := loadRuntimeCredentials(statePath)
	if err != nil {
		t.Fatalf("loadRuntimeCredentials() error = %v", err)
	}
	if credentials.AgentID != expected.AgentID {
		t.Fatalf("credentials.AgentID = %q, want %q", credentials.AgentID, expected.AgentID)
	}
	if credentials.GRPCEndpoint != expected.GRPCEndpoint {
		t.Fatalf("credentials.GRPCEndpoint = %q, want %q", credentials.GRPCEndpoint, expected.GRPCEndpoint)
	}
}

func heartbeatMessageForTest(nodeName string) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Heartbeat{
			Heartbeat: &gatewayrpc.Heartbeat{
				NodeName:       nodeName,
				FleetGroupId:   "default",
				Version:        "1.0.0",
				ObservedAtUnix: 1,
			},
		},
	}
}

func TestLoadRuntimeCredentialsRequiresBootstrapState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "missing-agent-state.json")

	_, err := loadRuntimeCredentials(statePath)
	if err == nil {
		t.Fatal("loadRuntimeCredentials() error = nil, want bootstrap requirement")
	}
	if !strings.Contains(err.Error(), "bootstrap the agent first") {
		t.Fatalf("loadRuntimeCredentials() error = %q, want bootstrap guidance", err.Error())
	}
}

func TestReconnectDelayCapsBackoff(t *testing.T) {
	if delay := reconnectDelay(1); delay != time.Second {
		t.Fatalf("reconnectDelay(1) = %v, want %v", delay, time.Second)
	}
	if delay := reconnectDelay(3); delay != 4*time.Second {
		t.Fatalf("reconnectDelay(3) = %v, want %v", delay, 4*time.Second)
	}
	if delay := reconnectDelay(10); delay != 15*time.Second {
		t.Fatalf("reconnectDelay(10) = %v, want %v", delay, 15*time.Second)
	}
}

func TestNewConnectionScheduleDisablesZeroIntervals(t *testing.T) {
	schedule := newConnectionSchedule(15*time.Second, time.Minute, 0, 25*time.Second, 0)

	if !schedule.config(pollHeartbeat).Enabled {
		t.Fatal("heartbeat poll disabled, want enabled")
	}
	if schedule.config(pollHeartbeat).Interval != 15*time.Second {
		t.Fatalf("heartbeat interval = %v, want %v", schedule.config(pollHeartbeat).Interval, 15*time.Second)
	}
	if schedule.config(pollUsage).Enabled {
		t.Fatal("usage poll enabled, want disabled for zero interval")
	}
	if schedule.config(pollIPUpload).Enabled {
		t.Fatal("ip upload enabled, want disabled for zero interval")
	}
}

func TestRunBootstrapCommandSavesIssuedState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("request.Method = %q, want %q", r.Method, http.MethodPost)
		}
		if r.URL.Path != "/api/agent/bootstrap" {
			t.Fatalf("request.URL.Path = %q, want %q", r.URL.Path, "/api/agent/bootstrap")
		}
		if r.Header.Get("Authorization") != "Bearer bootstrap-token" {
			t.Fatalf("request.Authorization = %q, want %q", r.Header.Get("Authorization"), "Bearer bootstrap-token")
		}

		var request struct {
			NodeName string `json:"node_name"`
			Version  string `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		if request.NodeName != "node-a" {
			t.Fatalf("request.NodeName = %q, want %q", request.NodeName, "node-a")
		}
		if request.Version != "1.2.3" {
			t.Fatalf("request.Version = %q, want %q", request.Version, "1.2.3")
		}

		if err := json.NewEncoder(w).Encode(map[string]any{
			"agent_id":         "agent-123",
			"certificate_pem":  "cert-pem",
			"private_key_pem":  "key-pem",
			"ca_pem":           "ca-pem",
			"grpc_endpoint":    "grpc.panel.example.com:443",
			"grpc_server_name": "grpc.panel.example.com",
			"expires_at_unix":  time.Date(2026, time.March, 16, 18, 0, 0, 0, time.UTC).Unix(),
		}); err != nil {
			t.Fatalf("Encode(response) error = %v", err)
		}
	}))
	defer server.Close()

	err := runBootstrapCommand([]string{
		"-panel-url", server.URL,
		"-enrollment-token", "bootstrap-token",
		"-state-file", statePath,
		"-node-name", "node-a",
		"-version", "1.2.3",
	}, server.Client())
	if err != nil {
		t.Fatalf("runBootstrapCommand() error = %v", err)
	}

	credentials, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if credentials.AgentID != "agent-123" {
		t.Fatalf("credentials.AgentID = %q, want %q", credentials.AgentID, "agent-123")
	}
	if credentials.GRPCEndpoint != "grpc.panel.example.com:443" {
		t.Fatalf("credentials.GRPCEndpoint = %q, want %q", credentials.GRPCEndpoint, "grpc.panel.example.com:443")
	}
	if credentials.GRPCServerName != "grpc.panel.example.com" {
		t.Fatalf("credentials.GRPCServerName = %q, want %q", credentials.GRPCServerName, "grpc.panel.example.com")
	}
}

func TestRunBootstrapCommandRejectsExistingStateWithoutForce(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	if err := agentstate.Save(statePath, agentstate.Credentials{
		AgentID:        "agent-existing",
		CertificatePEM: "cert",
		PrivateKeyPEM:  "key",
		CAPEM:          "ca",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	err := runBootstrapCommand([]string{
		"-panel-url", "https://panel.example.com",
		"-enrollment-token", "bootstrap-token",
		"-state-file", statePath,
	}, nil)
	if err == nil {
		t.Fatal("runBootstrapCommand() error = nil, want existing state rejection")
	}
	if !strings.Contains(err.Error(), "-force") {
		t.Fatalf("runBootstrapCommand() error = %q, want mention of -force", err.Error())
	}
}

func TestRunBootstrapCommandAllowsOverwriteWithForce(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	if err := agentstate.Save(statePath, agentstate.Credentials{
		AgentID:        "agent-existing",
		CertificatePEM: "old-cert",
		PrivateKeyPEM:  "old-key",
		CAPEM:          "old-ca",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{
			"agent_id":         "agent-new",
			"certificate_pem":  "new-cert",
			"private_key_pem":  "new-key",
			"ca_pem":           "new-ca",
			"grpc_endpoint":    "panel.example.com:8443",
			"grpc_server_name": "panel.example.com",
			"expires_at_unix":  time.Date(2026, time.March, 16, 19, 0, 0, 0, time.UTC).Unix(),
		}); err != nil {
			t.Fatalf("Encode(response) error = %v", err)
		}
	}))
	defer server.Close()

	err := runBootstrapCommand([]string{
		"-panel-url", server.URL,
		"-enrollment-token", "bootstrap-token",
		"-state-file", statePath,
		"-force",
	}, server.Client())
	if err != nil {
		t.Fatalf("runBootstrapCommand() error = %v", err)
	}

	credentials, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if credentials.AgentID != "agent-new" {
		t.Fatalf("credentials.AgentID = %q, want %q", credentials.AgentID, "agent-new")
	}
}

func TestRefreshRuntimeCredentialsIfNeededRenewsAndPersistsExpiringState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	now := time.Date(2026, time.March, 19, 10, 0, 0, 0, time.UTC)
	current := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "old-cert",
		PrivateKeyPEM:  "old-key",
		CAPEM:          "old-ca",
		GRPCEndpoint:   "panel.example.com:8443",
		GRPCServerName: "panel.example.com",
		ExpiresAt:      now.Add(30 * time.Minute),
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	renewer := &fakeCertificateRenewer{
		response: &gatewayrpc.RenewCertificateResponse{
			CertificatePem: "new-cert",
			PrivateKeyPem:  "new-key",
			CaPem:          "new-ca",
			ExpiresAtUnix:  now.Add(30 * 24 * time.Hour).Unix(),
		},
	}

	updated, err := refreshRuntimeCredentialsIfNeeded(context.Background(), statePath, current, renewer, now)
	if err != nil {
		t.Fatalf("refreshRuntimeCredentialsIfNeeded() error = %v", err)
	}
	if renewer.request == nil {
		t.Fatal("renewer.request = nil, want renewal call")
	}
	if renewer.request.GetAgentId() != current.AgentID {
		t.Fatalf("renewer.request.AgentId = %q, want %q", renewer.request.GetAgentId(), current.AgentID)
	}
	if updated.CertificatePEM != "new-cert" {
		t.Fatalf("updated.CertificatePEM = %q, want %q", updated.CertificatePEM, "new-cert")
	}

	persisted, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted.CertificatePEM != "new-cert" {
		t.Fatalf("persisted.CertificatePEM = %q, want %q", persisted.CertificatePEM, "new-cert")
	}
}

func TestRefreshRuntimeCredentialsIfNeededSkipsZeroExpiryState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "agent-state.json")
	now := time.Date(2026, time.March, 19, 10, 0, 0, 0, time.UTC)
	current := agentstate.Credentials{
		AgentID:        "agent-123",
		CertificatePEM: "old-cert",
		PrivateKeyPEM:  "old-key",
		CAPEM:          "old-ca",
		GRPCEndpoint:   "panel.example.com:8443",
		GRPCServerName: "panel.example.com",
	}
	if err := agentstate.Save(statePath, current); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	renewer := &fakeCertificateRenewer{
		response: &gatewayrpc.RenewCertificateResponse{
			CertificatePem: "new-cert",
			PrivateKeyPem:  "new-key",
			CaPem:          "new-ca",
			ExpiresAtUnix:  now.Add(30 * 24 * time.Hour).Unix(),
		},
	}

	updated, err := refreshRuntimeCredentialsIfNeeded(context.Background(), statePath, current, renewer, now)
	if err != nil {
		t.Fatalf("refreshRuntimeCredentialsIfNeeded() error = %v", err)
	}
	if renewer.request != nil {
		t.Fatal("renewer.request != nil, want no renewal call")
	}
	if updated != current {
		t.Fatalf("updated = %#v, want %#v", updated, current)
	}

	persisted, err := agentstate.Load(statePath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted != current {
		t.Fatalf("persisted = %#v, want %#v", persisted, current)
	}
}

type fakeCertificateRenewer struct {
	request  *gatewayrpc.RenewCertificateRequest
	response *gatewayrpc.RenewCertificateResponse
	err      error
}

type fakeAgentGatewayConnectClient struct {
	ctx context.Context
}

func (c *fakeAgentGatewayConnectClient) Header() (metadata.MD, error) {
	return metadata.MD{}, nil
}

func (c *fakeAgentGatewayConnectClient) Trailer() metadata.MD {
	return metadata.MD{}
}

func (c *fakeAgentGatewayConnectClient) CloseSend() error {
	return nil
}

func (c *fakeAgentGatewayConnectClient) Context() context.Context {
	return c.ctx
}

func (c *fakeAgentGatewayConnectClient) Send(*gatewayrpc.ConnectClientMessage) error {
	return nil
}

func (c *fakeAgentGatewayConnectClient) Recv() (*gatewayrpc.ConnectServerMessage, error) {
	<-c.ctx.Done()
	return nil, c.ctx.Err()
}

func (c *fakeAgentGatewayConnectClient) SendMsg(any) error {
	return nil
}

func (c *fakeAgentGatewayConnectClient) RecvMsg(any) error {
	<-c.ctx.Done()
	return c.ctx.Err()
}

type fakeInitialSyncTelemtClient struct {
	state      telemt.RuntimeState
	metricsErr error
	activeIPs  []telemt.UserActiveIPs
}

func (c *fakeInitialSyncTelemtClient) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return c.state, nil
}

func (c *fakeInitialSyncTelemtClient) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	if c.metricsErr != nil {
		return telemt.ClientUsageMetricsSnapshot{}, c.metricsErr
	}
	return telemt.ClientUsageMetricsSnapshot{}, nil
}

func (c *fakeInitialSyncTelemtClient) FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error) {
	return c.activeIPs, nil
}

func (c *fakeInitialSyncTelemtClient) ExecuteRuntimeReload(context.Context) error {
	return nil
}

func (c *fakeInitialSyncTelemtClient) CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeInitialSyncTelemtClient) UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeInitialSyncTelemtClient) DeleteClient(context.Context, string) error {
	return nil
}

func (c *fakeInitialSyncTelemtClient) InvalidateSlowDataCache() {}

type fakeDiagnosticsRefreshTelemtClient struct {
	state                     telemt.RuntimeState
	invalidateSlowDataCalls   int
	fetchErrAfterInvalidation bool
}

func (c *fakeDiagnosticsRefreshTelemtClient) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	if c.fetchErrAfterInvalidation && c.invalidateSlowDataCalls > 0 {
		return telemt.RuntimeState{}, context.DeadlineExceeded
	}
	return c.state, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	return telemt.ClientUsageMetricsSnapshot{}, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error) {
	return nil, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) ExecuteRuntimeReload(context.Context) error {
	return nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) DeleteClient(context.Context, string) error {
	return nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) InvalidateSlowDataCache() {
	c.invalidateSlowDataCalls++
}

func (r *fakeCertificateRenewer) RenewCertificate(_ context.Context, request *gatewayrpc.RenewCertificateRequest, _ ...grpc.CallOption) (*gatewayrpc.RenewCertificateResponse, error) {
	r.request = request
	if r.err != nil {
		return nil, r.err
	}

	return r.response, nil
}
