package conn

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/jobs"
	"github.com/lost-coder/panvex/internal/agent/runtime"
	agentstate "github.com/lost-coder/panvex/internal/agent/state"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	agentTransport "github.com/lost-coder/panvex/internal/agent/transport"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

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

	err := sendInitialMessages(t.Context(), outbound, agent)
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

// TestReconnectDelayJitterBounds guards D1: each attempt's delay must be
// full-jittered within [ceiling/2, ceiling], where the ceiling follows the
// exponential schedule capped at reconnectMaxDelay. Mirrors the panel-side
// jitter() in agenttransport/outbound.go so both transport directions
// de-synchronise herd reconnects the same way.
func TestReconnectDelayJitterBounds(t *testing.T) {
	cases := []struct {
		attempt int
		ceiling time.Duration
	}{
		{attempt: 0, ceiling: time.Second}, // clamped to attempt 1
		{attempt: 1, ceiling: time.Second},
		{attempt: 3, ceiling: 4 * time.Second},
		{attempt: 6, ceiling: 32 * time.Second},
		{attempt: 7, ceiling: reconnectMaxDelay},
		{attempt: 50, ceiling: reconnectMaxDelay},
	}
	for _, tc := range cases {
		for i := 0; i < 200; i++ {
			delay := reconnectDelay(tc.attempt)
			if delay < tc.ceiling/2 || delay > tc.ceiling {
				t.Fatalf("reconnectDelay(%d) = %v, want within [%v, %v]",
					tc.attempt, delay, tc.ceiling/2, tc.ceiling)
			}
		}
	}
}

// TestReconnectDelayIsJittered asserts the delay actually varies between
// calls — a deterministic implementation (the D1 bug) returns one value.
func TestReconnectDelayIsJittered(t *testing.T) {
	seen := make(map[time.Duration]struct{})
	for i := 0; i < 100; i++ {
		seen[reconnectDelay(10)] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatal("expected jittered delays to vary across 100 samples, got a single value")
	}
}

// TestWaitWithCancelHonoursContextCancellation verifies that the helper
// replacing the bare time.Sleep in runRuntimeReconnectLoop returns
// promptly when the supervisor ctx is cancelled, rather than waiting
// out the full backoff delay (~45s in production at max attempt).
func TestWaitWithCancelHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	start := time.Now()

	go func() {
		// 60s mirrors the worst-case backoff window the bug report cites.
		done <- waitWithCancel(ctx, 30*time.Second)
	}()

	// Let the goroutine enter the timer wait, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waitWithCancel error = %v, want context.Canceled", err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("waitWithCancel took %v after cancel, want <1s", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatalf("waitWithCancel did not return within 1s after cancel")
	}
}

// TestWaitWithCancelExpiresOnTimer verifies waitWithCancel returns nil
// (no error) when the timer fires before ctx is cancelled.
func TestWaitWithCancelExpiresOnTimer(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	if err := waitWithCancel(ctx, 10*time.Millisecond); err != nil {
		t.Fatalf("waitWithCancel error = %v, want nil", err)
	}
}

// TestReconnectBackoffHonoursContextCancellation drives the reconnect
// pattern (the loop's inner select{timer, ctx.Done}) directly, asserting
// that a cancellation while sitting in the backoff sleep unblocks the
// loop within milliseconds — not after the full reconnectDelay (up to
// 45s). This guards against regressions to the bare-time.Sleep code
// path that this task removed.
func TestReconnectBackoffHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		// Simulate the production loop body: do "work" that fails, then
		// sleep with reconnectDelay, repeat. The supervisor ctx must
		// short-circuit the sleep.
		attempt := 0
		for {
			if err := ctx.Err(); err != nil {
				done <- err
				return
			}
			attempt++
			if waitErr := waitWithCancel(ctx, reconnectDelay(attempt)); waitErr != nil {
				done <- waitErr
				return
			}
			// Saturate at the max backoff so a regression that ignored
			// ctx would block this goroutine for 45s.
			if attempt > 5 {
				done <- errors.New("loop did not exit despite ctx cancellation")
				return
			}
		}
	}()

	// Give the goroutine time to enter waitWithCancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("loop exit error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("reconnect loop did not exit on cancellation within 1s")
	}
}

