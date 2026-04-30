package agenttransport

import "sync"

// outboundTransport is the supervisor pool for outbound (reverse-mode) agents.
// supervisors maps nodeID to a presence sentinel. Task 7 will replace the
// struct{} value with a real supervisor handle carrying a cancel context and
// goroutine lifecycle.
type outboundTransport struct {
	mu          sync.Mutex
	supervisors map[string]struct{} // nodeID → presence (real type comes in Task 7)
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
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.supervisors[nodeID]
	return ok
}
