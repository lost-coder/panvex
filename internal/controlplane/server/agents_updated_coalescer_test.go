package server

import (
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/eventbus"
)

// TestAgentsUpdatedCoalescerLatestWins guards D6b: multiple Offers for one
// agent inside a flush window must collapse into a single publish carrying
// the LATEST value; distinct agents publish independently; an empty buffer
// publishes nothing.
func TestAgentsUpdatedCoalescerLatestWins(t *testing.T) {
	hub := eventbus.NewHub()
	ch, cancel := hub.Subscribe()
	defer cancel()

	c := newAgentsUpdatedCoalescer()
	c.Offer(Agent{ID: "a-1", Version: "1"})
	c.Offer(Agent{ID: "a-1", Version: "2"})
	c.Offer(Agent{ID: "a-2", Version: "9"})
	c.flush(hub)

	got := map[string]string{}
	for i := 0; i < 2; i++ {
		select {
		case evt := <-ch:
			if evt.Type != "agents.updated" {
				t.Fatalf("event type = %q, want agents.updated", evt.Type)
			}
			agent, ok := evt.Data.(Agent)
			if !ok {
				t.Fatalf("event data type = %T, want Agent", evt.Data)
			}
			got[agent.ID] = agent.Version
		default:
			t.Fatalf("expected 2 published events, got %d", i)
		}
	}
	if got["a-1"] != "2" {
		t.Fatalf("latest-wins violated: a-1 version = %q, want 2", got["a-1"])
	}
	if got["a-2"] != "9" {
		t.Fatalf("a-2 version = %q, want 9", got["a-2"])
	}
	select {
	case evt := <-ch:
		t.Fatalf("unexpected extra event %q", evt.Type)
	default:
	}

	c.flush(hub) // empty buffer
	select {
	case <-ch:
		t.Fatal("flushing an empty buffer must publish nothing")
	default:
	}
}
