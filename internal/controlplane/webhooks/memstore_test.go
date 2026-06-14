package webhooks

import (
	"context"
	"sort"
	"sync"
	"time"
)

// memStore is an in-memory Storage used only by tests in this
// package. Real backends (postgres, sqlite) live in
// internal/controlplane/storage/{postgres,sqlite}/webhooks.go.
type memStore struct {
	mu        sync.Mutex
	endpoints []Endpoint
	rows      map[string]*OutboxRow
}

func newMemStore() *memStore {
	return &memStore{rows: make(map[string]*OutboxRow)}
}

func (s *memStore) addEndpoint(ep Endpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endpoints = append(s.endpoints, ep)
}

func (s *memStore) ListEnabledEndpoints(ctx context.Context) ([]Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Endpoint, 0, len(s.endpoints))
	for _, ep := range s.endpoints {
		if ep.Enabled {
			// Defensive copy of the slice fields so the caller
			// can't mutate our store state.
			cp := ep
			cp.EventFilter = append([]string(nil), ep.EventFilter...)
			cp.Secret = append([]byte(nil), ep.Secret...)
			out = append(out, cp)
		}
	}
	return out, nil
}

func (s *memStore) InsertOutbox(ctx context.Context, row OutboxRow) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := row
	s.rows[row.ID] = &cp
	return nil
}

func (s *memStore) ClaimReady(ctx context.Context, now time.Time, max int) ([]Delivery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	endpoints := make(map[string]Endpoint, len(s.endpoints))
	for _, ep := range s.endpoints {
		endpoints[ep.ID] = ep
	}
	ids := make([]string, 0, len(s.rows))
	for id, r := range s.rows {
		if r.Dead || r.DeliveredAt != nil {
			continue
		}
		if r.NextAttemptAt.After(now) {
			continue
		}
		ids = append(ids, id)
	}
	// Deterministic order: oldest scheduled first, then ID for ties
	// (matches the worker's expectation when the test seeds many).
	sort.SliceStable(ids, func(i, j int) bool {
		ri, rj := s.rows[ids[i]], s.rows[ids[j]]
		if !ri.NextAttemptAt.Equal(rj.NextAttemptAt) {
			return ri.NextAttemptAt.Before(rj.NextAttemptAt)
		}
		return ids[i] < ids[j]
	})
	if max > 0 && len(ids) > max {
		ids = ids[:max]
	}
	out := make([]Delivery, 0, len(ids))
	for _, id := range ids {
		r := s.rows[id]
		ep, ok := endpoints[r.EndpointID]
		if !ok {
			continue
		}
		out = append(out, Delivery{Outbox: *r, Endpoint: ep})
	}
	return out, nil
}

func (s *memStore) MarkDelivered(ctx context.Context, id string, deliveredAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return ErrNotFound
	}
	t := deliveredAt
	r.DeliveredAt = &t
	return nil
}

func (s *memStore) MarkFailed(ctx context.Context, id string, attempt int, nextAttempt time.Time, errMsg string, dead bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return ErrNotFound
	}
	r.Attempt = attempt
	r.NextAttemptAt = nextAttempt
	r.LastError = errMsg
	r.Dead = dead
	return nil
}

// CRUD — minimal stubs so memStore satisfies Storage. Producer/Worker
// tests don't exercise these paths; storage-backend tests use the
// real DB.

func (s *memStore) CreateEndpoint(ctx context.Context, in EndpointInput, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ep := range s.endpoints {
		if ep.ID == in.ID || ep.Name == in.Name {
			return ErrNotFound // memStore doesn't model duplicate-key — tests don't hit it
		}
	}
	s.endpoints = append(s.endpoints, Endpoint{
		ID:           in.ID,
		Name:         in.Name,
		URL:          in.URL,
		Secret:       []byte(in.SecretCiphertext),
		EventFilter:  parseFilter(in.EventFilter),
		AllowPrivate: in.AllowPrivate,
		Enabled:      in.Enabled,
	})
	return nil
}

func (s *memStore) UpdateEndpoint(ctx context.Context, in EndpointInput, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, ep := range s.endpoints {
		if ep.ID == in.ID {
			ep.Name = in.Name
			ep.URL = in.URL
			if in.SecretCiphertext != "" {
				ep.Secret = []byte(in.SecretCiphertext)
			}
			ep.EventFilter = parseFilter(in.EventFilter)
			ep.AllowPrivate = in.AllowPrivate
			ep.Enabled = in.Enabled
			s.endpoints[i] = ep
			return nil
		}
	}
	return ErrNotFound
}

func (s *memStore) DeleteEndpoint(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, ep := range s.endpoints {
		if ep.ID == id {
			s.endpoints = append(s.endpoints[:i], s.endpoints[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (s *memStore) GetEndpointMeta(ctx context.Context, id string) (Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ep := range s.endpoints {
		if ep.ID == id {
			cp := ep
			cp.Secret = nil
			return cp, nil
		}
	}
	return Endpoint{}, ErrNotFound
}

func (s *memStore) ListEndpointMeta(ctx context.Context) ([]Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Endpoint, 0, len(s.endpoints))
	for _, ep := range s.endpoints {
		cp := ep
		cp.Secret = nil
		out = append(out, cp)
	}
	return out, nil
}

func (s *memStore) PruneOutbox(context.Context, time.Time) (int64, error) { return 0, nil }

func (s *memStore) snapshot(id string) (OutboxRow, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return OutboxRow{}, false
	}
	return *r, true
}

func (s *memStore) allRows() []OutboxRow {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OutboxRow, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
