package updates

import (
	"context"
	"encoding/json"
	"testing"
)

// memStore is an in-memory SettingsStore: four independent nil-able blobs.
type memStore struct {
	settings   json.RawMessage
	state      json.RawMessage
	selfUpdate json.RawMessage
	pending    json.RawMessage
}

func (m *memStore) GetUpdateSettings(context.Context) (json.RawMessage, error) {
	return m.settings, nil
}
func (m *memStore) PutUpdateSettings(_ context.Context, b json.RawMessage) error {
	m.settings = b
	return nil
}
func (m *memStore) GetUpdateState(context.Context) (json.RawMessage, error) { return m.state, nil }
func (m *memStore) PutUpdateState(_ context.Context, b json.RawMessage) error {
	m.state = b
	return nil
}
func (m *memStore) GetPanelSelfUpdate(context.Context) (json.RawMessage, error) {
	return m.selfUpdate, nil
}
func (m *memStore) PutPanelSelfUpdate(_ context.Context, b json.RawMessage) error {
	m.selfUpdate = b
	return nil
}
func (m *memStore) GetPendingAgentUpdates(context.Context) (json.RawMessage, error) {
	return m.pending, nil
}
func (m *memStore) PutPendingAgentUpdates(_ context.Context, b json.RawMessage) error {
	m.pending = b
	return nil
}

func TestPendingAgentUpdateRoundTrip(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	if _, ok, err := svc.PendingAgentUpdate(ctx, "agent-1"); err != nil || ok {
		t.Fatalf("empty store must have no pending update: ok=%v err=%v", ok, err)
	}
	if err := svc.SetPendingAgentUpdate(ctx, "agent-1", "1.4.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate: %v", err)
	}
	version, ok, err := svc.PendingAgentUpdate(ctx, "agent-1")
	if err != nil || !ok || version != "1.4.0" {
		t.Fatalf("want 1.4.0/true, got %q/%v/%v", version, ok, err)
	}
	if err := svc.ClearPendingAgentUpdate(ctx, "agent-1"); err != nil {
		t.Fatalf("ClearPendingAgentUpdate: %v", err)
	}
	if _, ok, _ := svc.PendingAgentUpdate(ctx, "agent-1"); ok {
		t.Fatal("cleared entry must be gone")
	}
}

// A newer operator click must overwrite the older target rather than append,
// and one agent's pending entry must never disturb another's.
func TestPendingAgentUpdateOverwritesPerAgent(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	ctx := context.Background()

	for _, version := range []string{"1.4.0", "1.5.0"} {
		if err := svc.SetPendingAgentUpdate(ctx, "agent-1", version); err != nil {
			t.Fatalf("SetPendingAgentUpdate(%s): %v", version, err)
		}
	}
	if err := svc.SetPendingAgentUpdate(ctx, "agent-2", "1.3.0"); err != nil {
		t.Fatalf("SetPendingAgentUpdate(agent-2): %v", err)
	}
	if got, _, _ := svc.PendingAgentUpdate(ctx, "agent-1"); got != "1.5.0" {
		t.Fatalf("newer click must win, got %q", got)
	}

	if err := svc.ClearPendingAgentUpdate(ctx, "agent-1"); err != nil {
		t.Fatalf("ClearPendingAgentUpdate: %v", err)
	}
	if got, ok, _ := svc.PendingAgentUpdate(ctx, "agent-2"); !ok || got != "1.3.0" {
		t.Fatalf("clearing agent-1 must not touch agent-2, got %q/%v", got, ok)
	}
}

// Clearing an agent that was never pending is a no-op, not an error: the
// reconcile path calls Clear whenever the reported version already matches.
func TestClearPendingAgentUpdateAbsentIsNoOp(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	svc := NewService(store)
	if err := svc.ClearPendingAgentUpdate(context.Background(), "agent-unknown"); err != nil {
		t.Fatalf("clearing an absent entry must not error: %v", err)
	}
	if store.pending != nil {
		t.Fatalf("a no-op clear must not write a blob, got %s", store.pending)
	}
}

func TestLoadSettingsEmptyStoreReturnsDefaults(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	got, err := svc.LoadSettings(context.Background())
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != DefaultSettings() {
		t.Fatalf("empty store should yield defaults, got %#v", got)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	want := Settings{
		CheckIntervalHours:  12,
		AutoUpdatePanel:     true,
		AutoUpdateAgents:    true,
		GitHubRepo:          "acme/panvex",
		GitHubToken:         "ghp_x",
		AgentDownloadSource: "mirror",
	}
	if err := svc.SaveSettings(context.Background(), want); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	got, err := svc.LoadSettings(context.Background())
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestStateRoundTripAndEmptyIsZero(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	zero, err := svc.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if zero != (State{}) {
		t.Fatalf("empty store should yield zero State, got %#v", zero)
	}
	want := State{
		LatestPanelVersion: "1.2.3",
		PanelDownloadURL:   "https://x/panel",
		LastCheckedAt:      1730000000,
		LastCheckError:     "rate limited",
	}
	if err := svc.SaveState(context.Background(), want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := svc.LoadState(context.Background())
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got != want {
		t.Fatalf("state round-trip mismatch:\n got %#v\nwant %#v", got, want)
	}
}
