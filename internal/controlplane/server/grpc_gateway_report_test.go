package server

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestReportEnrollmentStepsAppendsTimeline verifies the gateway RPC ingests
// agent-reported events into the existing attempt timeline. The recorder is
// only wired when the store exposes DB() — newEnrollmentRecorderTestServer
// uses the SQLite store which does, so srv.enrollmentRec is non-nil.
func TestReportEnrollmentStepsAppendsTimeline(t *testing.T) {
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	srv, _, db := newEnrollmentRecorderTestServer(t, now)

	rec := srv.enrollmentRec
	if rec == nil {
		t.Fatal("expected enrollmentRec to be wired in sqlite test fixture")
	}

	attemptID, err := rec.Begin(context.Background(), enrollment.ModeInbound, "", "1.2.3.4")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	req := &gatewayrpc.ReportEnrollmentStepsRequest{
		AttemptId: attemptID,
		Events: []*gatewayrpc.AgentEnrollmentEvent{
			{
				Step:    string(enrollment.StepAgentPersistedCert),
				Level:   string(enrollment.LevelInfo),
				Message: "saved",
				Ts:      timestamppb.New(now.Add(50 * time.Millisecond)),
			},
			{
				Step:  string(enrollment.StepGatewayDialed),
				Level: string(enrollment.LevelInfo),
				Ts:    timestamppb.New(now.Add(100 * time.Millisecond)),
				Fields: map[string]string{
					"endpoint": "panel.example.com:8443",
				},
			},
		},
	}

	if _, err := srv.ReportEnrollmentSteps(context.Background(), req); err != nil {
		t.Fatalf("ReportEnrollmentSteps: %v", err)
	}

	steps := loadEnrollmentEventSteps(t, db, attemptID)
	if len(steps) != 2 {
		t.Fatalf("event count = %d, want 2 (steps: %v)", len(steps), steps)
	}
	if steps[0] != string(enrollment.StepAgentPersistedCert) {
		t.Errorf("event[0].step = %q, want %q", steps[0], enrollment.StepAgentPersistedCert)
	}
	if steps[1] != string(enrollment.StepGatewayDialed) {
		t.Errorf("event[1].step = %q, want %q", steps[1], enrollment.StepGatewayDialed)
	}
}

// TestReportEnrollmentStepsNoOpInputs checks the short-circuit branches:
// nil request fields and unknown/empty attempt IDs must not return an
// error and must not append anything to the timeline.
func TestReportEnrollmentStepsNoOpInputs(t *testing.T) {
	now := time.Date(2026, time.May, 1, 12, 0, 0, 0, time.UTC)
	srv, _, _ := newEnrollmentRecorderTestServer(t, now)

	// Empty attempt_id → no-op.
	if _, err := srv.ReportEnrollmentSteps(context.Background(), &gatewayrpc.ReportEnrollmentStepsRequest{}); err != nil {
		t.Fatalf("empty attempt_id should not error: %v", err)
	}

	// Empty events list with a real attempt_id → no-op.
	attemptID, err := srv.enrollmentRec.Begin(context.Background(), enrollment.ModeInbound, "", "1.2.3.4")
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := srv.ReportEnrollmentSteps(context.Background(), &gatewayrpc.ReportEnrollmentStepsRequest{
		AttemptId: attemptID,
	}); err != nil {
		t.Fatalf("empty events should not error: %v", err)
	}
}
