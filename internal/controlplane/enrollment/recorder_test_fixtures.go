// This file ships in the production binary (no `_test.go` suffix) so that
// external test packages — e.g. internal/controlplane/server's HTTP-level
// integration tests and internal/controlplane/agenttransport's outbound
// integration test — can swap a MemStore into a Recorder without each
// caller redefining its own fixture. Do not reference MemStore from
// non-test production code.

package enrollment

import (
	"context"
	"sync"
	"time"
)

// MemStore is an in-memory Store implementation used by tests. It is
// exported (rather than the previous unexported memStore) so that tests in
// other packages can construct and inspect a recorder-backing store without
// rebuilding their own copy.
type MemStore struct {
	mu       sync.Mutex
	attempts map[string]*Attempt
	events   map[string][]Event
}

// NewMemStoreForTest returns a fresh in-memory Store suitable for tests. The
// `ForTest` suffix exists to make accidental production use stand out.
func NewMemStoreForTest() *MemStore {
	return &MemStore{
		attempts: map[string]*Attempt{},
		events:   map[string][]Event{},
	}
}

func (m *MemStore) CreateAttempt(_ context.Context, a Attempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts[a.ID] = &a
	return nil
}

func (m *MemStore) AppendEvent(_ context.Context, attemptID string, ev Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[attemptID] = append(m.events[attemptID], ev)
	return nil
}

func (m *MemStore) AttachAgent(_ context.Context, attemptID, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.attempts[attemptID]; ok {
		a.AgentID = agentID
	}
	return nil
}

func (m *MemStore) Complete(_ context.Context, attemptID string, finishedAt time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.attempts[attemptID]; ok && a.Status == StatusInProgress {
		a.Status = StatusSuccess
		a.FinishedAt = finishedAt
		return true, nil
	}
	return false, nil
}

func (m *MemStore) Fail(_ context.Context, attemptID string, finishedAt time.Time, code ErrorCode, msg string) (bool, error) {
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

// SnapshotAttempts returns a copy of the current attempts. Order is
// unstable; callers should look up by ID or filter as needed.
func (m *MemStore) SnapshotAttempts() []Attempt {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Attempt, 0, len(m.attempts))
	for _, a := range m.attempts {
		out = append(out, *a)
	}
	return out
}

// SnapshotEvents returns a copy of the events recorded for attemptID in
// insertion order.
func (m *MemStore) SnapshotEvents(attemptID string) []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events[attemptID]))
	copy(out, m.events[attemptID])
	return out
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}
