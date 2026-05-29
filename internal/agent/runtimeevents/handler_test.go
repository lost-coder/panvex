package runtimeevents_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/agent/runtimeevents"
)

func TestHandlerAppendsInfoAndAbove(t *testing.T) {
	var stderr bytes.Buffer
	inner := slog.NewTextHandler(&stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
	buf := runtimeevents.NewBuffer(10)
	h := runtimeevents.NewHandler(inner, buf)
	lg := slog.New(h)

	lg.Debug("debug-msg")
	lg.Info("info-msg")
	lg.Warn("warn-msg", "field1", "value1")
	lg.Error("error-msg")

	if buf.Len() != 3 {
		t.Fatalf("buf.Len = %d, want 3 (debug excluded)", buf.Len())
	}
	if !strings.Contains(stderr.String(), "debug-msg") {
		t.Fatalf("inner handler did not receive debug record: %q", stderr.String())
	}
}

func TestHandlerPreservesFields(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})
	buf := runtimeevents.NewBuffer(10)
	h := runtimeevents.NewHandler(inner, buf)
	lg := slog.New(h)

	lg.Warn("hello", "agent_id", "abc", "code", "TOKEN_EXPIRED")

	evs := buf.DrainAfter(0)
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Level != "warn" || evs[0].Message != "hello" {
		t.Fatalf("Event = %+v", evs[0])
	}
	if evs[0].Fields["agent_id"] != "abc" || evs[0].Fields["code"] != "TOKEN_EXPIRED" {
		t.Fatalf("Fields = %+v", evs[0].Fields)
	}
}

func TestHandlerEnabledDelegates(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})
	h := runtimeevents.NewHandler(inner, runtimeevents.NewBuffer(1))

	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatalf("Enabled(Debug) should be false when inner is Info")
	}
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatalf("Enabled(Info) should be true")
	}
}

func TestHandlerFiresUrgentCallbackOnWarnAndError(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})
	buf := runtimeevents.NewBuffer(10)
	h := runtimeevents.NewHandler(inner, buf)
	fired := 0
	h.SetUrgentCallback(func() { fired++ })
	lg := slog.New(h)

	lg.Info("ignored")
	lg.Warn("urgent1")
	lg.Error("urgent2")

	if fired != 2 {
		t.Fatalf("fired = %d, want 2 (Info ignored)", fired)
	}
}

func TestHandlerPreservesWithAttrsBoundFields(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})
	buf := runtimeevents.NewBuffer(10)
	h := runtimeevents.NewHandler(inner, buf)
	lg := slog.New(h).With("agent_id", "abc", "node_name", "alpha")

	lg.Info("hello", "step", "boot")

	evs := buf.DrainAfter(0)
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Fields["agent_id"] != "abc" {
		t.Fatalf("agent_id missing: %+v", ev.Fields)
	}
	if ev.Fields["node_name"] != "alpha" {
		t.Fatalf("node_name missing: %+v", ev.Fields)
	}
	if ev.Fields["step"] != "boot" {
		t.Fatalf("step missing: %+v", ev.Fields)
	}
}

func TestHandlerPreservesWithGroupPrefix(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})
	buf := runtimeevents.NewBuffer(10)
	h := runtimeevents.NewHandler(inner, buf)
	lg := slog.New(h).WithGroup("agent").With("id", "abc")

	lg.Info("hello", "step", "boot")

	evs := buf.DrainAfter(0)
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	ev := evs[0]
	// Bound attrs come under the group prefix; record-local attrs do too.
	if ev.Fields["agent.id"] != "abc" {
		t.Fatalf("agent.id missing under group: %+v", ev.Fields)
	}
	if ev.Fields["agent.step"] != "boot" {
		t.Fatalf("agent.step missing under group: %+v", ev.Fields)
	}
}
