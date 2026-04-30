package agenttransport

import (
	"context"
	"log/slog"
	"testing"
)

func TestManagerStartIsIdempotent(t *testing.T) {
	m := NewManager(nil, nil, slog.Default())
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
}

func TestManagerStopAfterStart(t *testing.T) {
	m := NewManager(nil, nil, slog.Default())
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.Stop() // should not panic
	// After Stop, Start again should still work.
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start after Stop: %v", err)
	}
}
