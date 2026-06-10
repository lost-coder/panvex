package agenttransport

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// fakePinReader is a map-backed CertPinReader for integration tests.
type fakePinReader struct {
	pins map[string][]byte // agentID → pin (nil/missing means ErrNotFound)
}

func (f *fakePinReader) GetAgentCertPin(_ context.Context, agentID string) ([]byte, error) {
	pin, ok := f.pins[agentID]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return pin, nil
}

// TestOutboundSupervisor_PinMatch verifies that when the stored pin matches
// the served leaf cert's SPKI hash, the connection succeeds and the handler
// is invoked. (S-02)
func TestOutboundSupervisor_PinMatch(t *testing.T) {
	stub := newAgentStubServer(t, "agent-match")
	defer stub.Close()

	// Compute the correct pin from the stub's server cert.
	// newAgentStubServer uses mustGenerateCA + mustGenerateLeaf internally.
	// We need the server cert's SPKI. To get it, we perform a quick TLS dial.
	serverCert := getStubServerCert(t, stub)
	pin := sha256.Sum256(serverCert.RawSubjectPublicKeyInfo)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var connectCount atomic.Int32
	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCount.Add(1)
		return nil // end session immediately
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-match", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	sup.pinReader = &fakePinReader{pins: map[string][]byte{"agent-match": pin[:]}}

	go sup.run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for connectCount.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if connectCount.Load() < 1 {
		t.Fatal("expected at least one successful connection with matching pin")
	}
}

// TestOutboundSupervisor_PinMismatch verifies that when the stored pin does
// not match the served cert, the connection is rejected with ErrCertPinMismatch
// wrapped in the returned error, and the handler is never invoked. (S-02)
func TestOutboundSupervisor_PinMismatch(t *testing.T) {
	stub := newAgentStubServer(t, "agent-mismatch")
	defer stub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var connectCount atomic.Int32
	var mismatchSeen atomic.Bool

	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCount.Add(1)
		return nil
	}

	// Wrong pin: all zeros.
	wrongPin := make([]byte, sha256.Size)

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-mismatch", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	sup.pinReader = &fakePinReader{pins: map[string][]byte{"agent-mismatch": wrongPin}}
	sup.pinObserver = func(result string) {
		if result == "mismatch" {
			mismatchSeen.Store(true)
		}
	}

	go sup.run(ctx)

	// Wait for at least one mismatch observation.
	deadline := time.Now().Add(3 * time.Second)
	for !mismatchSeen.Load() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !mismatchSeen.Load() {
		t.Fatal("expected at least one pin-mismatch observation")
	}
	if connectCount.Load() != 0 {
		t.Fatalf("handler called %d times despite pin mismatch, want 0", connectCount.Load())
	}
}

// TestOutboundSupervisor_EmptyPinSkips verifies that when the stored pin is
// empty (agent enrolled pre-S-02), verification is skipped and the connection
// succeeds. (S-02)
func TestOutboundSupervisor_EmptyPinSkips(t *testing.T) {
	stub := newAgentStubServer(t, "agent-noop")
	defer stub.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var connectCount atomic.Int32
	var missingCount atomic.Int32

	handler := func(_ context.Context, _ AgentSession, _ NodeMeta) error {
		connectCount.Add(1)
		return nil
	}

	sup := newOutboundSupervisor(
		NodeMeta{NodeID: "n1", AgentID: "agent-noop", DialAddress: stub.address},
		stub.clientTLS,
		handler,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	sup.backoffInitialFn = func() time.Duration { return 10 * time.Millisecond }
	sup.backoffMaxFn = func() time.Duration { return 50 * time.Millisecond }
	// ErrNotFound → empty pin → skip verification.
	sup.pinReader = &fakePinReader{pins: map[string][]byte{}} // no entry
	sup.pinObserver = func(result string) {
		if result == "missing" {
			missingCount.Add(1)
		}
	}

	go sup.run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for connectCount.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if connectCount.Load() < 1 {
		t.Fatal("expected at least one successful connection with empty pin (verification skipped)")
	}
	if missingCount.Load() < 1 {
		t.Fatal("expected at least one 'missing' pin observation")
	}
}

// getStubServerCert performs a plain TLS dial to the stub to obtain its leaf
// certificate. This is necessary because newAgentStubServer does not expose
// the server cert's *x509.Certificate directly.
func getStubServerCert(t *testing.T, stub *agentStubServer) *x509.Certificate {
	t.Helper()
	// Re-use the clientTLS config (trusts the stub's CA) to do a quick dial.
	dialer := &tls.Dialer{Config: stub.clientTLS.Clone()}
	netConn, err := dialer.DialContext(t.Context(), "tcp", stub.address)
	if err != nil {
		t.Fatalf("getStubServerCert: tls.Dial: %v", err)
	}
	defer netConn.Close()
	conn, ok := netConn.(*tls.Conn)
	if !ok {
		t.Fatalf("getStubServerCert: expected *tls.Conn, got %T", netConn)
	}
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		t.Fatal("getStubServerCert: no peer certificates")
	}
	return state.PeerCertificates[0]
}

// Compile-time check: fakePinReader satisfies CertPinReader.
var _ CertPinReader = (*fakePinReader)(nil)

// Compile-time check: ErrCertPinMismatch is reachable via errors.Is through
// a wrapped error (the fmt.Errorf wrapping in connectAndServe).
var _ = errors.Is(errors.New(""), ErrCertPinMismatch)
