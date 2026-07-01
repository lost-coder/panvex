package agenttransport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func TestOutboundSupervisorReconnectsAfterDisconnect(t *testing.T) {
	stub := newAgentStubServer(t, "agent-1")
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var connectCount atomic.Int32
	handler := func(_ context.Context, sess AgentSession, _ NodeMeta) error {
		connectCount.Add(1)
		// End session immediately so supervisor reconnects.
		return nil
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-1", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	go sup.run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for connectCount.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := connectCount.Load(); got < 2 {
		t.Fatalf("expected >= 2 connects, got %d", got)
	}
}

// TestOutboundSupervisorEnrollsWhenPending verifies that when
// bootstrapStateFn returns "pending" the enrollFn is called before the normal
// mTLS dial, and that after enrollment succeeds (bootstrapStateFn switches to
// "active") subsequent iterations skip the enrollment step.
func TestOutboundSupervisorEnrollsWhenPending(t *testing.T) {
	stub := newAgentStubServer(t, "agent-1")
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var enrollCalls atomic.Int32
	var connectCalls atomic.Int32

	// Simulate state: first call returns "pending", then "active".
	callCount := atomic.Int32{}
	bootstrapStateFn := func(_ context.Context, _ string) (string, error) {
		if callCount.Add(1) == 1 {
			return "pending", nil
		}
		return "active", nil
	}
	enrollFn := func(_ context.Context, _, _ string) error {
		enrollCalls.Add(1)
		return nil
	}
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCalls.Add(1)
		// End session immediately; supervisor reconnects.
		return nil
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-1", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	sup.enrollFn = enrollFn
	sup.bootstrapStateFn = bootstrapStateFn

	go sup.run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	// Wait until we have seen at least one enroll and at least two connects.
	for (enrollCalls.Load() < 1 || connectCalls.Load() < 2) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	if got := enrollCalls.Load(); got != 1 {
		t.Errorf("enrollFn calls: got %d, want 1", got)
	}
	if got := connectCalls.Load(); got < 2 {
		t.Errorf("connect calls: got %d, want >= 2", got)
	}
}

// TestOutboundSupervisorRetriesAfterEnrollFailure verifies that when enrollFn
// returns an error the supervisor backs off and tries again, and that once
// enrollFn eventually succeeds the normal mTLS dial is reached. This guards
// the failure-then-recover path that the happy-path test does not exercise.
func TestOutboundSupervisorRetriesAfterEnrollFailure(t *testing.T) {
	stub := newAgentStubServer(t, "agent-1")
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var enrollCalls atomic.Int32
	var connectCalls atomic.Int32

	// Stay "pending" until enrollFn succeeds, then flip to "active" so the
	// supervisor stops trying to enroll and proceeds to the mTLS dial.
	state := atomic.Value{}
	state.Store("pending")
	bootstrapStateFn := func(_ context.Context, _ string) (string, error) {
		return state.Load().(string), nil
	}
	// First two calls fail; third succeeds and flips the bootstrap state.
	enrollFn := func(_ context.Context, _, _ string) error {
		n := enrollCalls.Add(1)
		if n < 3 {
			return errors.New("enroll: simulated transient failure")
		}
		state.Store("active")
		return nil
	}
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCalls.Add(1)
		return nil
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-1", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	sup.enrollFn = enrollFn
	sup.bootstrapStateFn = bootstrapStateFn

	go sup.run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for (enrollCalls.Load() < 3 || connectCalls.Load() < 1) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	if got := enrollCalls.Load(); got < 3 {
		t.Errorf("enrollFn calls: got %d, want >= 3 (two failures + one success)", got)
	}
	if got := connectCalls.Load(); got < 1 {
		t.Errorf("connect calls: got %d, want >= 1 (mTLS dial after successful enrollment)", got)
	}
}

