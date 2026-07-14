package auth

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// BenchmarkGetSessionUnderLoginLoad measures the read path — GetSession runs
// once per authenticated HTTP request — while logins hammer the same service.
//
// Before R7 this path took the WRITE lock and swept the entire session map on
// every call, so concurrent requests serialised against each other and against
// every login. Now it takes the read lock and touches one map entry.
func BenchmarkGetSessionUnderLoginLoad(b *testing.B) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	svc := NewService()
	svc.SetNow(func() time.Time { return now })

	const users = 8
	for i := 0; i < users; i++ {
		if _, _, err := svc.BootstrapUser(context.Background(), BootstrapInput{
			Username: "user" + strconv.Itoa(i),
			Password: "Correct1horse2battery",
			Role:     RoleViewer,
		}, now); err != nil {
			b.Fatalf("BootstrapUser: %v", err)
		}
	}

	session, err := svc.Authenticate(context.Background(), LoginInput{
		Username: "user0",
		Password: "Correct1horse2battery",
	}, now)
	if err != nil {
		b.Fatalf("Authenticate: %v", err)
	}

	// Populate the map so the removed full-map sweep would have real work.
	svc.mu.Lock()
	for i := 0; i < 2000; i++ {
		id := "filler-" + strconv.Itoa(i)
		svc.sessions[id] = Session{ID: id, UserID: "user0", CreatedAt: now, LastSeenAt: now}
	}
	svc.mu.Unlock()

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			// A login is dominated by Argon2id, so keep the rate modest: this
			// goroutine models "someone is logging in while the fleet works".
			_, _ = svc.Authenticate(context.Background(), LoginInput{
				Username: "user" + strconv.Itoa(i%users),
				Password: "Correct1horse2battery",
			}, now)
		}
	}()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := svc.GetSession(session.ID); err != nil {
				b.Errorf("GetSession: %v", err)
			}
		}
	})
	b.StopTimer()
	close(stop)
	<-done
}
