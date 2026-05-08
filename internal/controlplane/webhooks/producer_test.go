package webhooks

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"
)

func TestProducerPublishFansOutToMatchingEndpoints(t *testing.T) {
	store := newMemStore()
	store.addEndpoint(Endpoint{
		ID: "ep-agent", Name: "Slack-agents",
		URL: "https://hooks.example.com/slack",
		Secret: []byte("k1"), EventFilter: []string{"agent.*"}, Enabled: true,
	})
	store.addEndpoint(Endpoint{
		ID: "ep-audit", Name: "PD-security",
		URL: "https://events.pagerduty.com/x",
		Secret: []byte("k2"), EventFilter: []string{"audit.security.*"}, Enabled: true,
	})
	store.addEndpoint(Endpoint{
		ID: "ep-disabled", Name: "Old",
		URL: "https://nope.example.com",
		Secret: []byte("k3"), EventFilter: nil, Enabled: false,
	})

	frozen := time.Date(2026, 5, 8, 1, 2, 3, 0, time.UTC)
	ids := newSeqIDs()
	p := NewProducer(store)
	p.SetClock(func() time.Time { return frozen })
	p.SetIDFunc(ids.next)

	ev := Event{Action: "agent.unhealthy", Payload: json.RawMessage(`{"agent":"a-1"}`)}
	if err := p.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	rows := store.allRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 outbox row (only ep-agent matches), got %d", len(rows))
	}
	r := rows[0]
	if r.EndpointID != "ep-agent" {
		t.Errorf("EndpointID = %q, want ep-agent", r.EndpointID)
	}
	if r.EventAction != ev.Action {
		t.Errorf("EventAction = %q, want %q", r.EventAction, ev.Action)
	}
	if !r.NextAttemptAt.Equal(frozen) || !r.CreatedAt.Equal(frozen) {
		t.Errorf("clock not threaded: NextAttemptAt=%v CreatedAt=%v want %v", r.NextAttemptAt, r.CreatedAt, frozen)
	}
	if r.Attempt != 0 {
		t.Errorf("Attempt = %d, want 0 on fresh row", r.Attempt)
	}
	if r.Dead || r.DeliveredAt != nil {
		t.Errorf("fresh row should be dead=false, DeliveredAt=nil; got dead=%v delivered=%v", r.Dead, r.DeliveredAt)
	}
}

func TestProducerEmptyFilterMatchesEverything(t *testing.T) {
	store := newMemStore()
	store.addEndpoint(Endpoint{
		ID: "ep-broadcast", URL: "https://x.example.com", Secret: []byte("k"),
		EventFilter: nil, Enabled: true,
	})
	p := NewProducer(store)
	p.SetIDFunc(newSeqIDs().next)

	for _, action := range []string{"agent.unhealthy", "audit.security.something", "job.failed"} {
		if err := p.Publish(context.Background(), Event{Action: action}); err != nil {
			t.Fatalf("Publish(%s): %v", action, err)
		}
	}
	if got := len(store.allRows()); got != 3 {
		t.Errorf("broadcast endpoint should receive all 3 events, got %d outbox rows", got)
	}
}

func TestProducerRejectsEmptyAction(t *testing.T) {
	p := NewProducer(newMemStore())
	if err := p.Publish(context.Background(), Event{Action: ""}); err == nil {
		t.Error("expected error on empty Action")
	}
}

func TestProducerNoEndpointsSilent(t *testing.T) {
	// No endpoints configured: Publish must not error — event
	// sources call it unconditionally and lookup-zero is the
	// common path on a fresh deployment.
	p := NewProducer(newMemStore())
	if err := p.Publish(context.Background(), Event{Action: "agent.unhealthy"}); err != nil {
		t.Errorf("Publish with no endpoints: unexpected error %v", err)
	}
}

// newSeqIDs returns a deterministic ID generator: row-0, row-1, …
type seqIDs struct{ n int }

func newSeqIDs() *seqIDs { return &seqIDs{} }

func (s *seqIDs) next() string {
	id := "row-" + strconv.Itoa(s.n)
	s.n++
	return id
}