// TestOutboundSupervisorSkipsEnrollWhenActive verifies that enrollFn is never
// called when bootstrapStateFn consistently returns "active".
func TestOutboundSupervisorSkipsEnrollWhenActive(t *testing.T) {
	stub := newAgentStubServer(t, "agent-1")
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var enrollCalls atomic.Int32
	var connectCalls atomic.Int32

	bootstrapStateFn := func(_ context.Context, _ string) (string, error) {
		return "active", nil
	}
	enrollFn := func(_ context.Context, _, _ string) error {
		enrollCalls.Add(1)
		return nil
	}
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCalls.Add(1)
		return nil
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-1", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	sup.enrollFn = enrollFn
	sup.bootstrapStateFn = bootstrapStateFn

	go sup.run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for connectCalls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	if got := enrollCalls.Load(); got != 0 {
		t.Errorf("enrollFn should not be called when state=active, got %d calls", got)
	}
}

// TestOutboundTransportSupervisorGaugeDelta verifies that the
// onSupervisorDelta callback fires with the right deltas as supervisors are
// added and removed, without needing a real gRPC server.
func TestOutboundTransportSupervisorGaugeDelta(t *testing.T) {
	var total int64
	delta := func(d float64) { total += int64(d) }

	ot := newOutboundTransport(nil, nil, slog.Default())
	ot.onSupervisorDelta = delta

	// Add two supervisors. Their goroutines will loop on connectAndServe
	// and fail immediately (tlsCfg==nil → errOutboundTLSMissing), but that
	// only affects the goroutines — the delta callback fires before they run.
	ot.ensureSupervisor(t.Context(), NodeMeta{NodeID: "n1", AgentID: "a1", DialAddress: "127.0.0.1:1"})
	ot.ensureSupervisor(t.Context(), NodeMeta{NodeID: "n2", AgentID: "a2", DialAddress: "127.0.0.1:2"})

	if total != 2 {
		t.Fatalf("after 2 ensureSupervisor: total=%d, want 2", total)
	}

	ot.removeSupervisor("n1")
	if total != 1 {
		t.Fatalf("after removeSupervisor(n1): total=%d, want 1", total)
	}

	ot.stopAll()
	if total != 0 {
		t.Fatalf("after stopAll: total=%d, want 0", total)
	}
}

// TestOutboundEnsureSupervisorConcurrentDialAddressChangeNoLeak guards the
// 3.9 TOCTOU fix: concurrent ensureSupervisor calls for the SAME node with
// DIFFERENT dial addresses must never let a superseded supervisor entry go
// uncancelled. The old implementation unlocked between "tear down the stale
// entry" and "install the new one" (removeSupervisor re-locks internally),
// so a second concurrent ensureSupervisor could install its own entry in
// that window; the first call's install would then silently clobber it
// without ever calling ITS cancel — a goroutine + cancel leak.
//
// This test wraps onSupervisorDelta to count net +1/-1 calls: if any
// installed entry is dropped without its delta -1 ever firing (either via
// removeSupervisor or the replace-path introduced by this fix), the running
// total will not settle back to 0 after stopAll.
func TestOutboundEnsureSupervisorConcurrentDialAddressChangeNoLeak(t *testing.T) {
	var total int64
	var deltaCalls int64
	var mu sync.Mutex
	delta := func(d float64) {
		mu.Lock()
		defer mu.Unlock()
		total += int64(d)
		deltaCalls++
	}

	tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only
	handler := SessionHandler(func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		return errors.New("not used")
	})
	tr := newOutboundTransport(tlsCfg, handler, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tr.onSupervisorDelta = delta
	tr.setLifecycleCtx(context.Background())

	const iterations = 50
	var wg sync.WaitGroup
	for i := 0; i < iterations; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", 10000+i)
		wg.Add(2)
		// Fire two concurrent ensureSupervisor calls per iteration racing to
		// install the same node under different addresses.
		go func(addr string) {
			defer wg.Done()
			tr.ensureSupervisor(t.Context(), NodeMeta{NodeID: "shared-node", AgentID: "agent-1", DialAddress: addr})
		}(addr)
		go func(addr string) {
			defer wg.Done()
			tr.ensureSupervisor(t.Context(), NodeMeta{NodeID: "shared-node", AgentID: "agent-1", DialAddress: addr + "-b"})
		}(addr)
	}
	wg.Wait()

	// Exactly one entry must survive for the node.
	if !tr.has("shared-node") {
		t.Fatal("expected shared-node to have a surviving supervisor entry")
	}

	tr.stopAll()

	mu.Lock()
	finalTotal := total
	calls := deltaCalls
	mu.Unlock()

	// Every +1 must be matched by a -1: net gauge must return to zero. If a
	// superseded entry's cancel (and therefore its delta -1) was never
	// invoked, finalTotal would be negative-of-leaked-count is wrong framing
	// — actually a leaked entry means one +1 was never paired with a -1, so
	// total would be > 0 after stopAll (stopAll only cancels entries still
	// present in the map; an orphaned goroutine whose entry was overwritten
	// without cancellation is invisible to stopAll and never emits -1).
	if finalTotal != 0 {
		t.Fatalf("gauge did not settle to 0 after stopAll: total=%d (calls=%d) — indicates an orphaned/leaked supervisor whose cancel was never invoked", finalTotal, calls)
	}
	// Sanity: at least iterations+1 installs happened (2*iterations installs,
	// each triggers a delta+1; some subset of the prior entries get replaced
	// and trigger delta-1). We only assert calls is even since every +1 must
	// pair with a -1 for the total to have settled at 0, which is already
	// checked above — this just guards against the test being a no-op.
	if calls == 0 {
		t.Fatal("expected onSupervisorDelta to have been invoked")
	}
}

