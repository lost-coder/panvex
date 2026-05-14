// This file ships in the production binary (no `_test.go` suffix) so that
// external test packages — e.g. internal/controlplane/server's HTTP-level
// integration tests and internal/controlplane/agenttransport's outbound
// integration test — can swap a MemStore into a Recorder without each
// caller redefining its own fixture. Do not reference MemStore from
// non-test production code.

package enrollment

import (
	"context"
	"encoding/json"
	"sort"
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

// ListAttempts returns matching attempts most-recent first. Filtering
// happens in-memory because tests almost never exercise large fixtures.
func (m *MemStore) ListAttempts(_ context.Context, f ListFilter) ([]AttemptDTO, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []AttemptDTO{}
	for _, a := range m.attempts {
		if f.TokenID != nil && a.TokenID != *f.TokenID {
			continue
		}
		if f.AgentID != nil && a.AgentID != *f.AgentID {
			continue
		}
		if f.Status != nil && a.Status != *f.Status {
			continue
		}
		if f.Mode != nil && a.Mode != *f.Mode {
			continue
		}
		if f.ErrorCode != nil && string(a.ErrorCode) != *f.ErrorCode {
			continue
		}
		if f.StartedAfter != nil && a.StartedAt.Before(*f.StartedAfter) {
			continue
		}
		if f.StartedBefore != nil && !a.StartedAt.Before(*f.StartedBefore) {
			continue
		}
		if f.CursorTs != nil {
			if a.StartedAt.After(*f.CursorTs) {
				continue
			}
			if a.StartedAt.Equal(*f.CursorTs) {
				if f.CursorID == nil || a.ID >= *f.CursorID {
					continue
				}
			}
		}
		out = append(out, toAttemptDTO(*a))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// GetWithEvents returns the attempt + its full timeline. Returns
// (nil, nil) when id is unknown so the HTTP handler can map that to 404.
func (m *MemStore) GetWithEvents(_ context.Context, id string) (*AttemptWithEvents, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.attempts[id]
	if !ok {
		return nil, nil
	}
	evs := make([]EventDTO, 0, len(m.events[id]))
	for _, e := range m.events[id] {
		evs = append(evs, toEventDTO(e))
	}
	res := AttemptWithEvents{Attempt: toAttemptDTO(*a), Events: evs}
	return &res, nil
}

// DeleteOlderThan removes attempts (and their events) whose StartedAt is
// strictly before cutoff. Returns the number of attempts removed.
func (m *MemStore) DeleteOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for id, a := range m.attempts {
		if a.StartedAt.Before(cutoff) {
			delete(m.attempts, id)
			delete(m.events, id)
			n++
		}
	}
	return n, nil
}

// InsertAttemptForTest seeds an attempt directly into the in-memory
// store. Tests that need to back-date an attempt's StartedAt (e.g. the
// retention-worker test) use this helper instead of going through
// Recorder.Begin, which always stamps the current clock.
func (m *MemStore) InsertAttemptForTest(a Attempt) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := a
	m.attempts[a.ID] = &cp
}

func toAttemptDTO(a Attempt) AttemptDTO {
	dto := AttemptDTO{
		ID:           a.ID,
		TokenID:      a.TokenID,
		AgentID:      a.AgentID,
		Mode:         a.Mode,
		ClientAddr:   a.ClientAddr,
		RequestID:    a.RequestID,
		Status:       a.Status,
		ErrorCode:    string(a.ErrorCode),
		ErrorMessage: a.ErrorMsg,
		StartedAt:    a.StartedAt,
	}
	if !a.FinishedAt.IsZero() {
		t := a.FinishedAt
		dto.FinishedAt = &t
	}
	return dto
}

func toEventDTO(e Event) EventDTO {
	dto := EventDTO{Step: e.Step, Level: e.Level, Message: e.Message, Ts: e.Ts}
	if e.FieldsJSON != "" {
		var f map[string]any
		if err := json.Unmarshal([]byte(e.FieldsJSON), &f); err == nil {
			dto.Fields = f
		}
	}
	return dto
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}
