package jobs

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// listRecentReference — эталонная (прежняя) реализация: полный снапшот,
// сортировка ascending, реверс, трим. Против неё проверяется top-K.
func listRecentReference(all []Job, limit int) []Job {
	sortJobsByCreatedAt(all) // существующий helper: CreatedAt asc, tie ID asc
	reversed := make([]Job, 0, limit)
	for i := len(all) - 1; i >= 0 && len(reversed) < limit; i-- {
		reversed = append(reversed, all[i])
	}
	return reversed
}

func TestListRecentTopKMatchesReference(t *testing.T) {
	base := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	svc := NewService()
	svc.SetNow(func() time.Time { return base })

	// 500 джоб: перемешанные времена + группы с ОДИНАКОВЫМ CreatedAt
	// (tie-break по ID обязан совпасть с эталоном). TTL=0 → expiry не
	// вмешивается.
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic seed for reproducible test fixtures, not security-sensitive
	for i := 0; i < 500; i++ {
		created := base.Add(time.Duration(rng.Intn(100)) * time.Minute)
		if _, err := svc.Enqueue(context.Background(), CreateJobInput{
			Action:         ActionRuntimeReload,
			TargetAgentIDs: []string{fmt.Sprintf("agent-%d", i)},
			TTL:            0,
			IdempotencyKey: fmt.Sprintf("key-%d", i),
			ActorID:        "user-1",
		}, created); err != nil {
			t.Fatalf("Enqueue(%d): %v", i, err)
		}
	}

	for _, limit := range []int{1, 10, 200, 499, 500} {
		got := svc.ListRecentWithContext(context.Background(), limit)
		want := listRecentReference(svc.ListWithContext(context.Background()), limit)
		if len(got) != len(want) {
			t.Fatalf("limit %d: len = %d, want %d", limit, len(got), len(want))
		}
		for i := range want {
			if got[i].ID != want[i].ID {
				t.Fatalf("limit %d, pos %d: ID = %q, want %q", limit, i, got[i].ID, want[i].ID)
			}
		}
	}
}

func TestListRecentStillExpiresStaleJobs(t *testing.T) {
	base := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	now := base
	svc := NewService()
	svc.SetNow(func() time.Time { return now })

	if _, err := svc.Enqueue(context.Background(), CreateJobInput{
		Action:         ActionRuntimeReload,
		TargetAgentIDs: []string{"agent-1"},
		TTL:            time.Minute,
		IdempotencyKey: "key-expire",
		ActorID:        "user-1",
	}, now); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Продвинуть время за TTL.
	now = base.Add(2 * time.Minute)

	got := svc.ListRecentWithContext(context.Background(), 10)
	if len(got) != 1 || got[0].Status != StatusExpired {
		t.Fatalf("job = %+v, want expired", got)
	}
}

func BenchmarkListRecent10kJobs(b *testing.B) {
	base := time.Date(2026, time.July, 2, 12, 0, 0, 0, time.UTC)
	svc := NewService()
	svc.SetNow(func() time.Time { return base })
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = 'x'
	}
	for i := 0; i < 10000; i++ {
		if _, err := svc.Enqueue(context.Background(), CreateJobInput{
			Action:         ActionRuntimeReload,
			TargetAgentIDs: []string{fmt.Sprintf("agent-%d", i)},
			TTL:            0,
			IdempotencyKey: fmt.Sprintf("key-%d", i),
			ActorID:        "user-1",
			PayloadJSON:    string(payload),
		}, base.Add(time.Duration(i)*time.Second)); err != nil {
			b.Fatalf("Enqueue(%d): %v", i, err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		svc.ListRecentWithContext(context.Background(), 200)
	}
}