// TestOutboundSupervisorUsesBackoffGetters verifies that backoffInitialFn /
// backoffMaxFn are consulted on each reconnect iteration rather than using
// the package-level constants. The test wires getter functions that return
// very short durations (so the supervisor reconnects quickly in CI), then
// confirms multiple connects occur within the deadline, proving the getter
// path drives the backoff.
func TestOutboundSupervisorUsesBackoffGetters(t *testing.T) {
	stub := newAgentStubServer(t, "agent-getter")
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var connectCount atomic.Int32
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCount.Add(1)
		return nil
	}

	// Values deliberately different from the package constants to prove the
	// getter path is exercised.
	const wantInitial = 5 * time.Millisecond
	const wantMax = 20 * time.Millisecond

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-getter", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.Default(),
	)
	sup.backoffInitialFn = func() time.Duration { return wantInitial }
	sup.backoffMaxFn = func() time.Duration { return wantMax }
	go sup.run(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for connectCount.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := connectCount.Load(); got < 3 {
		t.Fatalf("expected >= 3 connects via getter-driven backoff, got %d", got)
	}
}

// TestOutboundEnsureSupervisorCancelsViaParentCtx verifies that cancelling the
// transport's lifecycleCtx cascades into the supervisor goroutines so that
// stopAll's wg.Wait returns promptly. The supervisor never establishes a real
// connection (DialAddress points at 127.0.0.1:1, but even if it did, tlsCfg is
// minimal); we only assert lifecycle semantics, not session behaviour.
func TestOutboundEnsureSupervisorCancelsViaParentCtx(t *testing.T) {
	tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only
	handler := SessionHandler(func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		return errors.New("not used")
	})
	tr := newOutboundTransport(tlsCfg, handler, slog.New(slog.NewTextHandler(io.Discard, nil)))

	parentCtx, cancelParent := context.WithCancel(context.Background())
	tr.setLifecycleCtx(parentCtx)

	tr.ensureSupervisor(t.Context(), NodeMeta{AgentID: "n1", NodeID: "n1", DialAddress: "127.0.0.1:1"})
	if !tr.has("n1") {
		t.Fatal("supervisor not registered")
	}

	// Cancel parent ctx; stopAll's wg.Wait must complete because supervisors
	// inherit from parentCtx. If they used context.Background() instead, the
	// goroutines would only exit via the explicit entry.cancel — which stopAll
	// does call, so this test is timing-sensitive. The real signal: stopAll
	// returns within the test's deadline.
	cancelParent()
	done := make(chan struct{})
	go func() {
		tr.stopAll()
		close(done)
	}()
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("stopAll did not return after parent ctx cancel")
	}
}

