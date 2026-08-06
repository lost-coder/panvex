package server

import (
	"context"
	"testing"
	"time"
)

func TestApplyGroupToAgentEnqueuesWhenGroupHasConfig(t *testing.T) {
	s := testServerWithSQLite(t, time.Now())
	// seed group "g1" config = {general:{fast_mode:false}}
	if err := s.configTargets.Upsert(context.Background(), "group", "g1",
		map[string]any{"general": map[string]any{"fast_mode": false}}); err != nil {
		t.Fatalf("seed group config: %v", err)
	}
	batchID, err := s.applyGroupToAgent(context.Background(), "user-1", "agent-1", "g1")
	if err != nil || batchID == "" {
		t.Fatalf("expected a batch, got id=%q err=%v", batchID, err)
	}
}

func TestApplyGroupToAgentNoopWhenGroupEmpty(t *testing.T) {
	s := testServerWithSQLite(t, time.Now())
	batchID, err := s.applyGroupToAgent(context.Background(), "user-1", "agent-1", "g-empty")
	if err != nil || batchID != "" {
		t.Fatalf("expected no-op, got id=%q err=%v", batchID, err)
	}
}
