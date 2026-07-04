package transport

import (
	"context"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// BidiStream is the agent's view of an established Connect stream.
// Send is agent → panel (ConnectClientMessage); Recv is panel → agent (ConnectServerMessage).
type BidiStream interface {
	Send(*gatewayrpc.ConnectClientMessage) error
	Recv() (*gatewayrpc.ConnectServerMessage, error)
}

// SessionRunner runs application-layer logic on an established stream and
// returns when the session ends. Implementations should respect ctx.Done().
//
// client is the AgentGatewayClient for the underlying connection and is
// provided so callers can make additional unary RPCs (e.g. RenewCertificate)
// over the same connection for the lifetime of the session.
type SessionRunner func(ctx context.Context, stream BidiStream, client gatewayrpc.AgentGatewayClient) error

// Transport opens or accepts one connection cycle and runs the SessionRunner
// on the resulting stream. RunOnce returns when the session ends; callers
// (the reconnect loop) decide whether to call again.
type Transport interface {
	RunOnce(ctx context.Context, run SessionRunner) error
}
