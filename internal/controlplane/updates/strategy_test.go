package updates

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// fakeStrategyRepo is an in-memory StrategyRepo double that mirrors the
// storage contract closely enough for unit tests: Get returns
// storage.ErrNotFound for an absent agent, and Upsert never fabricates
// CreatedAt itself (the service is the one contractually responsible for
// preserving it), matching how a real store behaves.
type fakeStrategyRepo struct {
	records map[string]storage.AgentUpdateStrategyRecord
}

func newFakeStrategyRepo() *fakeStrategyRepo {
	return &fakeStrategyRepo{records: map[string]storage.AgentUpdateStrategyRecord{}}
}

func (f *fakeStrategyRepo) GetAgentUpdateStrategy(_ context.Context, agentID string) (storage.AgentUpdateStrategyRecord, error) {
	rec, ok := f.records[agentID]
	if !ok {
		return storage.AgentUpdateStrategyRecord{}, storage.ErrNotFound
	}
	return rec, nil
}

func (f *fakeStrategyRepo) UpsertAgentUpdateStrategy(_ context.Context, rec storage.AgentUpdateStrategyRecord) error {
	f.records[rec.AgentID] = rec
	return nil
}

func TestStrategyServiceGetMissingReturnsZeroFalseNil(t *testing.T) {
	repo := newFakeStrategyRepo()
	svc := NewStrategyService(repo, func() time.Time { return time.Unix(0, 0) })

	st, ok, err := svc.Get(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if ok {
		t.Fatalf("Get() ok = true, want false for an unconfigured agent")
	}
	if st != (Strategy{}) {
		t.Fatalf("Get() strategy = %+v, want zero value", st)
	}
}

func TestStrategyServicePutThenGetRoundtrips(t *testing.T) {
	repo := newFakeStrategyRepo()
	now := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	svc := NewStrategyService(repo, func() time.Time { return now })

	want := Strategy{
		Mode:        StrategyModeBinary,
		RestartSpec: "systemd:telemt",
		BinaryPath:  "/usr/local/bin/telemt",
		AssetFlavor: "v3",
	}
	if err := svc.Put(context.Background(), "agent-1", want); err != nil {
		t.Fatalf("Put() error = %v, want nil", err)
	}

	got, ok, err := svc.Get(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}
	if !ok {
		t.Fatalf("Get() ok = false, want true after Put")
	}
	if got != want {
		t.Fatalf("Get() strategy = %+v, want %+v", got, want)
	}
}

func TestStrategyServicePutPreservesCreatedAt(t *testing.T) {
	repo := newFakeStrategyRepo()
	t1 := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	current := t1
	svc := NewStrategyService(repo, func() time.Time { return current })

	if err := svc.Put(context.Background(), "agent-1", Strategy{Mode: StrategyModeNone}); err != nil {
		t.Fatalf("first Put() error = %v, want nil", err)
	}

	current = t2
	if err := svc.Put(context.Background(), "agent-1", Strategy{Mode: StrategyModeDocker}); err != nil {
		t.Fatalf("second Put() error = %v, want nil", err)
	}

	rec := repo.records["agent-1"]
	if !rec.CreatedAt.Equal(t1) {
		t.Errorf("CreatedAt = %v, want preserved %v", rec.CreatedAt, t1)
	}
	if !rec.UpdatedAt.Equal(t2) {
		t.Errorf("UpdatedAt = %v, want %v", rec.UpdatedAt, t2)
	}
	if rec.Mode != StrategyModeDocker {
		t.Errorf("Mode = %q, want %q (second Put must win)", rec.Mode, StrategyModeDocker)
	}
}

func TestStrategyServicePutValidation(t *testing.T) {
	tests := []struct {
		name string
		st   Strategy
	}{
		{
			name: "unknown mode",
			st:   Strategy{Mode: "bogus"},
		},
		{
			name: "empty mode",
			st:   Strategy{Mode: ""},
		},
		{
			name: "binary missing restart_spec",
			st:   Strategy{Mode: StrategyModeBinary, BinaryPath: "/usr/local/bin/telemt"},
		},
		{
			name: "binary invalid restart_spec",
			st:   Strategy{Mode: StrategyModeBinary, RestartSpec: "not-a-spec", BinaryPath: "/usr/local/bin/telemt"},
		},
		{
			name: "binary missing binary_path",
			st:   Strategy{Mode: StrategyModeBinary, RestartSpec: "systemd:telemt"},
		},
		{
			name: "binary relative binary_path",
			st:   Strategy{Mode: StrategyModeBinary, RestartSpec: "systemd:telemt", BinaryPath: "relative/telemt"},
		},
		{
			name: "invalid asset_flavor",
			st:   Strategy{Mode: StrategyModeNone, AssetFlavor: "v9"},
		},
	}

	repo := newFakeStrategyRepo()
	svc := NewStrategyService(repo, func() time.Time { return time.Unix(0, 0) })

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.Put(context.Background(), "agent-1", tt.st)
			if !errors.Is(err, ErrInvalidStrategy) {
				t.Fatalf("Put() error = %v, want wrapping ErrInvalidStrategy", err)
			}
			if _, ok := repo.records["agent-1"]; ok {
				t.Fatalf("Put() persisted a record despite validation failure")
			}
		})
	}
}

func TestStrategyServicePutAcceptsDockerAndNoneWithoutRestartFields(t *testing.T) {
	repo := newFakeStrategyRepo()
	svc := NewStrategyService(repo, func() time.Time { return time.Unix(0, 0) })

	for _, mode := range []string{StrategyModeDocker, StrategyModeNone} {
		t.Run(mode, func(t *testing.T) {
			if err := svc.Put(context.Background(), "agent-1", Strategy{Mode: mode}); err != nil {
				t.Fatalf("Put() error = %v, want nil for mode=%q with no restart fields", err, mode)
			}
		})
	}
}
