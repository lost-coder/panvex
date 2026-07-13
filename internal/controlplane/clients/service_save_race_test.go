package clients

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// orderingRepo records the last committed client name under its own lock so a
// concurrency test can assert the mirror agrees with the DB.
type orderingRepo struct {
	*fakeRepo
	mu   sync.Mutex
	last string
}

func (r *orderingRepo) Save(_ context.Context, c Client) error {
	r.mu.Lock()
	r.last = c.Name
	r.mu.Unlock()
	return nil
}

func (r *orderingRepo) committed() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

// TestSaveMirrorMatchesDBUnderConcurrency (R4 §1.6): concurrent Saves of the
// SAME client must leave the handler-facing mirror equal to the last committed
// DB state. Without the per-client save lock the mirror could be applied in an
// order opposite to the commits, diverging until restart. Run with
// -race -count=20.
func TestSaveMirrorMatchesDBUnderConcurrency(t *testing.T) {
	repo := &orderingRepo{fakeRepo: newFakeRepo()}
	svc := NewService(ServiceConfig{
		Repo:           repo,
		DiscoveredRepo: newFakeDiscoveredRepo(),
		UoW:            newFakeUoW(&fakeRepoSet{clients: repo, discovered: newFakeDiscoveredRepo()}),
	})

	const id ClientID = "c-1"
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if err := svc.Save(context.Background(), Client{ID: id, Name: fmt.Sprintf("v%02d", n)}); err != nil {
				t.Errorf("Save: %v", err)
			}
		}(i)
	}
	wg.Wait()

	mirror, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if mirror.Name != repo.committed() {
		t.Fatalf("mirror name %q != last committed DB name %q (mirror/DB diverged)", mirror.Name, repo.committed())
	}
}
