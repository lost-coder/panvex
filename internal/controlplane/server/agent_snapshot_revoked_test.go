package server

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/gateway"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// TestRevokedSnapshotDropWarnsOncePerAgent (R9b Task 7): a deleted agent still
// streaming is dropped and logged at Warn the first time (operator-visible),
// then rate-limited to Debug so a reconnect loop can't flood the log.
func TestRevokedSnapshotDropWarnsOncePerAgent(t *testing.T) {
	now := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	h := &recordingHandler{}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Logger:           slog.New(h),
	})
	defer server.Close()

	server.mu.Lock()
	server.revokedAgentIDs["agent-rev"] = struct{}{}
	server.mu.Unlock()

	mk := func() gateway.AgentSnapshot {
		return gateway.AgentSnapshot{
			AgentID:    "agent-rev",
			Snap:       &gatewayrpc.Snapshot{NodeName: "node-rev", Runtime: &gatewayrpc.RuntimeSnapshot{}},
			ObservedAt: now,
		}
	}
	for i := 0; i < 2; i++ {
		if err := server.applyAgentSnapshot(context.Background(), mk()); err != nil {
			t.Fatalf("applyAgentSnapshot() error = %v", err)
		}
	}

	if _, ok := server.live.Get("agent-rev"); ok {
		t.Fatal("revoked agent must not enter the live store")
	}

	var warns, debugs int
	h.mu.Lock()
	for _, r := range h.records {
		if r.Message != "dropping snapshot from revoked agent" {
			continue
		}
		switch r.Level {
		case slog.LevelWarn:
			warns++
		case slog.LevelDebug:
			debugs++
		}
	}
	h.mu.Unlock()
	if warns != 1 {
		t.Errorf("warn drops = %d, want 1", warns)
	}
	if debugs != 1 {
		t.Errorf("debug drops = %d, want 1", debugs)
	}
}
