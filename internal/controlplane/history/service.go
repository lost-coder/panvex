// Package history is the read-only domain service behind the panel's
// time-series history endpoints (server load, DC health, per-client IP
// history). It owns the store round-trips and the client-IP truncation rule;
// the HTTP handlers keep scope checks, time-range parsing, the raw-vs-hourly
// resolution policy, and geoip enrichment.
package history

import (
	"context"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Repository is the read subset of storage.Store this service needs.
// storage.Store satisfies it structurally.
type Repository interface {
	ListServerLoadPoints(ctx context.Context, agentID string, from, to time.Time) ([]storage.ServerLoadPointRecord, error)
	ListServerLoadHourly(ctx context.Context, agentID string, from, to time.Time) ([]storage.ServerLoadHourlyRecord, error)
	ListDCHealthPoints(ctx context.Context, agentID string, from, to time.Time) ([]storage.DCHealthPointRecord, error)
	AggregateClientIPHistory(ctx context.Context, clientID string, from, to time.Time, limit int) ([]storage.ClientIPAggregateRecord, error)
	CountUniqueClientIPs(ctx context.Context, clientID string) (int, error)
}

// Service is a passive read facade over the history repository.
type Service struct {
	repo Repository
}

// NewService constructs a history Service over the given repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// ServerLoadPoints returns raw per-sample server-load points in the window.
func (s *Service) ServerLoadPoints(ctx context.Context, agentID string, from, to time.Time) ([]storage.ServerLoadPointRecord, error) {
	return s.repo.ListServerLoadPoints(ctx, agentID, from, to)
}

// ServerLoadHourly returns hourly-rollup server-load points in the window.
func (s *Service) ServerLoadHourly(ctx context.Context, agentID string, from, to time.Time) ([]storage.ServerLoadHourlyRecord, error) {
	return s.repo.ListServerLoadHourly(ctx, agentID, from, to)
}

// DCHealthPoints returns raw DC-health points in the window.
func (s *Service) DCHealthPoints(ctx context.Context, agentID string, from, to time.Time) ([]storage.DCHealthPointRecord, error) {
	return s.repo.ListDCHealthPoints(ctx, agentID, from, to)
}

// CountUniqueClientIPs returns the authoritative distinct-IP total for a client.
func (s *Service) CountUniqueClientIPs(ctx context.Context, clientID string) (int, error) {
	return s.repo.CountUniqueClientIPs(ctx, clientID)
}

// ClientIPs returns up to limit aggregated per-IP rows for the client in the
// window, plus whether the result was truncated. Truncation is detected by
// over-fetching one row (limit+1) so no separate COUNT round-trip is needed;
// the returned slice is already capped to limit.
func (s *Service) ClientIPs(ctx context.Context, clientID string, from, to time.Time, limit int) (rows []storage.ClientIPAggregateRecord, truncated bool, err error) {
	aggregates, err := s.repo.AggregateClientIPHistory(ctx, clientID, from, to, limit+1)
	if err != nil {
		return nil, false, err
	}
	if len(aggregates) > limit {
		aggregates = aggregates[:limit]
		truncated = true
	}
	return aggregates, truncated, nil
}
