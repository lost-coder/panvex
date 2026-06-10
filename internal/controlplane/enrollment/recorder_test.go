package enrollment_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/controlplane/enrollment/enrollmenttest"
)

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestRecorderBeginEventComplete(t *testing.T) {
	store := enrollmenttest.NewMemStore()
	rec := enrollment.NewRecorder(store, fixedClock(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)))

	ctx := enrollment.WithRequestID(context.Background(), "req-1")
	attemptID, err := rec.Begin(ctx, enrollment.ModeInbound, "tok-1", "10.0.0.5")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if attemptID == "" {
		t.Fatal("Begin returned empty attempt id")
	}

	rec.Event(ctx, attemptID, enrollment.StepTokenValidated, enrollment.LevelInfo, "ok", map[string]any{"token_id": "tok-1"})
	rec.Event(ctx, attemptID, enrollment.StepCertSigned, enrollment.LevelInfo, "issued", nil)

	if err := rec.AttachAgent(ctx, attemptID, "agent-1"); err != nil {
		t.Fatalf("AttachAgent: %v", err)
	}
	if err := rec.Complete(ctx, attemptID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	att := store.GetAttempt(attemptID)
	if att.Status != enrollment.StatusSuccess {
		t.Fatalf("status = %q, want success", att.Status)
	}
	if att.AgentID != "agent-1" {
		t.Fatalf("agent_id = %q", att.AgentID)
	}
	if att.RequestID != "req-1" {
		t.Fatalf("request_id = %q", att.RequestID)
	}
	events := store.SnapshotEvents(attemptID)
	if got := len(events); got != 2 {
		t.Fatalf("events count = %d, want 2", got)
	}
	if events[0].Step != enrollment.StepTokenValidated {
		t.Fatalf("first step = %q", events[0].Step)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(events[0].FieldsJSON), &fields); err != nil {
		t.Fatalf("fields_json invalid: %v", err)
	}
	if fields["token_id"] != "tok-1" {
		t.Fatalf("fields token_id = %v", fields["token_id"])
	}
}

func TestRecorderFailIsTerminal(t *testing.T) {
	store := enrollmenttest.NewMemStore()
	rec := enrollment.NewRecorder(store, fixedClock(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)))

	ctx := enrollment.WithRequestID(context.Background(), "req-2")
	attemptID, err := rec.Begin(ctx, enrollment.ModeInbound, "tok-2", "10.0.0.6")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	if err := rec.Fail(ctx, attemptID, enrollment.ErrTokenExpired, nil, nil); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	att := store.GetAttempt(attemptID)
	if att.Status != enrollment.StatusFailed {
		t.Fatalf("status = %q, want failed", att.Status)
	}
	if att.ErrorCode != enrollment.ErrTokenExpired {
		t.Fatalf("error_code = %q", att.ErrorCode)
	}
	if att.ErrorMsg == "" {
		t.Fatalf("error_message is empty")
	}

	if err := rec.Fail(ctx, attemptID, enrollment.ErrInternal, nil, nil); err != nil {
		t.Fatalf("second Fail: %v", err)
	}
	if store.GetAttempt(attemptID).ErrorCode != enrollment.ErrTokenExpired {
		t.Fatalf("error_code overwritten: %q", store.GetAttempt(attemptID).ErrorCode)
	}

	if err := rec.Complete(ctx, attemptID); err != nil {
		t.Fatalf("Complete after Fail: %v", err)
	}
	if store.GetAttempt(attemptID).Status != enrollment.StatusFailed {
		t.Fatalf("status changed: %q", store.GetAttempt(attemptID).Status)
	}
}

