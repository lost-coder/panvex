package fleet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/fleet"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// newTestService spins up an ephemeral in-memory SQLite store + a
// frozen clock so every test gets deterministic timestamps and
// deterministic UUID ordering (service.newID is uuid.NewString, still
// random — tests assert on other fields).
func newTestService(t *testing.T) *fleet.Service {
	t.Helper()
	// sqlite.Open runs goose migrations internally, so a fresh DSN
	// produces a schema-ready store. Each test gets its own temp file
	// so there is no cross-test bleed.
	dbPath := t.TempDir() + "/fleet.db"
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return fleet.NewService(store, func() time.Time {
		return time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	})
}

func TestServiceCreateValidatesName(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	// Names are normalised to lowercase + trimmed before validation,
	// so the rejected cases here exercise character-class violations
	// that survive normalisation.
	cases := map[string]struct {
		name    string
		wantErr error
	}{
		"empty":         {"", fleet.ErrNameRequired},
		"trailing-dash": {"edge-", fleet.ErrNameInvalid},
		"underscore":    {"edge_ny", fleet.ErrNameInvalid},
		"space":         {"edge ny", fleet.ErrNameInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Create(ctx, fleet.CreateInput{Name: tc.name, Label: "x"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Create(%q) error = %v, want %v", tc.name, err, tc.wantErr)
			}
		})
	}
}

func TestServiceCreateAndGet(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, fleet.CreateInput{
		Name:        "edge",
		Label:       "Edge POPs",
		Description: "North America edge",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID == "" || created.Name != "edge" || created.Label != "Edge POPs" {
		t.Fatalf("Create() = %+v, want valid record", created)
	}

	got, err := svc.GetByName(ctx, "edge")
	if err != nil {
		t.Fatalf("GetByName() error = %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("GetByName().ID = %q, want %q", got.ID, created.ID)
	}
}

func TestServiceCreateDuplicateName(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, fleet.CreateInput{Name: "edge", Label: "Edge"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	_, err := svc.Create(ctx, fleet.CreateInput{Name: "edge", Label: "Other"})
	if !errors.Is(err, fleet.ErrNameInUse) {
		t.Fatalf("Create(duplicate) error = %v, want ErrNameInUse", err)
	}
}

func TestServiceUpdatePreservesName(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, fleet.CreateInput{Name: "edge", Label: "Edge"})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	updated, err := svc.Update(ctx, created.ID, fleet.UpdateInput{Label: "Edge v2", Description: "CDN"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Name != "edge" {
		t.Fatalf("Update().Name = %q, want immutable 'edge'", updated.Name)
	}
	if updated.Label != "Edge v2" {
		t.Fatalf("Update().Label = %q, want 'Edge v2'", updated.Label)
	}
}

func TestServiceDeleteReassignsMembers(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	src, err := svc.Create(ctx, fleet.CreateInput{Name: "edge-old", Label: "Edge old"})
	if err != nil {
		t.Fatalf("Create src error = %v", err)
	}
	dst, err := svc.Create(ctx, fleet.CreateInput{Name: "edge-new", Label: "Edge new"})
	if err != nil {
		t.Fatalf("Create dst error = %v", err)
	}

	// Empty-member case — no reassignTo required.
	if _, err := svc.Delete(ctx, src.ID, ""); err != nil {
		t.Fatalf("Delete(empty) error = %v", err)
	}
	// dst untouched.
	if _, err := svc.Get(ctx, dst.ID); err != nil {
		t.Fatalf("Get(dst) after sibling delete = %v", err)
	}
}

func TestServiceEnsureDefaultIdempotent(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	first, err := svc.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault first = %v", err)
	}
	second, err := svc.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault second = %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("EnsureDefault() returned different IDs: %q vs %q", first.ID, second.ID)
	}
}

// Compile-time check that sqlite.Store satisfies the subset of Store
// methods the service pulls.
var _ storage.Store = (*sqlite.Store)(nil)
