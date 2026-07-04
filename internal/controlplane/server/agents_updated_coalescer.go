package server

import (
	"context"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
	cpevents "github.com/lost-coder/panvex/internal/controlplane/events"
)

// agentsUpdatedFlushInterval is the latest-wins coalescing window for
// agents.updated bus events (D6b). Snapshots arrive per agent every few
// seconds across the whole fleet, while the dashboard only needs the
// freshest value per agent: one publish per agent per tick bounds WS fan-out
// at fleet_size/300ms instead of snapshot_rate × subscribers. 300ms is
// invisible next to the dashboard's refetch cost but collapses bursts
// (initial fleet sync, panel restart reconnect) by an order of magnitude.
const agentsUpdatedFlushInterval = 300 * time.Millisecond

// agentsUpdatedCoalescer buffers the latest Agent value per agent ID between
// flush ticks. Offer never blocks and never publishes; the flusher goroutine
// owns all publishing.
type agentsUpdatedCoalescer struct {
	mu      sync.Mutex
	pending map[string]Agent
}

func newAgentsUpdatedCoalescer() *agentsUpdatedCoalescer {
	return &agentsUpdatedCoalescer{pending: make(map[string]Agent)}
}

// Offer records the latest snapshot-derived Agent value; an unflushed older
// value for the same agent is replaced (latest wins).
func (c *agentsUpdatedCoalescer) Offer(agent Agent) {
	c.mu.Lock()
	c.pending[agent.ID] = agent
	c.mu.Unlock()
}

// flush publishes every pending agent exactly once and clears the buffer.
func (c *agentsUpdatedCoalescer) flush(hub *eventbus.Hub) {
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		return
	}
	batch := c.pending
	c.pending = make(map[string]Agent, len(batch))
	c.mu.Unlock()

	for _, agent := range batch {
		hub.Publish(eventbus.Event{Type: cpevents.TypeAgentsUpdated, Data: agent})
	}
}

// startAgentsUpdatedFlusher runs the coalescer flush loop until ctx is
// cancelled, then performs a final flush so shutdown does not eat the last
// tick's updates. Registers with rollupWg like the other background workers.
func (s *Server) startAgentsUpdatedFlusher(ctx context.Context) {
	s.rollupWg.Add(1)
	go func() {
		defer s.rollupWg.Done()
		ticker := time.NewTicker(agentsUpdatedFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.agentsUpdated.flush(s.events)
				return
			case <-ticker.C:
				s.agentsUpdated.flush(s.events)
			}
		}
	}()
}
