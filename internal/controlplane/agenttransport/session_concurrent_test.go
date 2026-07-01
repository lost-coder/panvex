package agenttransport

import (
	"sync"
	"testing"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// recordingStream is a minimal AgentGateway_ConnectServer stub whose Send
// asserts it is never entered concurrently (the grpc-go contract we must
// honor). It deliberately does NOT lock around inFlight/sent: the whole
// point is to let `go test -race` observe the unsynchronized access from
// the caller's concurrent goroutines. If the caller (ServerStreamSession)
// fails to serialize its calls into Send, the race detector flags the
// concurrent read-modify-write on inFlight/sent, and the inFlight guard
// below additionally panics on the logical overlap.
type recordingStream struct {
	gatewayrpc.AgentGateway_ConnectServer
	inFlight int
	sent     int
}

func (r *recordingStream) Send(*gatewayrpc.ConnectServerMessage) error {
	r.inFlight++
	if r.inFlight != 1 {
		panic("concurrent Send on stream")
	}
	r.sent++
	r.inFlight--
	return nil
}

func TestServerStreamSessionSendIsSerialized(t *testing.T) {
	rs := &recordingStream{}
	sess := &ServerStreamSession{Stream: rs}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = sess.Send(&gatewayrpc.ConnectServerMessage{}) }()
	}
	wg.Wait()
	if rs.sent != 100 {
		t.Fatalf("want 100 sends, got %d", rs.sent)
	}
}

// recordingClientStream is a minimal AgentGateway_ConnectClient stub whose
// SendMsg asserts it is never entered concurrently. ClientStreamSession.Send
// calls Stream.SendMsg (see session.go), so the same contract applies here.
// Deliberately unsynchronized for the same reason as recordingStream above.
type recordingClientStream struct {
	gatewayrpc.AgentGateway_ConnectClient
	inFlight int
	sent     int
}

func (r *recordingClientStream) SendMsg(any) error {
	r.inFlight++
	if r.inFlight != 1 {
		panic("concurrent SendMsg on stream")
	}
	r.sent++
	r.inFlight--
	return nil
}

func TestClientStreamSessionSendIsSerialized(t *testing.T) {
	rs := &recordingClientStream{}
	sess := &ClientStreamSession{Stream: rs}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = sess.Send(&gatewayrpc.ConnectServerMessage{}) }()
	}
	wg.Wait()
	if rs.sent != 100 {
		t.Fatalf("want 100 sends, got %d", rs.sent)
	}
}
