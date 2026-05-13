package enrollment

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRecorderBeginEventComplete(t *testing.T) {
	store := newMemStore()
	rec := NewRecorder(store, fixedClock(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)))

	ctx := WithRequestID(context.Background(), "req-1")
	attemptID, err := rec.Begin(ctx, ModeInbound, "tok-1", "10.0.0.5")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if attemptID == "" {
		t.Fatal("Begin returned empty attempt id")
	}

	rec.Event(ctx, attemptID, StepTokenValidated, LevelInfo, "ok", map[string]any{"token_id": "tok-1"})
	rec.Event(ctx, attemptID, StepCertSigned, LevelInfo, "issued", nil)

	if err := rec.AttachAgent(ctx, attemptID, "agent-1"); err != nil {
		t.Fatalf("AttachAgent: %v", err)
	}
	if err := rec.Complete(ctx, attemptID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	att := store.attempts[attemptID]
	if att.Status != StatusSuccess {
		t.Fatalf("status = %q, want success", att.Status)
	}
	if att.AgentID != "agent-1" {
		t.Fatalf("agent_id = %q", att.AgentID)
	}
	if att.RequestID != "req-1" {
		t.Fatalf("request_id = %q", att.RequestID)
	}
	if got := len(store.events[attemptID]); got != 2 {
		t.Fatalf("events count = %d, want 2", got)
	}
	if store.events[attemptID][0].Step != StepTokenValidated {
		t.Fatalf("first step = %q", store.events[attemptID][0].Step)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(store.events[attemptID][0].FieldsJSON), &fields); err != nil {
		t.Fatalf("fields_json invalid: %v", err)
	}
	if fields["token_id"] != "tok-1" {
		t.Fatalf("fields token_id = %v", fields["token_id"])
	}
}
