package updates

import (
	"context"
	"encoding/json"
	"testing"
)

// memStore is an in-memory SettingsStore: two independent nil-able blobs.
type memStore struct {
	settings json.RawMessage
	state    json.RawMessage
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
