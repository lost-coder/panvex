package agenttransport

import (
	"context"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// AgentIDExtractor extracts the agent's identity from a stream context (e.g.,
// from the mTLS peer certificate). Supplied by the wirer to keep transport
// independent of the auth implementation.
type AgentIDExtractor func(ctx context.Context) (string, error)

// NodeResolver returns transport-layer metadata for the given agent ID.
type NodeResolver func(ctx context.Context, agentID string) (NodeMeta, error)

// inboundTransport adapts a gRPC server-side stream (agent dialed in) to the
// transport-agnostic SessionHandler. Not yet wired into the live gRPC server
// — the existing Connect handler in package server keeps its own path while
// this scaffold is built out.
type inboundTransport struct {
	extractAgentID AgentIDExtractor
	resolveNode    NodeResolver
	handler        SessionHandler
}

// HandleConnect is the future entry point for AgentGateway.Connect. Currently
// unused — kept here so Task 6/later tasks can wire the inbound path through
// Manager without restructuring this package.
func (t *inboundTransport) HandleConnect(stream gatewayrpc.AgentGateway_ConnectServer) error {
	ctx := stream.Context()
	agentID, err := t.extractAgentID(ctx)
	if err != nil {
		return err
	}
	meta, err := t.resolveNode(ctx, agentID)
	if err != nil {
		return err
	}
	sess := &ServerStreamSession{Stream: stream}
	return t.handler(ctx, sess, meta)
}
