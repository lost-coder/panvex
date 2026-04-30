package agenttransport

import "testing"

// Compile-time check: ServerStreamSession satisfies AgentSession.
func TestServerStreamSessionImplementsInterface(t *testing.T) {
	var _ AgentSession = (*ServerStreamSession)(nil)
}

// Compile-time check: ClientStreamSession satisfies AgentSession.
func TestClientStreamSessionImplementsInterface(t *testing.T) {
	var _ AgentSession = (*ClientStreamSession)(nil)
}
