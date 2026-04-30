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

// ClientStreamSession adapts a gRPC client-side stream (panel dialed the
// agent) to AgentSession. The proto's Connect RPC is unidirectionally
// typed (client→server: ConnectClientMessage; server→client:
// ConnectServerMessage), but the protocol semantics are agent-side regardless
// of who initiated the TCP connection. So this adapter uses the untyped
// SendMsg/RecvMsg pathway to transmit ConnectServerMessage from the panel
// (gRPC client) and receive ConnectClientMessage from the agent (gRPC
// server). Both sides cooperate on the same wire encoding — protobuf bytes
// are identical regardless of which generated wrapper "owns" the type.
type ClientStreamSession struct {
	Stream gatewayrpc.AgentGateway_ConnectClient
}

func (s *ClientStreamSession) Send(msg *gatewayrpc.ConnectServerMessage) error {
	return s.Stream.SendMsg(msg)
}

func (s *ClientStreamSession) Recv() (*gatewayrpc.ConnectClientMessage, error) {
	msg := &gatewayrpc.ConnectClientMessage{}
	if err := s.Stream.RecvMsg(msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func (s *ClientStreamSession) Context() context.Context {
	return s.Stream.Context()
}
