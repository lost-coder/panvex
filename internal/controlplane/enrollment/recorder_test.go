package enrollment

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRecorderBeginEventComplete(t *testing.T) {
	store := NewMemStoreForTest()
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

func TestRecorderFailIsTerminal(t *testing.T) {
	store := NewMemStoreForTest()
	rec := NewRecorder(store, fixedClock(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)))

	ctx := WithRequestID(context.Background(), "req-2")
	attemptID, err := rec.Begin(ctx, ModeInbound, "tok-2", "10.0.0.6")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	if err := rec.Fail(ctx, attemptID, ErrTokenExpired, nil, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	att := store.attempts[attemptID]
	if att.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", att.Status)
	}
	if att.ErrorCode != ErrTokenExpired {
		t.Fatalf("error_code = %q", att.ErrorCode)
	}
	if att.ErrorMsg == "" {
		t.Fatalf("error_message is empty")
	}

	if err := rec.Fail(ctx, attemptID, ErrInternal, nil, nil); err != nil {
		t.Fatalf("second Fail: %v", err)
	}
	if store.attempts[attemptID].ErrorCode != ErrTokenExpired {
		t.Fatalf("error_code overwritten: %q", store.attempts[attemptID].ErrorCode)
	}

	if err := rec.Complete(ctx, attemptID); err != nil {
		t.Fatalf("Complete after Fail: %v", err)
	}
	if store.attempts[attemptID].Status != StatusFailed {
		t.Fatalf("status changed: %q", store.attempts[attemptID].Status)
	}
}

func TestRecorderIngestAgentEvents(t *testing.T) {
	store := NewMemStoreForTest()
	rec := NewRecorder(store, fixedClock(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)))

	ctx := context.Background()
	attemptID, err := rec.Begin(ctx, ModeInbound, "tok-3", "10.0.0.7")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	earlier := time.Date(2026, 5, 13, 11, 59, 30, 0, time.UTC)
	events := []AgentReportedEvent{
		{Step: StepAgentPersistedCert, Level: LevelInfo, Ts: earlier, Message: "saved"},
		{Step: StepGatewayDialed, Level: LevelInfo, Ts: earlier.Add(time.Second), Message: "dialed"},
	}
	if err := rec.Ingest(ctx, attemptID, events); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	stored := store.events[attemptID]
	if len(stored) != 2 {
		t.Fatalf("event count = %d", len(stored))
	}
	if !stored[0].Ts.Equal(earlier) {
		t.Fatalf("ts not preserved: %v", stored[0].Ts)
	}
	if stored[0].Step != StepAgentPersistedCert {
		t.Fatalf("step = %q", stored[0].Step)
	}
}
