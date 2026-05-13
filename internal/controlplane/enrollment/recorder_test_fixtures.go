package enrollment

import (
	"context"
	"sync"
	"time"
)

type memAttempt struct {
	ID         string
	TokenID    string
	AgentID    string
	Mode       Mode
	ClientAddr string
	RequestID  string
	Status     Status
	ErrorCode  ErrorCode
	ErrorMsg   string
	StartedAt  time.Time
	FinishedAt time.Time
}

type memEvent struct {
	Step       Step
	Level      Level
	Message    string
	FieldsJSON string
	Ts         time.Time
}

type memStore struct {
	mu       sync.Mutex
	attempts map[string]*memAttempt
	events   map[string][]memEvent
}

func newMemStore() *memStore {
	return &memStore{
		attempts: map[string]*memAttempt{},
		events:   map[string][]memEvent{},
	}
}

func (m *memStore) CreateAttempt(_ context.Context, a memAttempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts[a.ID] = &a
	return nil
}

func (m *memStore) AppendEvent(_ context.Context, attemptID string, ev memEvent) error {
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

func (m *memStore) Complete(_ context.Context, attemptID string, finishedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.attempts[attemptID]; ok && a.Status == StatusInProgress {
		a.Status = StatusSuccess
		a.FinishedAt = finishedAt
	}
	return nil
}

func (m *memStore) Fail(_ context.Context, attemptID string, finishedAt time.Time, code ErrorCode, msg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.attempts[attemptID]; ok && a.Status == StatusInProgress {
		a.Status = StatusFailed
		a.FinishedAt = finishedAt
		a.ErrorCode = code
		a.ErrorMsg = msg
	}
	return nil
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}
