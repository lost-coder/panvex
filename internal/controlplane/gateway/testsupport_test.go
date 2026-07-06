package gateway

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/controlplane/agents"
	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/jobs"
	"github.com/lost-coder/panvex/internal/controlplane/metrics"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc/metadata"
)

// stubDeps is a no-op gateway.Deps for the unit tests that exercise the
// job-dispatch / message helpers without a full *server.Server. The cross-
// domain callbacks are not what these tests assert on (they assert on the
// jobs.Service state and the effect channels), so a no-op implementation is
// sufficient and keeps the gateway package free of a server import.
type stubDeps struct{}

func (stubDeps) AuthorizeAgentConnect(context.Context, agenttransport.AgentSession) (string, string, error) {
	return "", "", nil
}
func (stubDeps) ShouldTerminateForRevocation(context.Context, string, string) bool { return false }
func (stubDeps) MarkTransportSwitchResolved(string)                                {}
func (stubDeps) RegisterAgentSession(string, context.CancelFunc) (*agents.Session, func()) {
	return nil, func() {}
}
func (stubDeps) NotifyAgentSession(string)                                           {}
func (stubDeps) ApplyAgentSnapshot(context.Context, AgentSnapshot) error             { return nil }
func (stubDeps) AppendAudit(context.Context, string, string, string, map[string]any) {}
func (stubDeps) RecordClientJobResult(context.Context, string, string, bool, string, string, time.Time) {
}
func (stubDeps) ReconcileDiscoveredClients(context.Context, string, []*gatewayrpc.ClientDetailRecord, bool, time.Time) {
}
func (stubDeps) ResolveClientIDByName(string, string) string { return "" }
func (stubDeps) RenewAgentCertificate(context.Context, string, *gatewayrpc.RenewCertificateRequest) (*gatewayrpc.RenewCertificateResponse, error) {
	return nil, nil
}
func (stubDeps) RecordEnrollmentSteps(context.Context, *gatewayrpc.ReportEnrollmentStepsRequest) (*gatewayrpc.ReportEnrollmentStepsResponse, error) {
	return nil, nil
}
func (stubDeps) HandleInStreamRenewalRequest(context.Context, string, agenttransport.AgentSession, *gatewayrpc.RenewalRequest) {
}

// newTestGateway builds a Gateway backed by a real in-memory jobs.Service
// (clock driven by now) and a no-op Deps. Returns the jobs.Service so tests
// can enqueue/inspect jobs directly, mirroring the pre-extraction tests that
// reached through server.jobs.
func newTestGateway(now func() time.Time) (*Gateway, *jobs.Service) {
	svc := jobs.NewService()
	svc.SetNow(now)
	g := New(Config{
		Deps:   stubDeps{},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Jobs:   svc,
		Obs:    metrics.NewCollectors(),
		Now:    now,
	})
	return g, svc
}

func enqueueJobForAgent(t *testing.T, svc *jobs.Service, agentID string, idempotencyKey string, now time.Time) jobs.Job {
	t.Helper()

	job, err := svc.Enqueue(context.Background(), jobs.CreateJobInput{
		Action:         jobs.ActionRuntimeReload,
		TargetAgentIDs: []string{agentID},
		TTL:            time.Minute,
		IdempotencyKey: idempotencyKey,
		ActorID:        "user-1",
		ReadOnlyAgents: map[string]bool{
			agentID: false,
		},
	}, now)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	return job
}

func heartbeatMessageForTest(nodeName string) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_Heartbeat{
			Heartbeat: &gatewayrpc.Heartbeat{
				NodeName:       nodeName,
				FleetGroupId:   "default",
				Version:        "1.0.0",
				ReadOnly:       false,
				ObservedAtUnix: 1,
			},
		},
	}
}

func jobAcknowledgementMessageForTest(jobID string) *gatewayrpc.ConnectClientMessage {
	return &gatewayrpc.ConnectClientMessage{
		Body: &gatewayrpc.ConnectClientMessage_JobAcknowledgement{
			JobAcknowledgement: &gatewayrpc.JobAcknowledgement{
				JobId:          jobID,
				ObservedAtUnix: 1,
			},
		},
	}
}

// fakeGatewayConnectStream captures outbound ConnectServerMessages so the
// dispatch tests can assert on what the gateway sent.
type fakeGatewayConnectStream struct {
	ctx  context.Context
	sent []*gatewayrpc.ConnectServerMessage
}

func newFakeGatewayConnectStream(ctx context.Context) *fakeGatewayConnectStream {
	return &fakeGatewayConnectStream{
		ctx:  ctx,
		sent: make([]*gatewayrpc.ConnectServerMessage, 0),
	}
}

func (s *fakeGatewayConnectStream) Send(message *gatewayrpc.ConnectServerMessage) error {
	s.sent = append(s.sent, message)
	return nil
}

func (s *fakeGatewayConnectStream) Recv() (*gatewayrpc.ConnectClientMessage, error) {
	return nil, io.EOF
}

func (s *fakeGatewayConnectStream) SetHeader(_ metadata.MD) error {
	return nil
}

func (s *fakeGatewayConnectStream) SendHeader(_ metadata.MD) error {
	return nil
}

func (s *fakeGatewayConnectStream) SetTrailer(_ metadata.MD) {}

func (s *fakeGatewayConnectStream) Context() context.Context {
	return s.ctx
}

func (s *fakeGatewayConnectStream) SendMsg(_ any) error {
	return nil
}

func (s *fakeGatewayConnectStream) RecvMsg(_ any) error {
	return io.EOF
}

type fakeRuntimeReloadClient struct {
	reloadCalls int
}

func (c *fakeRuntimeReloadClient) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return telemt.RuntimeState{}, nil
}

func (c *fakeRuntimeReloadClient) PatchConfig(context.Context, map[string]any, string) (telemt.PatchConfigResult, error) {
	return telemt.PatchConfigResult{}, nil
}

func (c *fakeRuntimeReloadClient) GetManagedConfig(context.Context) (map[string]any, string, error) {
	return nil, "", nil
}

func (c *fakeRuntimeReloadClient) HealthReady(context.Context) (bool, string, error) {
	return true, "", nil
}

func (c *fakeRuntimeReloadClient) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	return telemt.ClientUsageMetricsSnapshot{}, nil
}

func (c *fakeRuntimeReloadClient) FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error) {
	return nil, nil
}

func (c *fakeRuntimeReloadClient) ExecuteRuntimeReload(context.Context) error {
	c.reloadCalls++
	return nil
}

func (c *fakeRuntimeReloadClient) CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeRuntimeReloadClient) UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeRuntimeReloadClient) DeleteClient(context.Context, string) error {
	return nil
}

func (c *fakeRuntimeReloadClient) InvalidateSlowDataCache() {}

func (c *fakeRuntimeReloadClient) FetchSystemInfo(context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, nil
}

func (c *fakeRuntimeReloadClient) FetchDiscoveredUsers(_ context.Context, _ string) ([]telemt.DiscoveredUser, error) {
	return nil, nil
}

func (c *fakeRuntimeReloadClient) ResetUserQuota(context.Context, string) (telemt.ResetUserQuotaResult, error) {
	return telemt.ResetUserQuotaResult{}, nil
}
