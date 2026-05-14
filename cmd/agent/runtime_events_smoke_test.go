package main

import (
	"log/slog"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtimeevents"
	"github.com/lost-coder/panvex/internal/logutil"
)

// TestRuntimeHandlerBuffersInfoPlus is a smoke test that the agent's slog
// stack (logutil.NewHandler wrapped by runtimeevents.NewHandler) captures
// Info+ records into the ring buffer while excluding Debug. This mirrors
// the wiring done in runRuntime so a regression there is caught at the
// cmd/agent test level too.
func TestRuntimeHandlerBuffersInfoPlus(t *testing.T) {
	inner := logutil.NewHandler(logutil.Options{
		Format: logutil.FormatText,
		Level:  slog.LevelDebug,
	})
	buf := runtimeevents.NewBuffer(10)
	lg := slog.New(runtimeevents.NewHandler(inner, buf))

	lg.Debug("d")
	lg.Info("i")
	lg.Warn("w")

	evs := buf.DrainSince(time.Time{})
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2 (Debug excluded)", len(evs))
	}
}