func TestRecorderIngestAgentEvents(t *testing.T) {
	store := enrollmenttest.NewMemStore()
	rec := enrollment.NewRecorder(store, fixedClock(time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)))

	ctx := context.Background()
	attemptID, err := rec.Begin(ctx, enrollment.ModeInbound, "tok-3", "10.0.0.7")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	earlier := time.Date(2026, 5, 13, 11, 59, 30, 0, time.UTC)
	events := []enrollment.AgentReportedEvent{
		{Step: enrollment.StepAgentPersistedCert, Level: enrollment.LevelInfo, Ts: earlier, Message: "saved"},
		{Step: enrollment.StepGatewayDialed, Level: enrollment.LevelInfo, Ts: earlier.Add(time.Second), Message: "dialed"},
	}
	if err := rec.Ingest(ctx, attemptID, events); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	stored := store.SnapshotEvents(attemptID)
	if len(stored) != 2 {
		t.Fatalf("event count = %d", len(stored))
	}
	if !stored[0].Ts.Equal(earlier) {
		t.Fatalf("ts not preserved: %v", stored[0].Ts)
	}
	if stored[0].Step != enrollment.StepAgentPersistedCert {
		t.Fatalf("step = %q", stored[0].Step)
	}
}

// TestListAttemptsFiltersByMode verifies that the new Mode filter on
// ListFilter is honoured by ListAttemptsPage and only returns attempts
// whose mode equals the filter value.
func TestListAttemptsFiltersByMode(t *testing.T) {
	store := enrollmenttest.NewMemStore()
	clock := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	tick := clock
	rec := enrollment.NewRecorder(store, func() time.Time {
		t := tick
		tick = tick.Add(time.Millisecond)
		return t
	})
	ctx := context.Background()

	id1, err := rec.Begin(ctx, enrollment.ModeInbound, "", "addr1")
	if err != nil {
		t.Fatalf("Begin1: %v", err)
	}
	if err := rec.Complete(ctx, id1); err != nil {
		t.Fatalf("Complete1: %v", err)
	}
	id2, err := rec.Begin(ctx, enrollment.ModeOutbound, "", "addr2")
	if err != nil {
		t.Fatalf("Begin2: %v", err)
	}
	if err := rec.Complete(ctx, id2); err != nil {
		t.Fatalf("Complete2: %v", err)
	}

	in := enrollment.ModeInbound
	page, err := rec.ListAttemptsPage(ctx, enrollment.ListFilter{Mode: &in, Limit: 10})
	if err != nil {
		t.Fatalf("ListAttemptsPage: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != id1 {
		t.Fatalf("got %+v, want only inbound id1", page.Items)
	}
	if page.NextCursor != nil {
		t.Fatalf("NextCursor = %+v, want nil", page.NextCursor)
	}
}

// TestListAttemptsCursorPagination drives ListAttemptsPage through two
// pages and asserts the cursor advances correctly: page 2 starts after
// page 1's last row, and the second page's NextCursor is nil when the
// store has no more rows.
func TestListAttemptsCursorPagination(t *testing.T) {
	store := enrollmenttest.NewMemStore()
	clock := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	tick := clock
	rec := enrollment.NewRecorder(store, func() time.Time {
		t := tick
		tick = tick.Add(2 * time.Millisecond)
		return t
	})
	ctx := context.Background()

	ids := []string{}
	for i := 0; i < 3; i++ {
		id, err := rec.Begin(ctx, enrollment.ModeInbound, "", fmt.Sprintf("addr%d", i))
		if err != nil {
			t.Fatalf("Begin %d: %v", i, err)
		}
		if err := rec.Complete(ctx, id); err != nil {
			t.Fatalf("Complete %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	_ = ids

	p1, err := rec.ListAttemptsPage(ctx, enrollment.ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("p1: %v", err)
	}
	if len(p1.Items) != 2 || p1.NextCursor == nil {
		t.Fatalf("p1 = %+v", p1)
	}

	p2, err := rec.ListAttemptsPage(ctx, enrollment.ListFilter{
		Limit:    2,
		CursorTs: &p1.NextCursor.Ts,
		CursorID: &p1.NextCursor.ID,
	})
	if err != nil {
		t.Fatalf("p2: %v", err)
	}
	if len(p2.Items) != 1 {
		t.Fatalf("p2 items = %d, want 1", len(p2.Items))
	}
	if p2.NextCursor != nil {
		t.Fatalf("p2 cursor should be nil (end of pages)")
	}
}
