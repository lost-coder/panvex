package agenttransport

import "sync"

// outboundTransport is the supervisor pool for outbound (reverse-mode) agents.
// supervisors maps nodeID to a supervisor handle; the value type is a stub
// until the outbound dial loop is implemented.
type outboundTransport struct {
	mu          sync.RWMutex
	supervisors map[string]struct{}
}

func newOutboundTransport() *outboundTransport {
	return &outboundTransport{supervisors: map[string]struct{}{}}
}

func (t *outboundTransport) ensureSupervisor(meta NodeMeta) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.supervisors[meta.NodeID] = struct{}{}
}

func (t *outboundTransport) removeSupervisor(nodeID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.supervisors, nodeID)
}

func (t *outboundTransport) has(nodeID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, ok := t.supervisors[nodeID]
	return ok
}
