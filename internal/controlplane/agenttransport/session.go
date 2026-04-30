package agenttransport

import (
	"context"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// AgentSession is a bidirectional message channel to a single agent,
// abstracted over the direction of the underlying TCP connection.
// Inbound sessions wrap a server-side stream (agent dialed in);
// outbound sessions wrap a client-side stream (panel dialed the agent).
type AgentSession interface {
	Send(*gatewayrpc.ConnectServerMessage) error
	Recv() (*gatewayrpc.ConnectClientMessage, error)
	Context() context.Context
}

// ServerStreamSession adapts a gRPC server-side stream (the agent dialed in)
// to AgentSession. The wrapper is intentionally trivial — it exists so the
// same handler can run against both server and client streams.
type ServerStreamSession struct {
	Stream gatewayrpc.AgentGateway_ConnectServer
}

func (s *ServerStreamSession) Send(msg *gatewayrpc.ConnectServerMessage) error {
	return s.Stream.Send(msg)
}

func (s *ServerStreamSession) Recv() (*gatewayrpc.ConnectClientMessage, error) {
	return s.Stream.Recv()
}

func (s *ServerStreamSession) Context() context.Context {
	return s.Stream.Context()
}