func TestNewConnectionScheduleDisablesZeroIntervals(t *testing.T) {
	schedule := NewSchedule(15*time.Second, 15*time.Second, time.Minute, 0, 25*time.Second, 0)

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

func TestSelectTransportPicksDialByDefault(t *testing.T) {
	ca := newTestCA(t)
	agentCert := ca.issueClientCert(t, "agent")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: agentCert.Certificate[0]})
	keyDER, err := x509.MarshalECPrivateKey(agentCert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	creds := agentstate.Credentials{
		TransportMode:  "", // default — should pick dial
		CertificatePEM: string(certPEM),
		PrivateKeyPEM:  string(keyPEM),
		CAPEM:          string(ca.certPEM),
	}
	dialCfg := agentTransport.DialConfig{
		GatewayAddr: "127.0.0.1:9999",
		ServerName:  "panel",
		CAPEM:       string(ca.certPEM),
	}

	tr, err := selectTransport(creds, dialCfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.(addresser); ok {
		t.Fatal("expected dial transport, got listen transport")
	}
}

func TestSelectTransportPicksListenWhenStateSaysListen(t *testing.T) {
	ca := newTestCA(t)
	agentCert := ca.issueClientCert(t, "agent")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: agentCert.Certificate[0]})
	keyDER, err := x509.MarshalECPrivateKey(agentCert.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	creds := agentstate.Credentials{
		TransportMode:  "listen",
		ListenAddr:     "127.0.0.1:0",
		CertificatePEM: string(certPEM),
		PrivateKeyPEM:  string(keyPEM),
		CAPEM:          string(ca.certPEM),
	}

	tr, err := selectTransport(creds, agentTransport.DialConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tr.(addresser); !ok {
		t.Fatal("expected listen transport (addresser), got dial transport")
	}
}

func TestSelectTransportRejectsInvalidKeypairInListenMode(t *testing.T) {
	creds := agentstate.Credentials{
		TransportMode:  "listen",
		ListenAddr:     "127.0.0.1:0",
		CertificatePEM: "not-a-cert",
		PrivateKeyPEM:  "not-a-key",
		CAPEM:          "not-a-ca",
	}

	_, err := selectTransport(creds, agentTransport.DialConfig{})
	if err == nil {
		t.Fatal("expected error for invalid keypair, got nil")
	}
}

func TestStartInboundPumpRoutesRenewalResponseToChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	renewalResp := &gatewayrpc.RenewalResponse{
		CertificatePem: "new-cert",
		CaPem:          "ca-pem",
		ExpiresAtUnix:  1234567890,
	}
	serverMsg := &gatewayrpc.ConnectServerMessage{
		Body: &gatewayrpc.ConnectServerMessage_RenewalResponse{
			RenewalResponse: renewalResp,
		},
	}
	stream := &renewalTestBidiStream{
		messages:       []*gatewayrpc.ConnectServerMessage{serverMsg},
		cancelAfterAll: cancel,
	}

	agent := runtime.New(runtime.Config{
		AgentID:  "agent-1",
		NodeName: "node-a",
	}, nil)
	tracker := jobs.NewInflightTracker()
	jobQueues := map[jobs.Pipeline]chan *gatewayrpc.JobCommand{
		jobs.PipelineRuntimeReload:  make(chan *gatewayrpc.JobCommand, 1),
		jobs.PipelineClientMutation: make(chan *gatewayrpc.JobCommand, 1),
		jobs.PipelineDefault:        make(chan *gatewayrpc.JobCommand, 1),
	}
	criticalOutbound := make(chan *gatewayrpc.ConnectClientMessage, 1)
	renewalResponses := make(chan *gatewayrpc.RenewalResponse, 1)
	clientDataSem := make(chan struct{}, 1)

	var wg sync.WaitGroup
	startInboundPump(ctx, &wg, stream, agent, tracker, jobQueues, criticalOutbound, clientDataSem, renewalResponses, func(error) {})

	select {
	case got := <-renewalResponses:
		if got.GetCertificatePem() != "new-cert" {
			t.Fatalf("renewalResponses cert = %q, want %q", got.GetCertificatePem(), "new-cert")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for renewal response to be routed")
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

// addresser is the interface exposed by listenTransport to report its bound
// address. We use it here to distinguish listen vs dial transport without
// depending on the unexported concrete type.
type addresser interface {
	Address() string
}

// renewalTestBidiStream is a minimal BidiStream that returns a fixed list of
// server messages then blocks (or returns context error).
type renewalTestBidiStream struct {
	messages       []*gatewayrpc.ConnectServerMessage
	pos            int
	cancelAfterAll context.CancelFunc
}

func (s *renewalTestBidiStream) Send(*gatewayrpc.ConnectClientMessage) error { return nil }
func (s *renewalTestBidiStream) Recv() (*gatewayrpc.ConnectServerMessage, error) {
	if s.pos < len(s.messages) {
		msg := s.messages[s.pos]
		s.pos++
		return msg, nil
	}
	if s.cancelAfterAll != nil {
		s.cancelAfterAll()
	}
	return nil, context.Canceled
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

func (c *fakeInitialSyncTelemtClient) CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeInitialSyncTelemtClient) UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeInitialSyncTelemtClient) DeleteClient(context.Context, string) error {
	return nil
}

func (c *fakeInitialSyncTelemtClient) FetchSystemInfo(context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, nil
}

func (c *fakeInitialSyncTelemtClient) FetchDiscoveredUsers(context.Context, string) ([]telemt.DiscoveredUser, error) {
	return nil, nil
}

func (c *fakeInitialSyncTelemtClient) InvalidateSlowDataCache() {}

func (c *fakeInitialSyncTelemtClient) ResetUserQuota(context.Context, string) (telemt.ResetUserQuotaResult, error) {
	return telemt.ResetUserQuotaResult{}, nil
}

func (c *fakeInitialSyncTelemtClient) PatchConfig(context.Context, map[string]any, string) (telemt.PatchConfigResult, error) {
	return telemt.PatchConfigResult{}, nil
}

func (c *fakeInitialSyncTelemtClient) GetManagedConfig(context.Context) (map[string]any, string, error) {
	return nil, "", nil
}

func (c *fakeInitialSyncTelemtClient) HealthReady(context.Context) (bool, string, error) {
	return true, "", nil
}

func (c *fakeInitialSyncTelemtClient) SubmitReload(context.Context, string, int, string, string) (telemt.ReloadAccepted, error) {
	return telemt.ReloadAccepted{}, nil
}

func (c *fakeInitialSyncTelemtClient) GetReloadStatus(context.Context, uint64) (telemt.ReloadStatus, error) {
	return telemt.ReloadStatus{}, nil
}
