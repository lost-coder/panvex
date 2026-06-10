package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type ctxErrJobStore struct{ Store }

func (ctxErrJobStore) ListJobs(ctx context.Context) ([]storage.JobRecord, error) {
	return nil, ctx.Err()
}

func TestNewServiceWithStore_RestoreUsesProvidedContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := NewServiceWithStore(ctx, ctxErrJobStore{})
	if err := service.StartupError(); !errors.Is(err, context.Canceled) {
		t.Fatalf("StartupError() = %v, want context.Canceled", err)
	}
}
