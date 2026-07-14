package auth

import (
	"context"
	"sort"
	"sync"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// memoryUserStore is the storage.UserStore used when the service is built
// without a persistent backend (NewService — tests, and the placeholder the
// control-plane constructs before initStoreBackedSubsystems swaps in the real
// store). It exists so Service has exactly ONE user-data path: every read and
// write goes through s.userStore, never through a parallel in-memory map kept
// in sync by hand (R5 §2.3 — the old double was write-only whenever a real
// store was wired, so the two copies could silently diverge).
//
// Ordering matches the SQL stores: ListUsers sorts by CreatedAt then ID.
type memoryUserStore struct {
	mu    sync.RWMutex
	users map[string]storage.UserRecord // keyed by user ID
}

func newMemoryUserStore() *memoryUserStore {
	return &memoryUserStore{users: make(map[string]storage.UserRecord)}
}

func (m *memoryUserStore) PutUser(_ context.Context, user storage.UserRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, existing := range m.users {
		if existing.Username == user.Username && id != user.ID {
			return storage.ErrConflict
		}
	}
	m.users[user.ID] = user
	return nil
}

func (m *memoryUserStore) DeleteUser(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.users[userID]; !ok {
		return storage.ErrNotFound
	}
	delete(m.users, userID)
	return nil
}

func (m *memoryUserStore) GetUserByID(_ context.Context, userID string) (storage.UserRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, ok := m.users[userID]
	if !ok {
		return storage.UserRecord{}, storage.ErrNotFound
	}
	return user, nil
}

func (m *memoryUserStore) GetUserByUsername(_ context.Context, username string) (storage.UserRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, user := range m.users {
		if user.Username == username {
			return user, nil
		}
	}
	return storage.UserRecord{}, storage.ErrNotFound
}

func (m *memoryUserStore) ListUsers(_ context.Context) ([]storage.UserRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	records := make([]storage.UserRecord, 0, len(m.users))
	for _, user := range m.users {
		records = append(records, user)
	}
	sort.Slice(records, func(left, right int) bool {
		if records[left].CreatedAt.Equal(records[right].CreatedAt) {
			return records[left].ID < records[right].ID
		}
		return records[left].CreatedAt.Before(records[right].CreatedAt)
	})
	return records, nil
}
