package agenttransport

import "testing"

// Compile-time check: ServerStreamSession satisfies AgentSession.
func TestServerStreamSessionImplementsInterface(t *testing.T) {
	var _ AgentSession = (*ServerStreamSession)(nil)
}
