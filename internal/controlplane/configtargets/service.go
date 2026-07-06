// Package configtargets owns the persistence + read semantics of per-scope
// agent config targets (the desired editable Telemt sections for a fleet
// group or a single agent). It is the exemplar thin domain service for the
// P8.2 layering split (fleet.Service pattern): the HTTP handlers keep request
// decoding, the editable-section allowlist validation, and the drift view;
// this service owns only the store round-trips and the "no record = empty
// sections" / "preserve CreatedAt on update" rules.
package configtargets

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Repository is the subset of storage.Store this service needs. storage.Store
// satisfies it structurally, so no adapter is required.
type Repository interface {
	GetAgentConfigTarget(ctx context.Context, scopeType, scopeID string) (storage.AgentConfigTargetRecord, error)
	UpsertAgentConfigTarget(ctx context.Context, rec storage.AgentConfigTargetRecord) error
}

// Service persists and reads config-target sections for a scope.
type Service struct {
	repo Repository
	now  func() time.Time
}

// NewService constructs a Service. now supplies the UpdatedAt/CreatedAt clock
// (injected so tests can pin it).
func NewService(repo Repository, now func() time.Time) *Service {
	return &Service{repo: repo, now: now}
}

// Sections returns the stored sections for a scope. A missing target is NOT an
// error — it yields an empty (non-nil) map, matching the handlers' "no target
// → {}" contract. A non-NotFound store error, or malformed stored JSON, is
// propagated.
func (s *Service) Sections(ctx context.Context, scopeType, scopeID string) (map[string]any, error) {
	rec, err := s.repo.GetAgentConfigTarget(ctx, scopeType, scopeID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	sections := map[string]any{}
	if rec.SectionsJSON != "" {
		if err := json.Unmarshal([]byte(rec.SectionsJSON), &sections); err != nil {
			return nil, err
		}
	}
	return sections, nil
}

// Upsert serializes sections and stores them for the scope, preserving the
// CreatedAt of an existing record (a fresh record stamps CreatedAt = now).
func (s *Service) Upsert(ctx context.Context, scopeType, scopeID string, sections map[string]any) error {
	encoded, err := json.Marshal(sections)
	if err != nil {
		return err
	}
	now := s.now()
	createdAt := now
	if existing, err := s.repo.GetAgentConfigTarget(ctx, scopeType, scopeID); err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	return s.repo.UpsertAgentConfigTarget(ctx, storage.AgentConfigTargetRecord{
		ScopeType:    scopeType,
		ScopeID:      scopeID,
		SectionsJSON: string(encoded),
		CreatedAt:    createdAt,
		UpdatedAt:    now,
	})
}
