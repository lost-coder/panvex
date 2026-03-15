package server

import (
	"testing"
	"time"
)

func TestEventHubBroadcastsPublishedEventToSubscribers(t *testing.T) {
	hub := newEventHub()
	subscription, cancel := hub.subscribe()
	defer cancel()

	event := eventEnvelope{
		Type: "jobs.created",
		Data: map[string]any{
			"id": "job-1",
		},
	}
	hub.publish(event)

	select {
	case received := <-subscription:
		if received.Type != event.Type {
			t.Fatalf("received.Type = %q, want %q", received.Type, event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("publish() did not reach subscriber")
	}
}
