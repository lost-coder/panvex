package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// fakeConsumedTotpStore is a minimal in-test implementation of
// storage.ConsumedTotpStore used to assert that the auth service mirrors
// consumed TOTP codes to a persistent store and rebuilds its in-memory
// map from that store on RestoreSessions — the property that closes the
// post-restart replay window (audit S3). The serve path's invariant that
// this store is always wired is enforced in
// server.initStoreBackedSubsystems (fail-fast); this test guards the
// auth-side behaviour that fail-fast protects.
type fakeConsumedTotpStore struct {
	mu      sync.Mutex
	records map[totpUseKey]time.Time
}

func newFakeConsumedTotpStore() *fakeConsumedTotpStore {
	return &fakeConsumedTotpStore{records: make(map[totpUseKey]time.Time)}
}

func (f *fakeConsumedTotpStore) UpsertConsumedTotp(_ context.Context, record storage.ConsumedTotpRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[totpUseKey{UserID: record.UserID, Code: record.Code}] = record.UsedAt
	return nil
}

func (f *fakeConsumedTotpStore) ListConsumedTotp(_ context.Context) ([]storage.ConsumedTotpRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.ConsumedTotpRecord, 0, len(f.records))
	for key, usedAt := range f.records {
		out = append(out, storage.ConsumedTotpRecord{UserID: key.UserID, Code: key.Code, UsedAt: usedAt})
	}
	return out, nil
}

func (f *fakeConsumedTotpStore) DeleteExpiredConsumedTotp(_ context.Context, before time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, usedAt := range f.records {
		if usedAt.Before(before) {
			delete(f.records, key)
		}
	}
	return nil
}

// TestRestoreConsumedTotpRebuildsFromPersistentStore verifies that a
// freshly constructed Service repopulates its in-memory consumed-TOTP
// map from the persistent store. This is the behaviour that prevents a
// restart from reopening the TOTP replay window: a code consumed before
// the restart must still be seen as consumed afterwards (audit S3).
func TestRestoreConsumedTotpRebuildsFromPersistentStore(t *testing.T) {
	store := newFakeConsumedTotpStore()
	key := totpUseKey{UserID: "user-1", Code: "123456"}
	// Simulate a code consumed shortly before the restart (well within
	// the ~90s acceptance window).
	if err := store.UpsertConsumedTotp(context.Background(), storage.ConsumedTotpRecord{
		UserID: key.UserID,
		Code:   key.Code,
		UsedAt: time.Now().UTC().Add(-5 * time.Second),
	}); err != nil {
		t.Fatalf("UpsertConsumedTotp: %v", err)
	}

	svc := NewService()
	svc.SetConsumedTotpStore(store)
	svc.restoreConsumedTotp(context.Background())

	svc.mu.Lock()
	_, ok := svc.consumedTotp[key]
	svc.mu.Unlock()
	if !ok {
		t.Fatal("restoreConsumedTotp did not rebuild the consumed code from the persistent store; replay window reopens after restart")
	}
}

// TestRestoreConsumedTotpDropsExpiredCodes verifies the restore path
// prunes codes older than the acceptance window so the in-memory map
// does not grow unbounded across restarts and stale codes are not
// resurrected.
func TestRestoreConsumedTotpDropsExpiredCodes(t *testing.T) {
	store := newFakeConsumedTotpStore()
	expired := totpUseKey{UserID: "user-1", Code: "000000"}
	if err := store.UpsertConsumedTotp(context.Background(), storage.ConsumedTotpRecord{
		UserID: expired.UserID,
		Code:   expired.Code,
		UsedAt: time.Now().UTC().Add(-10 * time.Minute),
	}); err != nil {
		t.Fatalf("UpsertConsumedTotp: %v", err)
	}

	svc := NewService()
	svc.SetConsumedTotpStore(store)
	svc.restoreConsumedTotp(context.Background())

	svc.mu.Lock()
	_, ok := svc.consumedTotp[expired]
	svc.mu.Unlock()
	if ok {
		t.Fatal("restoreConsumedTotp kept an expired code; acceptance window not enforced")
	}
}
