package history

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type fakeRepo struct {
	agg        []storage.ClientIPAggregateRecord
	aggLimit   int
	aggErr     error
	count      int
	countErr   error
	loadPoints []storage.ServerLoadPointRecord
}

func (f *fakeRepo) ListServerLoadPoints(context.Context, string, time.Time, time.Time) ([]storage.ServerLoadPointRecord, error) {
	return f.loadPoints, nil
}
func (f *fakeRepo) ListServerLoadHourly(context.Context, string, time.Time, time.Time) ([]storage.ServerLoadHourlyRecord, error) {
	return nil, nil
}
func (f *fakeRepo) ListDCHealthPoints(context.Context, string, time.Time, time.Time) ([]storage.DCHealthPointRecord, error) {
	return nil, nil
}
func (f *fakeRepo) AggregateClientIPHistory(_ context.Context, _ string, _, _ time.Time, limit int) ([]storage.ClientIPAggregateRecord, error) {
	f.aggLimit = limit
	if f.aggErr != nil {
		return nil, f.aggErr
	}
	if len(f.agg) > limit {
		return f.agg[:limit], nil
	}
	return f.agg, nil
}
func (f *fakeRepo) CountUniqueClientIPs(context.Context, string) (int, error) {
	return f.count, f.countErr
}

func mkAgg(n int) []storage.ClientIPAggregateRecord {
	out := make([]storage.ClientIPAggregateRecord, n)
	for i := range out {
		out[i].IPAddress = "10.0.0." + string(rune('0'+i))
	}
	return out
}

func TestClientIPsTruncatesWhenMoreThanLimit(t *testing.T) {
	t.Parallel()
	// 4 rows available, limit 3 → over-fetch 4, cap to 3, truncated.
	repo := &fakeRepo{agg: mkAgg(4)}
	svc := NewService(repo)
	rows, truncated, err := svc.ClientIPs(context.Background(), "c1", time.Time{}, time.Time{}, 3)
	if err != nil {
		t.Fatalf("ClientIPs: %v", err)
	}
	if repo.aggLimit != 4 {
		t.Fatalf("over-fetch should request limit+1=4, got %d", repo.aggLimit)
	}
	if len(rows) != 3 || !truncated {
		t.Fatalf("want 3 rows + truncated, got %d rows truncated=%v", len(rows), truncated)
	}
}

func TestClientIPsNotTruncatedAtLimit(t *testing.T) {
	t.Parallel()
	// Exactly limit rows available → not truncated.
	repo := &fakeRepo{agg: mkAgg(3)}
	svc := NewService(repo)
	rows, truncated, err := svc.ClientIPs(context.Background(), "c1", time.Time{}, time.Time{}, 3)
	if err != nil {
		t.Fatalf("ClientIPs: %v", err)
	}
	if len(rows) != 3 || truncated {
		t.Fatalf("want 3 rows + not truncated, got %d rows truncated=%v", len(rows), truncated)
	}
}

func TestClientIPsPropagatesStoreError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("boom")
	svc := NewService(&fakeRepo{aggErr: sentinel})
	if _, _, err := svc.ClientIPs(context.Background(), "c1", time.Time{}, time.Time{}, 3); !errors.Is(err, sentinel) {
		t.Fatalf("ClientIPs should propagate store error, got %v", err)
	}
}

func TestCountUniqueClientIPsPropagatesError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("count-fail")
	svc := NewService(&fakeRepo{countErr: sentinel})
	if _, err := svc.CountUniqueClientIPs(context.Background(), "c1"); !errors.Is(err, sentinel) {
		t.Fatalf("CountUniqueClientIPs should propagate error, got %v", err)
	}
}

func TestServerLoadPointsDelegates(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{loadPoints: []storage.ServerLoadPointRecord{{AgentID: "a1"}}}
	svc := NewService(repo)
	got, err := svc.ServerLoadPoints(context.Background(), "a1", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("ServerLoadPoints: %v", err)
	}
	if len(got) != 1 || got[0].AgentID != "a1" {
		t.Fatalf("delegation failed: %#v", got)
	}
}
