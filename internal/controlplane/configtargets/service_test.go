package configtargets

import (
	"context"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type fakeRepo struct {
	rec    storage.AgentConfigTargetRecord
	getErr error
	putRec *storage.AgentConfigTargetRecord
}

func (f *fakeRepo) GetAgentConfigTarget(_ context.Context, _, _ string) (storage.AgentConfigTargetRecord, error) {
	return f.rec, f.getErr
}

func (f *fakeRepo) UpsertAgentConfigTarget(_ context.Context, rec storage.AgentConfigTargetRecord) error {
	f.putRec = &rec
	return nil
}

func TestSectionsMissingTargetIsEmptyMap(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeRepo{getErr: storage.ErrNotFound}, time.Now)
	got, err := svc.Sections(context.Background(), storage.ConfigScopeAgent, "a1")
	if err != nil {
		t.Fatalf("Sections: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("want empty non-nil map, got %#v", got)
	}
}

func TestSectionsUnmarshalsStoredJSON(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeRepo{rec: storage.AgentConfigTargetRecord{SectionsJSON: `{"telemt":{"x":1}}`}}, time.Now)
	got, err := svc.Sections(context.Background(), storage.ConfigScopeGroup, "g1")
	if err != nil {
		t.Fatalf("Sections: %v", err)
	}
	inner, ok := got["telemt"].(map[string]any)
	if !ok || inner["x"] != float64(1) {
		t.Fatalf("unmarshalled sections wrong: %#v", got)
	}
}

func TestUpsertPreservesCreatedAt(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	repo := &fakeRepo{rec: storage.AgentConfigTargetRecord{CreatedAt: created}}
	svc := NewService(repo, func() time.Time { return now })
	if err := svc.Upsert(context.Background(), storage.ConfigScopeGroup, "g1", map[string]any{"telemt": map[string]any{"x": 1}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if repo.putRec == nil {
		t.Fatal("nothing persisted")
	}
	if !repo.putRec.CreatedAt.Equal(created) || !repo.putRec.UpdatedAt.Equal(now) {
		t.Fatalf("CreatedAt/UpdatedAt: got %v/%v", repo.putRec.CreatedAt, repo.putRec.UpdatedAt)
	}
	if repo.putRec.ScopeType != storage.ConfigScopeGroup || repo.putRec.ScopeID != "g1" || repo.putRec.SectionsJSON == "" {
		t.Fatalf("record fields: %#v", *repo.putRec)
	}
}

func TestUpsertMissingTargetStampsCreatedAtNow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC)
	repo := &fakeRepo{getErr: storage.ErrNotFound}
	svc := NewService(repo, func() time.Time { return now })
	if err := svc.Upsert(context.Background(), storage.ConfigScopeAgent, "a1", map[string]any{}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if repo.putRec == nil {
		t.Fatal("nothing persisted")
	}
	if !repo.putRec.CreatedAt.Equal(now) || !repo.putRec.UpdatedAt.Equal(now) {
		t.Fatalf("fresh record should stamp CreatedAt=UpdatedAt=now, got %v/%v", repo.putRec.CreatedAt, repo.putRec.UpdatedAt)
	}
}
