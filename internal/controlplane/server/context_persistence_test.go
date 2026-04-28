package server

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/security"
)

func TestEnrollAgentWithContextUsesCallerContextForPersistence(t *testing.T) {
	now := time.Date(2026, time.March, 29, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		LoginTimingFloor: -1,
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()

	token, err := server.issueEnrollmentToken(security.EnrollmentScope{
		FleetGroupID: "ams",
		TTL:          time.Minute,
	}, now)
	if err != nil {
		t.Fatalf("issueEnrollmentToken() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = server.enrollAgentWithContext(ctx, agentEnrollmentRequest{
		Token:    token.Value,
		NodeName: "node-a",
		Version:  "1.0.0",
	}, now)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("enrollAgentWithContext() error = %v, want %v", err, context.Canceled)
	}
}

// TestApplyAgentSnapshotWithContextSucceedsRegardlessOfCallerContext verifies
// that snapshot application always succeeds because persistence is handled
// asynchronously by the batch writer with its own context.
func TestApplyAgentSnapshotWithContextSucceedsRegardlessOfCallerContext(t *testing.T) {
	now := time.Date(2026, time.March, 29, 10, 15, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	fleetGroupID := seedTestFleetGroup(t, store, "ams", now)

	server := New(Options{
		LoginTimingFloor: -1,
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()
	server.agents["agent-1"] = Agent{
		ID:           "agent-1",
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		LastSeenAt:   now,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Cancelled context should not prevent in-memory state update.
	err = server.applyAgentSnapshotWithContext(ctx, agentSnapshot{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "ams",
		Version:      "1.0.1",
		ObservedAt:   now,
	})
	if err != nil {
		t.Fatalf("applyAgentSnapshotWithContext() error = %v, want nil (async persistence)", err)
	}

	server.mu.RLock()
	agent := server.agents["agent-1"]
	server.mu.RUnlock()
	if agent.Version != "1.0.1" {
		t.Fatalf("agent.Version = %q, want %q", agent.Version, "1.0.1")
	}
}
