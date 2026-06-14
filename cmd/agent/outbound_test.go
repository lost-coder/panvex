package main

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestSendInitialMessagesAbortsOnCancelledContext: the initial-sync sends
// must be ctx-guarded — with the outbound pump gone (connection torn down
// between connect and initial sync) a bare channel send blocks forever.
func TestSendInitialMessagesAbortsOnCancelledContext(t *testing.T) {
	agent := runtime.New(runtime.Config{AgentID: "agent-1", NodeName: "n"}, failingTelemt{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Unbuffered channel with no reader: a bare send blocks forever.
	outbound := make(chan *gatewayrpc.ConnectClientMessage)

	done := make(chan error, 1)
	go func() { done <- sendInitialMessages(ctx, outbound, agent) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a ctx error, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sendInitialMessages blocked on a bare channel send with cancelled ctx")
	}
}
