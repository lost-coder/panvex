package agenttransport

import (
	"context"
	"errors"
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

func TestManagerStartAfterStopReturnsError(t *testing.T) {
	m := NewManager(nil, nil, slog.Default())
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.Stop()
	// Manager is one-way: Start after Stop must fail so callers don't
	// silently resurrect a torn-down transport.
	if err := m.Start(context.Background()); !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("Start after Stop: got %v, want ErrManagerStopped", err)
	}
}
