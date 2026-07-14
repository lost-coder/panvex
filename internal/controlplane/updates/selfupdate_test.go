package updates

import (
	"context"
	"testing"
)

func TestLoadSelfUpdateEmptyStoreReturnsZero(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	zero, err := svc.LoadSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("LoadSelfUpdate: %v", err)
	}
	if zero != (SelfUpdateState{}) {
		t.Fatalf("empty store should yield zero SelfUpdateState, got %#v", zero)
	}
	if zero.Phase != SelfUpdateIdle {
		t.Fatalf("empty store should yield SelfUpdateIdle phase, got %q", zero.Phase)
	}
}

func TestSelfUpdateRoundTrip(t *testing.T) {
	t.Parallel()
	svc := NewService(&memStore{})
	want := SelfUpdateState{
		Phase:       SelfUpdateRestartPending,
		FromVersion: "1.2.3",
		ToVersion:   "1.3.0",
		Message:     "binary replaced, awaiting restart",
		UpdatedAt:   1730000000,
	}
	if err := svc.SaveSelfUpdate(context.Background(), want); err != nil {
		t.Fatalf("SaveSelfUpdate: %v", err)
	}
	got, err := svc.LoadSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("LoadSelfUpdate: %v", err)
	}
	if got != want {
		t.Fatalf("self-update round-trip mismatch:\n got %#v\nwant %#v", got, want)
	}
}

func TestSelfUpdatePhaseTerminal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		phase SelfUpdatePhase
		want  bool
	}{
		{SelfUpdateIdle, false},
		{SelfUpdateDownloading, false},
		{SelfUpdateInstalling, false},
		{SelfUpdateRestartPending, false},
		{SelfUpdateCompleted, true},
		{SelfUpdateFailed, true},
	}
	for _, tt := range tests {
		if got := tt.phase.Terminal(); got != tt.want {
			t.Errorf("SelfUpdatePhase(%q).Terminal() = %v, want %v", tt.phase, got, tt.want)
		}
	}
}