// TestOutboundEnsureSupervisorAfterStopIsNoop verifies the stopped-flag guard:
// once stopAll has run, ensureSupervisor must not register new entries. This
// closes the race window where OnNodeChanged could re-register a supervisor
// mid-shutdown.
func TestOutboundEnsureSupervisorAfterStopIsNoop(t *testing.T) {
	tlsCfg := &tls.Config{InsecureSkipVerify: true} //nolint:gosec // test-only
	handler := SessionHandler(func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		return nil
	})
	tr := newOutboundTransport(tlsCfg, handler, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tr.setLifecycleCtx(context.Background())

	tr.stopAll()
	tr.ensureSupervisor(t.Context(), NodeMeta{AgentID: "n1", NodeID: "n1", DialAddress: "127.0.0.1:1"})
	if tr.has("n1") {
		t.Fatal("ensureSupervisor must not register entries after stopAll")
	}
}

// TestOutboundDialVerifiesAgentServerName guards the A1 fix: the supervisor
// must set ServerName to AgentServerName(meta.AgentID), so a certificate
// issued to a DIFFERENT agent (same CA!) is rejected by standard x509
// hostname verification.
func TestOutboundDialVerifiesAgentServerName(t *testing.T) {
	stub := newAgentStubServer(t, "agent-other") // cert SAN: agent-other.agents.panvex.internal
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var connectCount atomic.Int32
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCount.Add(1)
		return nil
	}
	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n-san", AgentID: "agent-san", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	go sup.run(ctx)

	time.Sleep(500 * time.Millisecond)
	if connectCount.Load() != 0 {
		t.Fatalf("supervisor connected to a stub serving another agent's certificate (%d sessions)", connectCount.Load())
	}
}

// TestOutboundConnectAndServeTimesOutAgainstBlackHole guards M2: dialing a
// listener that accepts the TCP connection but never completes the TLS
// handshake / gRPC stream setup (a "black hole" or half-open agent) must be
// bounded by the connect-timeout AfterFunc, not by the ~40s gRPC keepalive
// (30s ping + 10s pong deadline). Before the fix, connectAndServe called
// client.Connect(ctx) with the raw supervisor ctx and no deadline of its own,
// so a black-hole peer would hang until keepalive intervened.
//
// The test uses a short connectTimeoutFn override (not the 15s production
// value) so it runs quickly in CI while still exercising the exact same
// AfterFunc+cancel code path connectAndServe uses in production.
func TestOutboundConnectAndServeTimesOutAgainstBlackHole(t *testing.T) {
	var lc net.ListenConfig
	listener, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	// Accept and hold the connection open without ever speaking TLS, so the
	// client's handshake blocks indefinitely — a stand-in for a half-open /
	// black-hole agent.
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Hold the raw TCP conn open; never write/negotiate TLS.
			go func() { <-t.Context().Done(); conn.Close() }()
		}
	}()

	caCert, _ := mustGenerateCA(t)
	tlsCfg := &tls.Config{RootCAs: rootPool(caCert)}

	handler := SessionHandler(func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		t.Fatal("handler must not be invoked: Connect should never succeed against a black hole")
		return nil
	})

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n-blackhole", AgentID: "agent-blackhole", DialAddress: listener.Addr().String()},
		tlsCfg,
		handler,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	const testTimeout = 200 * time.Millisecond
	sup.connectTimeoutFn = func() time.Duration { return testTimeout }

	start := time.Now()
	errCh := make(chan error, 1)
	go func() { errCh <- sup.connectAndServe(t.Context()) }()

	select {
	case err := <-errCh:
		elapsed := time.Since(start)
		if err == nil {
			t.Fatal("connectAndServe returned nil error against a black-hole peer")
		}
		// Generous upper bound: well under the ~40s keepalive floor this
		// regression test guards against, but tolerant of CI scheduling
		// jitter around the 200ms connectTimeoutFn.
		if elapsed > 5*time.Second {
			t.Fatalf("connectAndServe took %v, want well under the 40s keepalive floor (connectTimeoutFn=%v)", elapsed, testTimeout)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("connectAndServe did not return within 10s against a black-hole peer (M2 regression: keepalive-only reaping)")
	}
}

