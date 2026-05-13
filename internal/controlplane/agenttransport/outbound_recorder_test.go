package agenttransport

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/enrollment"
)

// TestOutboundConnectAndServeRecordsDialFailure exercises the unhappy path of
// connectAndServe against a closed port. It verifies that:
//   - An enrollment attempt row is created at the top of the cycle.
//   - The attempt is attached to the agent ID immediately (so the UI can
//     surface it under the right agent before the dial completes).
//   - The attempt ends in StatusFailed with a dial-related ErrorCode
//     (OUTBOUND_LISTENER_REFUSED on Linux; OUTBOUND_DIAL_TIMEOUT under
//     loaded test infra is also acceptable).
//   - A panel_dial_attempted event is recorded before the failure.
//
// The dial target is 127.0.0.1:1 — port 1 is reserved and refuses TCP, so
// the call returns synchronously with a "connection refused" error on
// typical Linux/macOS hosts. A minimal &tls.Config{} is provided so the
// outbound TLS-missing guard does not short-circuit ahead of the dial.
func TestOutboundConnectAndServeRecordsDialFailure(t *testing.T) {
	store := enrollment.NewMemStoreForTest()
	rec := enrollment.NewRecorder(store, time.Now)

	sup := &outboundSupervisor{
		meta: NodeMeta{
			NodeID:      "test-node",
			AgentID:     "00000000-0000-0000-0000-000000000001",
			DialAddress: "127.0.0.1:1",
		},
		// Stub TLS config so connectAndServe reaches grpc.NewClient; we
		// rely on the connection itself failing rather than the TLS-missing
		// guard.
		tlsCfg: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only, no real handshake
		handler: SessionHandler(func(_ context.Context, _ AgentSession, _ NodeMeta) error {
			t.Fatal("handler must not be invoked on dial failure")
			return nil
		}),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		rec:    rec,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Error is expected; we only assert on the recorder state.
	_ = sup.connectAndServe(ctx)

	attempts := store.SnapshotAttempts()
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	att := attempts[0]
	if att.Status != enrollment.StatusFailed {
		t.Fatalf("status = %q, want %q", att.Status, enrollment.StatusFailed)
	}
	if att.Mode != enrollment.ModeOutbound {
		t.Fatalf("mode = %q, want %q", att.Mode, enrollment.ModeOutbound)
	}
	if att.AgentID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("agent_id = %q, want it attached at Begin", att.AgentID)
	}
	switch att.ErrorCode {
	case enrollment.ErrOutboundListenerRefused,
		enrollment.ErrOutboundDialTimeout,
		enrollment.ErrPanelUnreachable:
		// Any of these dial-time codes is a passing classification.
	default:
		t.Fatalf("error_code = %q, want a dial-related code", att.ErrorCode)
	}

	events := store.SnapshotEvents(att.ID)
	if len(events) == 0 {
		t.Fatalf("expected at least one event, got 0")
	}
	if events[0].Step != enrollment.StepPanelDialAttempted {
		t.Fatalf("first event step = %q, want %q",
			events[0].Step, enrollment.StepPanelDialAttempted)
	}
}

// TestOutboundConnectAndServeWithoutRecorderIsNoop verifies that the
// recorder-gating cleanly degrades when rec == nil — connectAndServe must
// behave identically to its pre-instrumentation form. We can't observe the
// "no recording" property directly, but we can confirm the call still
// returns an error against a closed port without panicking.
func TestOutboundConnectAndServeWithoutRecorderIsNoop(t *testing.T) {
	sup := &outboundSupervisor{
		meta: NodeMeta{
			NodeID:      "test-node",
			AgentID:     "agent-without-recorder",
			DialAddress: "127.0.0.1:1",
		},
		tlsCfg: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test-only, no real handshake
		handler: SessionHandler(func(_ context.Context, _ AgentSession, _ NodeMeta) error {
			t.Fatal("handler must not be invoked on dial failure")
			return nil
		}),
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		// rec deliberately left nil — every recorder call must be gated.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sup.connectAndServe(ctx); err == nil {
		t.Fatal("connectAndServe against closed port: want error, got nil")
	}
}
