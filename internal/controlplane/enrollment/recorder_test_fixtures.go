package enrollment

import (
	"context"
	"sync"
	"time"
)

type memStore struct {
	mu       sync.Mutex
	attempts map[string]*Attempt
	events   map[string][]Event
}

func newMemStore() *memStore {
	return &memStore{
		attempts: map[string]*Attempt{},
		events:   map[string][]Event{},
	}
}

func (m *memStore) CreateAttempt(_ context.Context, a Attempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts[a.ID] = &a
	return nil
}

func (m *memStore) AppendEvent(_ context.Context, attemptID string, ev Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[attemptID] = append(m.events[attemptID], ev)
	return nil
}

func (m *memStore) AttachAgent(_ context.Context, attemptID, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.attempts[attemptID]; ok {
		a.AgentID = agentID
	}
	return nil
}

func (m *memStore) Complete(_ context.Context, attemptID string, finishedAt time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.attempts[attemptID]; ok && a.Status == StatusInProgress {
		a.Status = StatusSuccess
		a.FinishedAt = finishedAt
		return true, nil
	}
	return false, nil
}

func (m *memStore) Fail(_ context.Context, attemptID string, finishedAt time.Time, code ErrorCode, msg string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.attempts[attemptID]; ok && a.Status == StatusInProgress {
		a.Status = StatusFailed
		a.FinishedAt = finishedAt
		a.ErrorCode = code
		a.ErrorMsg = msg
		return true, nil
	}
	return false, nil
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}
