package agenttransport

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/lost-coder/panvex/internal/dbsqlc"
)

func TestManagerStartIsIdempotent(t *testing.T) {
	m := NewManager(nil, nil, nil, slog.Default())
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
}

func TestManagerStartAfterStopReturnsError(t *testing.T) {
	m := NewManager(nil, nil, nil, slog.Default())
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

// fakeTransportQueries is a map-backed fake that satisfies transportQueries.
type fakeTransportQueries struct {
	rows     map[string]dbsqlc.GetAgentTransportRow
	listRows map[string][]dbsqlc.ListAgentsByTransportModeRow
}

func (f *fakeTransportQueries) GetAgentTransport(_ context.Context, id string) (dbsqlc.GetAgentTransportRow, error) {
	if r, ok := f.rows[id]; ok {
		return r, nil
	}
	return dbsqlc.GetAgentTransportRow{}, sql.ErrNoRows
}

func (f *fakeTransportQueries) ListAgentsByTransportMode(_ context.Context, mode string) ([]dbsqlc.ListAgentsByTransportModeRow, error) {
	return f.listRows[mode], nil
}

func TestManagerHandlesTransportModeChange(t *testing.T) {
	fake := &fakeTransportQueries{rows: map[string]dbsqlc.GetAgentTransportRow{
		"node-1": {ID: "node-1", TransportMode: "inbound"},
	}}
	m := NewManager(nil, nil, nil, slog.Default())
	// Wire the fake directly — NewManager accepts *dbsqlc.Queries (nil-safe);
	// here we set the interface field directly for testing.
	m.db = fake

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Initial state: node-1 is inbound — no supervisor expected.
	m.OnNodeChanged("node-1")
	if m.HasOutboundSupervisor("node-1") {
		t.Fatal("inbound node-1 should not have a supervisor")
	}

	// Flip to outbound.
	fake.rows["node-1"] = dbsqlc.GetAgentTransportRow{
		ID:            "node-1",
		TransportMode: "outbound",
		DialAddress:   sql.NullString{String: "vps:8443", Valid: true},
	}
	m.OnNodeChanged("node-1")
	if !m.HasOutboundSupervisor("node-1") {
		t.Fatal("expected outbound supervisor for node-1 after mode change")
	}

	// Flip back to inbound.
	fake.rows["node-1"] = dbsqlc.GetAgentTransportRow{ID: "node-1", TransportMode: "inbound"}
	m.OnNodeChanged("node-1")
	if m.HasOutboundSupervisor("node-1") {
		t.Fatal("supervisor should be removed when mode flips back to inbound")
	}
}

func TestManagerStartRestoresOutboundSupervisors(t *testing.T) {
	fake := &fakeTransportQueries{
		listRows: map[string][]dbsqlc.ListAgentsByTransportModeRow{
			TransportModeOutbound: {
				{ID: "n1", TransportMode: TransportModeOutbound, DialAddress: sql.NullString{String: "vps1:8443", Valid: true}},
				{ID: "n3", TransportMode: TransportModeOutbound, DialAddress: sql.NullString{String: "vps3:8443", Valid: true}},
			},
		},
	}
	// Discard logger keeps test output clean — supervisor goroutines will
	// loop with reconnection errors because the dial address is unreachable.
	// Stub tlsCfg satisfies the fail-fast guard in Start.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(nil, nil, &tls.Config{}, logger)
	m.db = fake
	t.Cleanup(m.Stop) // drain goroutines via outbound.stopAll()

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !m.HasOutboundSupervisor("n1") {
		t.Fatal("expected supervisor for n1")
	}
	if !m.HasOutboundSupervisor("n3") {
		t.Fatal("expected supervisor for n3")
	}
	if m.HasOutboundSupervisor("n2") {
		t.Fatal("did not expect supervisor for n2 (inbound)")
	}
}

func TestManagerStartSkipsOutboundWithoutDialAddress(t *testing.T) {
	fake := &fakeTransportQueries{
		listRows: map[string][]dbsqlc.ListAgentsByTransportModeRow{
			TransportModeOutbound: {
				{ID: "n-no-addr", TransportMode: TransportModeOutbound, DialAddress: sql.NullString{Valid: false}},
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := NewManager(nil, nil, &tls.Config{}, logger)
	m.db = fake
	t.Cleanup(m.Stop)

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if m.HasOutboundSupervisor("n-no-addr") {
		t.Fatal("expected skip when dial_address is null")
	}
}

func TestManagerStartFailsWhenOutboundRequiresTLS(t *testing.T) {
	fake := &fakeTransportQueries{
		listRows: map[string][]dbsqlc.ListAgentsByTransportModeRow{
			TransportModeOutbound: {
				{ID: "n1", TransportMode: TransportModeOutbound, DialAddress: sql.NullString{String: "vps:8443", Valid: true}},
			},
		},
	}
	m := NewManager(nil, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.db = fake
	t.Cleanup(m.Stop)

	err := m.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when tlsCfg is nil but outbound rows exist")
	}
}