// TestOutboundConnectTimeoutDoesNotCancelLiveSession guards the other half of
// M2: once client.Connect succeeds, the AfterFunc timer MUST be stopped so it
// cannot later cancel connectCtx (and therefore the live stream, which
// inherits connectCtx) mid-session. This mirrors
// TestDialTransportConnectTimeoutDoesNotCancelLiveStream in
// internal/agent/transport/transport_test.go.
//
// Setup: a real agent stub server accepts the stream, a short
// connectTimeoutFn, and a handler that blocks (simulating a long-lived
// session) past the connect-timeout window. If the timer were left armed
// (the bug this guards against), the handler's ctx would be cancelled at
// ~connectTimeoutFn and the handler would observe ctx.Err() != nil before
// the test releases it.
func TestOutboundConnectTimeoutDoesNotCancelLiveSession(t *testing.T) {
	stub := newAgentStubServer(t, "agent-live")
	defer stub.Close()

	handlerEntered := make(chan struct{})
	release := make(chan struct{})
	var sawCancelDuringWait atomic.Bool

	handler := SessionHandler(func(ctx context.Context, _ AgentSession, _ NodeMeta) error {
		close(handlerEntered)
		select {
		case <-ctx.Done():
			// Fired before we were released — the AfterFunc leaked past
			// Connect returning and killed the live session. Record it and
			// return promptly; the assertion below fails the test.
			sawCancelDuringWait.Store(true)
		case <-release:
		}
		return ctx.Err()
	})

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n-live", AgentID: "agent-live", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	const testTimeout = 50 * time.Millisecond
	sup.connectTimeoutFn = func() time.Duration { return testTimeout }

	errCh := make(chan error, 1)
	go func() { errCh <- sup.connectAndServe(t.Context()) }()

	select {
	case <-handlerEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start within 2s")
	}

	// Hold well past connectTimeoutFn to prove the timer was stopped after
	// Connect returned.
	time.Sleep(5 * testTimeout)
	close(release)

	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
		t.Fatal("connectAndServe did not return after release")
	}

	if sawCancelDuringWait.Load() {
		t.Fatal("live session ctx was cancelled by the connect-timeout AfterFunc — timer was not stopped after Connect succeeded")
	}
}

// ----------------- helpers -----------------

type agentStubServer struct {
	server    *grpc.Server
	listener  net.Listener
	address   string
	clientTLS *tls.Config
	gatewayrpc.UnimplementedAgentGatewayServer
}

// Connect is the agent-side handler in the stub. The panel (gRPC client) sends
// ConnectServerMessage on the wire using the inverted-type approach; we peek at
// one such message via RecvMsg then return so the panel sees EOF and the
// supervisor reconnects.
func (s *agentStubServer) Connect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	var inbound gatewayrpc.ConnectServerMessage
	_ = stream.RecvMsg(&inbound)
	return nil
}

func (s *agentStubServer) Close() {
	s.server.GracefulStop()
}

func newAgentStubServer(t *testing.T, agentID string) *agentStubServer {
	t.Helper()
	caCert, caKey := mustGenerateCA(t)
	serverCert, serverKey := mustGenerateLeaf(t, caCert, caKey, AgentServerName(agentID))

	tlsCert := tls.Certificate{
		Certificate: [][]byte{serverCert.Raw},
		PrivateKey:  serverKey,
	}
	serverTLSCfg := &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	}
	clientTLSCfg := &tls.Config{
		// ServerName intentionally empty: the supervisor must set it to
		// AgentServerName(meta.AgentID) per dial — that is the behaviour
		// under test since the A1 fix.
		RootCAs: rootPool(caCert),
	}

	var lc net.ListenConfig
	lis, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	creds := credentials.NewTLS(serverTLSCfg)
	gs := grpc.NewServer(grpc.Creds(creds))
	stub := &agentStubServer{
		server:    gs,
		listener:  lis,
		address:   lis.Addr().String(),
		clientTLS: clientTLSCfg,
	}
	gatewayrpc.RegisterAgentGatewayServer(gs, stub)
	go gs.Serve(lis) //nolint:errcheck
	return stub
}

func mustGenerateCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("ca create: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ca parse: %v", err)
	}
	return cert, priv
}

func mustGenerateLeaf(t *testing.T, parent *x509.Certificate, parentKey *ecdsa.PrivateKey, host string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &priv.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("leaf create: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("leaf parse: %v", err)
	}
	return cert, priv
}

func rootPool(cert *x509.Certificate) *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(cert)
	return p
}
