package agents

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// Repository is the narrow persistence surface that agents.Service
// exercises for an agent's IDENTITY. It exists so the service depends
// on exactly the store methods it calls rather than the whole
// storage.Store aggregate (audit finding A6): the concrete store passed
// to NewService still satisfies this subset, so wiring is unchanged.
//
// Method signatures are copied verbatim from storage.Store so any
// storage.Store value (storage/sqlite, storage/postgres) implements
// Repository without an adapter.
type Repository interface {
	// ListAgents returns every persisted agent identity row. Used by
	// Restore at boot to rehydrate the in-memory mirror.
	ListAgents(ctx context.Context) ([]storage.AgentRecord, error)
	// PutAgent upserts a single agent identity row (UPSERT on id). Used
	// by the service's write-through UpsertIdentity.
	PutAgent(ctx context.Context, agent storage.AgentRecord) error
}

// Service is the single owner of agent state, modelled on
// clients.Service (mirror + write-through). It owns an in-memory mirror
// of every agent's IDENTITY — the persistence-shaped fields that come
// from storage.AgentRecord (id, node_name, fleet_group_id, version,
// read_only, cert_* and last_seen_at) — backed by a Repository.
//
// This is the D.1 SHELL: the mirror holds identity only. Runtime
// (presence, derived lifecycle) and instances are added in D.2/D.3, at
// which point the server's read/write paths are repointed at this
// service. For now the service coexists with the server's own
// s.agents map and is not yet read by any handler.
//
// # Lock discipline
//
// Service.mu protects the mirror map. The service owns its own mutex
// and never reaches into Server.mu (mirroring clients.Service), so the
// documented lock ordering is preserved.
type Service struct {
	now  func() time.Time
	repo Repository

	mu     sync.RWMutex
	mirror map[string]storage.AgentRecord
}

// NewService constructs a Service backed by repo. The mirror starts
// empty; call Restore to populate it from the Repository. A nil now
// falls back to time.Now.
func NewService(repo Repository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		now:    now,
		repo:   repo,
		mirror: make(map[string]storage.AgentRecord),
	}
}

// Restore loads all agent identities from the Repository into the
// in-memory mirror. Idempotent: it is a full snapshot, so subsequent
// calls overwrite (not merge) the mirror with the latest state.
func (s *Service) Restore(ctx context.Context) error {
	list, err := s.repo.ListAgents(ctx)
	if err != nil {
		return fmt.Errorf("agents.Service.Restore: list agents: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.mirror = make(map[string]storage.AgentRecord, len(list))
	for _, rec := range list {
		s.mirror[rec.ID] = cloneAgentRecord(rec)
	}
	return nil
}

// UpsertIdentity persists the agent identity via the Repository and, on
// success, updates the in-memory mirror. The mirror is left unchanged
// if the persist fails. Identity is not cumulative, so a plain
// overwrite after a successful write matches clients.Service's
// PersistDeployment shape.
func (s *Service) UpsertIdentity(ctx context.Context, rec storage.AgentRecord) error {
	if err := s.repo.PutAgent(ctx, rec); err != nil {
		return fmt.Errorf("agents.Service.UpsertIdentity: put agent %s: %w", rec.ID, err)
	}
	s.mu.Lock()
	s.mirror[rec.ID] = cloneAgentRecord(rec)
	s.mu.Unlock()
	return nil
}

// Remove evicts an agent identity from the in-memory mirror.
//
// D.1 scope: this is mirror-only. The authoritative deregister DB
// delete (instances, agent row, revocation record) is still owned by
// the server's deregister flow; D.2 will move that boundary. Remove
// exists now so the parallel mirror can be kept consistent once D.2
// repoints writes.
func (s *Service) Remove(id string) {
	s.mu.Lock()
	delete(s.mirror, id)
	s.mu.Unlock()
}

// Get returns a deep copy of the cached agent identity by ID. ok is
// false when the ID is not in the mirror.
func (s *Service) Get(id string) (storage.AgentRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.mirror[id]
	if !ok {
		return storage.AgentRecord{}, false
	}
	return cloneAgentRecord(rec), true
}

// List returns deep copies of every cached agent identity. Order is
// unspecified — callers that need ordering must sort.
func (s *Service) List() []storage.AgentRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]storage.AgentRecord, 0, len(s.mirror))
	for _, rec := range s.mirror {
		out = append(out, cloneAgentRecord(rec))
	}
	return out
}

// cloneAgentRecord returns a deep copy of rec so callers cannot mutate
// the mirror through the returned value. storage.AgentRecord carries
// two *time.Time pointers and a []byte slice; copying the struct alone
// would alias those.
func cloneAgentRecord(rec storage.AgentRecord) storage.AgentRecord {
	out := rec
	if rec.CertIssuedAt != nil {
		v := *rec.CertIssuedAt
		out.CertIssuedAt = &v
	}
	if rec.CertExpiresAt != nil {
		v := *rec.CertExpiresAt
		out.CertExpiresAt = &v
	}
	if rec.CertSPKISHA256 != nil {
		out.CertSPKISHA256 = append([]byte(nil), rec.CertSPKISHA256...)
	}
	return out
}
