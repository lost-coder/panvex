package clients

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/audit"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

const seqClientAssignment = "client-assignment"

// usageMirror is the in-Service snapshot of (client, agent) usage.
// Distinct from clients.Usage (Repository row type) and clients.UsageSnapshot
// (in-memory mirror value type used by legacy methods) to avoid name
// collision while we migrate in Phase 6.
type usageMirror struct {
	ClientID         ClientID
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
	ActiveUniqueIPs  int
	ObservedAt       time.Time
	LastSeq          uint64
}

// ErrNotFound is returned by Service.Get when the requested Client ID
// is not present in the in-memory mirror. Use errors.Is for checks.
var ErrNotFound = errors.New("clients: not found")

// ClientsRepoSet is the subset of uow.RepoSet that clients.Service
// requires. Defined here to avoid an import cycle:
// uow → clients, so clients must not import uow.
// The concrete uow.UnitOfWork satisfies ServiceUoW via an adapter in
// the server/bootstrap layer.
type ClientsRepoSet interface {
	Clients() Repository
	Discovered() discovered.Repository
	Audit() audit.Repository
}

// ServiceUoW is the unit-of-work interface that clients.Service
// accepts. It is structurally equivalent to uow.UnitOfWork but scoped
// to the two repositories Service needs (Clients + Discovered).
// Callers provide an adapter that delegates to the real uow.UnitOfWork.
type ServiceUoW interface {
	Do(ctx context.Context, fn func(rs ClientsRepoSet) error) error
}

// Service is the orchestration entry point for managed clients. It owns
// the in-memory store for clients, assignments, deployments, and live
// usage snapshots, and provides the pure-query surface that the
// control-plane HTTP and gRPC handlers consume.
//
// Stateful mutation orchestration (create/update/rotate/delete/adopt)
// still lives on controlplane/server.Server for now — those flows also
// interact with the jobs service, event bus, audit log, and cert
// authority. The remaining P3-ARCH-01b follow-ups will migrate those
// methods onto Service once the jobs + events dependencies are
// constructor-injected.
//
// # Lock discipline
//
// Service.mu protects the four in-memory maps (clients, assignments,
// deployments, usage) and the client/assignment sequence counters. The
// caller-supplied agent-topology snapshot (AgentTopology) is produced
// by the server under its own mu lock, so Service never holds mu while
// asking the server for topology. This preserves the documented lock
// ordering (Server.mu -> Service.mu -> Server.metricsAuditMu).
//
// # Nil-store mode
//
// When the optional Store dependency is nil, Service acts as a pure
// in-memory store — no persistence is attempted. This matches how the
// server is constructed in unit tests that set Options.Store = nil.
type Service struct {
	store storage.Store
	now   func() time.Time
	vault *secretvault.Vault

	mu            sync.RWMutex
	clients       map[string]Client
	assignments   map[string][]Assignment
	deployments   map[string]map[string]Deployment
	usage         map[string]map[string]UsageSnapshot
	lastUsageSeq  map[string]uint64
	clientSeq     uint64
	assignmentSeq uint64
	discoveredSeq uint64

	// Phase 6 additions: Repository + UoW + in-memory mirror.
	repo           Repository
	discoveredRepo discovered.Repository
	uow            ServiceUoW

	mirrorClients      map[ClientID]Client
	mirrorAssignments  map[ClientID][]Assignment
	mirrorDeployments  map[ClientID]map[string]Deployment // outer=ClientID, inner=AgentID
	mirrorUsage        map[ClientID]map[string]usageMirror
	mirrorLastUsageSeq map[string]uint64 // per-agent
}

// NewService returns a Service with no Store and the wall clock. Kept
// for callers (including tests) that only exercise the pure helpers.
func NewService() *Service {
	return NewServiceWithDeps(nil, nil)
}

// NewServiceWithDeps constructs a Service backed by the given Store and
// clock. Either may be nil; nil store means "in-memory only", nil now
// falls back to time.Now.
func NewServiceWithDeps(store storage.Store, now func() time.Time) *Service {
	return NewServiceWithVault(store, now, nil)
}

// NewServiceWithVault is the vault-aware constructor. A nil or disabled
// vault keeps client secrets as plaintext at-rest (legacy behaviour);
// any other vault encrypts them via the client_secret domain key.
func NewServiceWithVault(store storage.Store, now func() time.Time, vault *secretvault.Vault) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:        store,
		now:          now,
		vault:        vault,
		clients:      make(map[string]Client),
		assignments:  make(map[string][]Assignment),
		deployments:  make(map[string]map[string]Deployment),
		usage:        make(map[string]map[string]UsageSnapshot),
		lastUsageSeq: make(map[string]uint64),
	}
}

// ServiceConfig carries the dependencies for NewServiceV2. Fields are
// additive over the legacy constructors — Store is optional during
// Phase 6 (legacy methods still use it).
type ServiceConfig struct {
	Repo           Repository
	DiscoveredRepo discovered.Repository
	UoW            ServiceUoW
	Vault          *secretvault.Vault
	Store          storage.Store
	Now            func() time.Time
}

// NewServiceV2 constructs a Service with the full Phase 6 dependency
// set: a clients.Repository, a discovered.Repository, a UoW, and the
// optional legacy Store. The in-memory mirror maps are pre-allocated;
// call Service.Restore to populate them from the Repository.
//
// Phase 7 will wire the server to use NewServiceV2 and migrate handlers
// to the new mirror-backed methods; Phase 8 removes the legacy Store
// path and the old constructors.
func NewServiceV2(cfg ServiceConfig) *Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:        cfg.Store,
		now:          now,
		vault:        cfg.Vault,
		clients:      make(map[string]Client),
		assignments:  make(map[string][]Assignment),
		deployments:  make(map[string]map[string]Deployment),
		usage:        make(map[string]map[string]UsageSnapshot),
		lastUsageSeq: make(map[string]uint64),

		repo:           cfg.Repo,
		discoveredRepo: cfg.DiscoveredRepo,
		uow:            cfg.UoW,

		mirrorClients:      make(map[ClientID]Client),
		mirrorAssignments:  make(map[ClientID][]Assignment),
		mirrorDeployments:  make(map[ClientID]map[string]Deployment),
		mirrorUsage:        make(map[ClientID]map[string]usageMirror),
		mirrorLastUsageSeq: make(map[string]uint64),
	}
}

// Vault exposes the configured vault so other parts of the control
// plane can encrypt/decrypt records at the same boundaries.
func (s *Service) Vault() *secretvault.Vault {
	return s.vault
}

// SetNow overrides the time source. Used by tests that inject a
// controllable clock after construction.
func (s *Service) SetNow(now func() time.Time) {
	if now == nil {
		return
	}
	s.now = now
}

// --- Pure helper method wrappers (kept for backwards compatibility) ---

// ResolveTargetAgentIDs is a method wrapper over the package-level
// pure helper. See ResolveTargetAgentIDs in resolver.go.
func (s *Service) ResolveTargetAgentIDs(assignments []Assignment, topology AgentTopology) []string {
	return ResolveTargetAgentIDs(assignments, topology)
}

// ResolveIDByName is a method wrapper over the package-level pure
// helper. See ResolveIDByName in resolver.go.
func (s *Service) ResolveIDByName(
	clients map[string]Client,
	assignmentsByClient map[string][]Assignment,
	agentID string,
	agentFleetGroupID string,
	clientName string,
) string {
	return ResolveIDByName(clients, assignmentsByClient, agentID, agentFleetGroupID, clientName)
}

// AggregateUsage is a method wrapper over the package-level pure
// helper. See AggregateUsage in resolver.go.
func (s *Service) AggregateUsage(usageByAgent map[string]UsageSnapshot) AggregatedUsage {
	return AggregateUsage(usageByAgent)
}

// ValidateHexSecret reports whether s is a 32-char hex string. Thin
// method wrapper so mock services can stub validation.
func (s *Service) ValidateHexSecret(secret string) bool {
	return IsValidHexSecret(secret)
}

// --- Sequence helpers ---

// NextClientID returns a fresh client ID ("client-<N>") under the
// Service's own mutex. Callers (including the server's createClient /
// adoptDiscoveredClient flows) must not hold any Service-internal lock
// when invoking this.
func (s *Service) NextClientID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientSeq++
	return newSequenceID("client", s.clientSeq)
}

// NextAssignmentID returns a fresh assignment ID ("client-assignment-<N>").
func (s *Service) NextAssignmentID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assignmentSeq++
	return newSequenceID(seqClientAssignment, s.assignmentSeq)
}

// NextDiscoveredID returns a fresh discovered-client ID ("discovered-<N>").
func (s *Service) NextDiscoveredID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discoveredSeq++
	return newSequenceID("discovered", s.discoveredSeq)
}

// RecoverSequencesFromRecords seeds the Service's monotonic counters
// from persisted record IDs so the next NextClientID / NextAssignmentID /
// NextDiscoveredID call returns a value strictly greater than any
// previously-issued ID. Safe to call multiple times.
func (s *Service) RecoverSequencesFromRecords(
	clientIDs, assignmentIDs, discoveredIDs []string,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range clientIDs {
		s.clientSeq = maxPrefixedSequence(s.clientSeq, "client", id)
	}
	for _, id := range assignmentIDs {
		s.assignmentSeq = maxPrefixedSequence(s.assignmentSeq, seqClientAssignment, id)
	}
	for _, id := range discoveredIDs {
		s.discoveredSeq = maxPrefixedSequence(s.discoveredSeq, "discovered", id)
	}
}

// --- State snapshots and mutation ---

// ListSnapshot returns a sorted copy of all non-deleted managed
// clients. Returned slice is safe to retain after the call.
func (s *Service) ListSnapshot() []Client {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Client, 0, len(s.clients))
	for _, client := range s.clients {
		if client.DeletedAt != nil {
			continue
		}
		result = append(result, client)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].CreatedAt.Equal(result[right].CreatedAt) {
			return result[left].ID < result[right].ID
		}
		return result[left].CreatedAt.Before(result[right].CreatedAt)
	})
	return result
}

// DetailSnapshot returns the managed client, its assignments (sorted
// by CreatedAt then ID), and its deployments (sorted by AgentID) for
// the given ID. Returns storage.ErrNotFound when the client is
// unknown.
func (s *Service) DetailSnapshot(clientID string) (Client, []Assignment, []Deployment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	client, ok := s.clients[clientID]
	if !ok {
		return Client{}, nil, nil, storage.ErrNotFound
	}
	assignments := append([]Assignment(nil), s.assignments[clientID]...)
	sort.Slice(assignments, func(left, right int) bool {
		if assignments[left].CreatedAt.Equal(assignments[right].CreatedAt) {
			return assignments[left].ID < assignments[right].ID
		}
		return assignments[left].CreatedAt.Before(assignments[right].CreatedAt)
	})
	deploymentsMap := s.deployments[clientID]
	deployments := make([]Deployment, 0, len(deploymentsMap))
	for _, deployment := range deploymentsMap {
		deployments = append(deployments, deployment)
	}
	sort.Slice(deployments, func(left, right int) bool {
		return deployments[left].AgentID < deployments[right].AgentID
	})
	return client, assignments, deployments, nil
}

// ReplaceInMemory updates the in-memory mirror of client state without
// touching the store. Callers that drive persistence through
// Store.Transact apply the in-memory update only after the transaction
// commits.
func (s *Service) ReplaceInMemory(client Client, assignments []Assignment, deployments []Deployment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[string(client.ID)] = client
	s.assignments[string(client.ID)] = append([]Assignment(nil), assignments...)
	nextDeployments := make(map[string]Deployment, len(deployments))
	for _, deployment := range deployments {
		nextDeployments[deployment.AgentID] = deployment
	}
	s.deployments[string(client.ID)] = nextDeployments
}

// ReplaceState persists the client + assignments + deployments to the
// Service's store (when present) and mirrors them in memory. Returns
// storage errors unchanged; under nil-store mode only the in-memory
// mirror is updated.
func (s *Service) ReplaceState(ctx context.Context, client Client, assignments []Assignment, deployments []Deployment) error {
	if s.store != nil {
		if err := PersistState(ctx, s.store, client, assignments, deployments, s.vault); err != nil {
			return err
		}
	}
	s.ReplaceInMemory(client, assignments, deployments)
	return nil
}

// FindByNameAndSecret returns the first non-deleted client matching
// both the name and the secret, case-sensitive. The boolean is false
// when no match exists.
func (s *Service) FindByNameAndSecret(name, secret string) (Client, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, client := range s.clients {
		if client.DeletedAt != nil {
			continue
		}
		if client.Name == name && client.Secret == secret {
			return client, true
		}
	}
	return Client{}, false
}

// ManagedIdentifiersForAgent returns two sets — one of client names,
// one of client secrets — for every non-deleted managed client
// currently assigned (directly or via a fleet group) to the agent.
// agentFleetGroupID is the fleet-group the agent currently belongs to
// (may be empty).
//
// This is used by the discovery reconciliation loop to decide whether a
// proxy client reported by an agent is already managed or genuinely
// new.
func (s *Service) ManagedIdentifiersForAgent(agentID, agentFleetGroupID string) (names, secrets map[string]struct{}) {
	names = make(map[string]struct{})
	secrets = make(map[string]struct{})

	s.mu.RLock()
	defer s.mu.RUnlock()

	for clientID, client := range s.clients {
		if client.DeletedAt != nil {
			continue
		}
		if !assignmentMatchesAgent(s.assignments[clientID], agentID, agentFleetGroupID) {
			continue
		}
		if client.Name != "" {
			names[client.Name] = struct{}{}
		}
		if client.Secret != "" {
			secrets[client.Secret] = struct{}{}
		}
	}
	return names, secrets
}

// ResolveIDByNameForAgent finds the panel client ID assigned to the
// given agent whose managed name matches. Returns "" when no match.
// agentFleetGroupID is the fleet-group the agent currently belongs to
// (may be empty).
func (s *Service) ResolveIDByNameForAgent(agentID, agentFleetGroupID, clientName string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return ResolveIDByName(s.clients, s.assignments, agentID, agentFleetGroupID, clientName)
}

// AggregatedUsage returns the sum-over-agents usage for a single
// client. Zero value is returned for unknown client IDs.
func (s *Service) AggregatedUsage(clientID string) AggregatedUsage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return AggregateUsage(s.usage[clientID])
}

// SeedUsage records (or overwrites) a per-(client, agent) usage
// snapshot without touching the per-agent seq tracker. Used by the
// adopt flow to seed initial counters from a discovered record.
func (s *Service) SeedUsage(clientID, agentID string, trafficBytes uint64, activeConns, activeUniqueIPs int, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.usage[clientID]; !ok {
		s.usage[clientID] = make(map[string]UsageSnapshot)
	}
	s.usage[clientID][agentID] = UsageSnapshot{
		ClientID:         ClientID(clientID),
		TrafficUsedBytes: trafficBytes,
		UniqueIPsUsed:    activeUniqueIPs,
		ActiveTCPConns:   activeConns,
		ActiveUniqueIPs:  activeUniqueIPs,
		ObservedAt:       observedAt,
	}
}

// ApplyUsageSnapshot applies an agent's live usage reports to the
// in-memory aggregator, deduplicating by per-agent Seq. Snapshots
// whose Seq is <= the stored value are discarded as duplicates/replays.
// A Seq == 1 after a non-zero stored value signals an agent restart:
// the CP records the new baseline without double-counting. See
// P2-LOG-06 / L-07.
//
// onlyKnownClients is the set of client IDs currently managed by the
// caller. Snapshots for unknown client IDs are dropped (typically
// proxy-client usage that belongs to a still-discovered record). When
// onlyKnownClients is nil every snapshot ID is accepted.
func (s *Service) ApplyUsageSnapshot(agentID string, snapshots []UsageSnapshot, onlyKnownClients map[string]struct{}) {
	if len(snapshots) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	maxSeqSeen := highestSeq(snapshots)
	if !s.acceptUsageSeqLocked(agentID, maxSeqSeen) {
		return
	}
	for _, snapshot := range snapshots {
		if !shouldRecordSnapshot(snapshot, onlyKnownClients) {
			continue
		}
		s.storeUsageSnapshotLocked(agentID, snapshot)
	}
	if maxSeqSeen != 0 {
		s.lastUsageSeq[agentID] = maxSeqSeen
	}
}

func highestSeq(snapshots []UsageSnapshot) uint64 {
	var max uint64
	for _, s := range snapshots {
		if s.Seq > max {
			max = s.Seq
		}
	}
	return max
}

// acceptUsageSeqLocked enforces the seq monotonicity contract: legacy
// agents (seq == 0) accumulate unconditionally; modern agents require
// strictly-increasing seq, except seq == 1 which signals an agent
// restart baseline (P2-LOG-06 / L-07).
func (s *Service) acceptUsageSeqLocked(agentID string, maxSeqSeen uint64) bool {
	prior := s.lastUsageSeq[agentID]
	if maxSeqSeen == 0 || prior == 0 {
		return true
	}
	return maxSeqSeen > prior || maxSeqSeen == 1
}

func shouldRecordSnapshot(snapshot UsageSnapshot, onlyKnownClients map[string]struct{}) bool {
	if snapshot.ClientID.IsZero() {
		return false
	}
	if onlyKnownClients == nil {
		return true
	}
	_, ok := onlyKnownClients[string(snapshot.ClientID)]
	return ok
}

func (s *Service) storeUsageSnapshotLocked(agentID string, snapshot UsageSnapshot) {
	byAgent, ok := s.usage[string(snapshot.ClientID)]
	if !ok {
		byAgent = make(map[string]UsageSnapshot)
		s.usage[string(snapshot.ClientID)] = byAgent
	}
	byAgent[agentID] = snapshot
}

// DropAgentUsage removes all usage rows keyed on the given agent across
// every client. Used when an agent is deregistered / forgotten.
func (s *Service) DropAgentUsage(agentID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for clientID, byAgent := range s.usage {
		delete(byAgent, agentID)
		if len(byAgent) == 0 {
			delete(s.usage, clientID)
		}
	}
	delete(s.lastUsageSeq, agentID)
}

// RestoreFromRecords loads persisted client + assignment + deployment
// records into the in-memory store. Sequence counters are recovered
// from the record IDs. Typically invoked once at server startup.
func (s *Service) RestoreFromRecords(
	clientRecords []storage.ClientRecord,
	assignmentRecords []storage.ClientAssignmentRecord,
	deploymentRecords []storage.ClientDeploymentRecord,
) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, record := range clientRecords {
		client := ClientFromRecord(record)
		s.clients[string(client.ID)] = client
		s.clientSeq = maxPrefixedSequence(s.clientSeq, "client", string(client.ID))
	}
	for _, record := range assignmentRecords {
		assignment := AssignmentFromRecord(record)
		s.assignments[string(assignment.ClientID)] = append(s.assignments[string(assignment.ClientID)], assignment)
		s.assignmentSeq = maxPrefixedSequence(s.assignmentSeq, seqClientAssignment, string(assignment.ID))
	}
	for _, record := range deploymentRecords {
		deployment := DeploymentFromRecord(record)
		byAgent, ok := s.deployments[string(deployment.ClientID)]
		if !ok {
			byAgent = make(map[string]Deployment)
			s.deployments[string(deployment.ClientID)] = byAgent
		}
		byAgent[deployment.AgentID] = deployment
	}
}

// UpdateDeployment mutates a single deployment row in-place by its
// (clientID, agentID) primary key and returns whether the row was
// found. Used by recordClientJobResult to apply job outcomes.
func (s *Service) UpdateDeployment(clientID, agentID string, mutate func(*Deployment)) (Deployment, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	byAgent, ok := s.deployments[clientID]
	if !ok {
		return Deployment{}, false
	}
	deployment, ok := byAgent[agentID]
	if !ok {
		return Deployment{}, false
	}
	mutate(&deployment)
	byAgent[agentID] = deployment
	return deployment, true
}

// WithStateLocked exposes the in-memory maps to the caller under a
// read lock for callers that need a consistent cross-map snapshot
// (e.g. the server's createClient flow reading the current client set
// during persistence-failure recovery). The callback MUST NOT block on
// any long-running operation and MUST NOT attempt to mutate the maps.
func (s *Service) WithStateLocked(fn func(clients map[string]Client, assignments map[string][]Assignment, deployments map[string]map[string]Deployment, usage map[string]map[string]UsageSnapshot)) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn(s.clients, s.assignments, s.deployments, s.usage)
}

// WithStateWriteLocked is the mutation counterpart of WithStateLocked.
// Used by server flows (adopt, discovery reconcile) that compose
// multi-map updates not otherwise expressible as a single Service
// method. The callback MUST release the maps (via the supplied
// pointers) synchronously; long-running operations are forbidden.
func (s *Service) WithStateWriteLocked(fn func(clients map[string]Client, assignments map[string][]Assignment, deployments map[string]map[string]Deployment, usage map[string]map[string]UsageSnapshot)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(s.clients, s.assignments, s.deployments, s.usage)
}

// --- Phase 6: Repository-backed mirror methods ---

// Restore loads all clients (and their assignments, deployments, usage)
// from the Repository into the in-memory mirror. Idempotent: subsequent
// calls overwrite the mirror with the latest snapshot.
//
// Phase 6.2: Service-owned mirror replaces the historical Server-owned
// mirror in clients_state.go. The legacy restoreStoredClients on Server
// delegates to this once Phase 7 lands.
func (s *Service) Restore(ctx context.Context) error {
	if s.repo == nil {
		return errors.New("clients.Service: Restore requires Repository (NewServiceV2 wiring)")
	}

	list, err := s.repo.List(ctx)
	if err != nil {
		return fmt.Errorf("clients.Service.Restore: list clients: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset mirror — Restore is a full snapshot, not incremental.
	s.mirrorClients = make(map[ClientID]Client, len(list))
	s.mirrorAssignments = make(map[ClientID][]Assignment, len(list))
	s.mirrorDeployments = make(map[ClientID]map[string]Deployment, len(list))
	s.mirrorUsage = make(map[ClientID]map[string]usageMirror)
	s.mirrorLastUsageSeq = make(map[string]uint64)

	for _, c := range list {
		s.mirrorClients[c.ID] = c

		assigns, err := s.repo.ListAssignments(ctx, c.ID)
		if err != nil {
			return fmt.Errorf("clients.Service.Restore: list assignments for %s: %w", c.ID, err)
		}
		s.mirrorAssignments[c.ID] = assigns

		deploys, err := s.repo.ListDeployments(ctx, c.ID)
		if err != nil {
			return fmt.Errorf("clients.Service.Restore: list deployments for %s: %w", c.ID, err)
		}
		dmap := make(map[string]Deployment, len(deploys))
		for _, d := range deploys {
			dmap[d.AgentID] = d
		}
		s.mirrorDeployments[c.ID] = dmap
	}

	// Usage rehydration via single ListUsage call.
	usages, err := s.repo.ListUsage(ctx)
	if err != nil {
		return fmt.Errorf("clients.Service.Restore: list usage: %w", err)
	}
	for _, u := range usages {
		if s.mirrorUsage[u.ClientID] == nil {
			s.mirrorUsage[u.ClientID] = make(map[string]usageMirror)
		}
		s.mirrorUsage[u.ClientID][u.AgentID] = usageMirror{
			ClientID:         u.ClientID,
			TrafficUsedBytes: u.TrafficUsedBytes,
			UniqueIPsUsed:    u.UniqueIPsUsed,
			ActiveTCPConns:   u.ActiveTCPConns,
			ActiveUniqueIPs:  u.ActiveUniqueIPs,
			ObservedAt:       u.ObservedAt,
			LastSeq:          u.LastSeq,
		}
		if u.LastSeq > s.mirrorLastUsageSeq[u.AgentID] {
			s.mirrorLastUsageSeq[u.AgentID] = u.LastSeq
		}
	}

	return nil
}

// Get returns the cached Client by ID. The mirror is populated by
// Restore; after a Save/SaveState/AdoptDiscovered the mirror is updated
// atomically. Returns ErrNotFound if the ID is unknown.
//
// No name collision with existing Service methods: legacy read paths use
// DetailSnapshot (single-client) and ListSnapshot (all clients).
func (s *Service) Get(ctx context.Context, id ClientID) (Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.mirrorClients[id]
	if !ok {
		return Client{}, ErrNotFound
	}
	return c, nil
}

// List returns all cached Clients (snapshot of the mirror at call
// time). Order is unspecified — callers that need ordering must sort.
//
// No name collision with existing Service methods: the legacy path uses
// ListSnapshot which filters deleted clients and sorts by CreatedAt.
func (s *Service) List(ctx context.Context) ([]Client, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Client, 0, len(s.mirrorClients))
	for _, c := range s.mirrorClients {
		out = append(out, c)
	}
	return out, nil
}

// --- Phase 6.4–6.7: UoW-backed mutation methods ---

// encryptSecret seals the plaintext secret via the vault's
// DomainClientSecret key. A nil or disabled vault is a no-op.
// An empty plaintext is returned unchanged.
func (s *Service) encryptSecret(plaintext string) (string, error) {
	if s.vault == nil || !s.vault.Enabled() || plaintext == "" {
		return plaintext, nil
	}
	ct, err := s.vault.Encrypt(secretvault.DomainClientSecret, plaintext)
	if err != nil {
		return "", fmt.Errorf("encryptSecret: %w", err)
	}
	return ct, nil
}

// decryptSecret reverses encryptSecret. Plaintext (non-prefixed) values
// are returned unchanged for backwards compatibility.
func (s *Service) decryptSecret(ciphertext string) (string, error) {
	if s.vault == nil || !s.vault.Enabled() || ciphertext == "" {
		return ciphertext, nil
	}
	pt, err := s.vault.Decrypt(secretvault.DomainClientSecret, ciphertext)
	if err != nil {
		return "", fmt.Errorf("decryptSecret: %w", err)
	}
	return pt, nil
}

// Save persists the client (encrypted) via the Repository inside a UoW
// transaction, then updates the in-memory mirror with the plaintext
// Client. The Repository always stores ciphertext for Secret.
func (s *Service) Save(ctx context.Context, c Client) error {
	if s.uow == nil || s.repo == nil {
		return errors.New("clients.Service: Save requires UoW + Repository (NewServiceV2)")
	}
	encryptedSecret, err := s.encryptSecret(c.Secret)
	if err != nil {
		return fmt.Errorf("clients.Service.Save: encrypt secret: %w", err)
	}
	toStore := c
	toStore.Secret = encryptedSecret

	if err := s.uow.Do(ctx, func(rs ClientsRepoSet) error {
		return rs.Clients().Save(ctx, toStore)
	}); err != nil {
		return err
	}

	// Mirror holds plaintext (handler-facing).
	s.mu.Lock()
	s.mirrorClients[c.ID] = c
	s.mu.Unlock()
	return nil
}

// SaveState atomically persists client + assignments + deployments in
// one UoW transaction, then updates the in-memory mirror. On any Tx
// error the mirror is left unchanged.
func (s *Service) SaveState(ctx context.Context, c Client, assignments []Assignment, deployments []Deployment) error {
	if s.uow == nil || s.repo == nil {
		return errors.New("clients.Service: SaveState requires UoW + Repository (NewServiceV2)")
	}
	encryptedSecret, err := s.encryptSecret(c.Secret)
	if err != nil {
		return fmt.Errorf("clients.Service.SaveState: encrypt: %w", err)
	}
	toStore := c
	toStore.Secret = encryptedSecret

	if err := s.uow.Do(ctx, func(rs ClientsRepoSet) error {
		if err := rs.Clients().Save(ctx, toStore); err != nil {
			return err
		}
		if err := rs.Clients().SaveAssignments(ctx, c.ID, assignments); err != nil {
			return err
		}
		if err := rs.Clients().SaveDeployments(ctx, c.ID, deployments); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	s.mu.Lock()
	s.mirrorClients[c.ID] = c
	s.mirrorAssignments[c.ID] = append([]Assignment(nil), assignments...)
	dmap := make(map[string]Deployment, len(deployments))
	for _, d := range deployments {
		dmap[d.AgentID] = d
	}
	s.mirrorDeployments[c.ID] = dmap
	s.mu.Unlock()
	return nil
}

// AdoptInput carries the parameters for Service.AdoptDiscovered.
type AdoptInput struct {
	DiscoveredID discovered.DiscoveredID
	// Secret is the plaintext MTProto secret for the client being adopted.
	// The discovered.DiscoveredClient domain type does not carry the
	// secret; callers that have it (e.g. the gRPC handler) supply it here.
	// Empty is valid — the repository row will store an empty ciphertext.
	Secret     string
	ActorID    string
	ObservedAt time.Time
}

// AdoptDiscovered promotes a discovered client to a managed client in
// one cross-domain UoW transaction: reads the discovered record,
// creates the managed client, flips the discovered status to Adopted,
// and appends an audit event. Mirror is updated on success only.
func (s *Service) AdoptDiscovered(ctx context.Context, in AdoptInput) (Client, error) {
	if s.uow == nil || s.repo == nil {
		return Client{}, errors.New("clients.Service: AdoptDiscovered requires UoW + Repository (NewServiceV2)")
	}
	if in.ObservedAt.IsZero() {
		in.ObservedAt = s.now().UTC()
	}
	var adopted Client

	err := s.uow.Do(ctx, func(rs ClientsRepoSet) error {
		dc, err := rs.Discovered().Get(ctx, in.DiscoveredID)
		if err != nil {
			return err
		}
		if dc.Status != discovered.StatusPending {
			return fmt.Errorf("clients.Service.AdoptDiscovered: cannot adopt %s in status %s", dc.ID, dc.Status)
		}

		c := buildClientFromDiscovered(dc, in.Secret, s.NextClientID(), in.ObservedAt)

		encryptedSecret, err := s.encryptSecret(c.Secret)
		if err != nil {
			return fmt.Errorf("clients.Service.AdoptDiscovered: encrypt: %w", err)
		}
		toStore := c
		toStore.Secret = encryptedSecret

		if err := rs.Clients().Save(ctx, toStore); err != nil {
			return err
		}
		if err := rs.Discovered().UpdateStatus(ctx, dc.ID, discovered.StatusAdopted, in.ObservedAt); err != nil {
			return err
		}
		if err := rs.Audit().Append(ctx, audit.Event{
			ActorID:   in.ActorID,
			Action:    "client.adopt",
			TargetID:  string(c.ID),
			CreatedAt: in.ObservedAt,
			Details:   map[string]any{"discovered_id": string(dc.ID)},
		}); err != nil {
			return err
		}
		adopted = c
		return nil
	})
	if err != nil {
		return Client{}, err
	}

	s.mu.Lock()
	s.mirrorClients[adopted.ID] = adopted
	s.mu.Unlock()
	return adopted, nil
}

// buildClientFromDiscovered constructs a managed Client from a
// DiscoveredClient. Pure function — no I/O, no encryption.
// ID generation uses the same sequential "client-N" scheme as
// Service.NextClientID (legacy server.buildAdoptedClientState).
func buildClientFromDiscovered(dc discovered.DiscoveredClient, secret, id string, observedAt time.Time) Client {
	return Client{
		ID:        ClientID(id),
		Name:      dc.ClientName,
		Secret:    secret,
		Enabled:   true,
		CreatedAt: observedAt,
		UpdatedAt: observedAt,
	}
}

// Delete removes the client from the Repository via a UoW transaction
// and evicts all mirror entries for that client ID on success.
//
// No name collision: the legacy Service has no Delete method; only the
// fakeRepo in tests had a stub.
func (s *Service) Delete(ctx context.Context, id ClientID) error {
	if s.uow == nil || s.repo == nil {
		return errors.New("clients.Service: Delete requires UoW + Repository (NewServiceV2)")
	}
	if err := s.uow.Do(ctx, func(rs ClientsRepoSet) error {
		return rs.Clients().Delete(ctx, id)
	}); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.mirrorClients, id)
	delete(s.mirrorAssignments, id)
	delete(s.mirrorDeployments, id)
	delete(s.mirrorUsage, id)
	s.mu.Unlock()
	return nil
}

// --- Phase 6.8: UpsertUsage / UpsertUsageBulk ---

// UpsertUsage persists a single (client, agent) usage record and
// updates the in-memory mirror. Bypasses UoW — usage updates are not
// part of any cross-domain transaction.
func (s *Service) UpsertUsage(ctx context.Context, u Usage) error {
	if s.repo == nil {
		return errors.New("clients.Service: UpsertUsage requires Repository (NewServiceV2)")
	}
	if err := s.repo.UpsertUsage(ctx, u); err != nil {
		return err
	}
	s.applyUsageMirror(u)
	return nil
}

// UpsertUsageBulk is the hot-path bulk variant called from agent-flow
// telemetry tick. 500x50 batches flush in a single Repository call.
// Empty slice is a no-op.
//
// No name collision: the legacy Service has no UpsertUsage(Bulk) method.
func (s *Service) UpsertUsageBulk(ctx context.Context, batch []Usage) error {
	if len(batch) == 0 {
		return nil
	}
	if s.repo == nil {
		return errors.New("clients.Service: UpsertUsageBulk requires Repository (NewServiceV2)")
	}
	if err := s.repo.UpsertUsageBulk(ctx, batch); err != nil {
		return err
	}
	s.mu.Lock()
	for _, u := range batch {
		s.applyUsageMirrorLocked(u)
	}
	s.mu.Unlock()
	return nil
}

// applyUsageMirror acquires the write lock and delegates to
// applyUsageMirrorLocked.
func (s *Service) applyUsageMirror(u Usage) {
	s.mu.Lock()
	s.applyUsageMirrorLocked(u)
	s.mu.Unlock()
}

// PersistDeployment writes a single deployment record to the legacy
// storage.Store via the client service. This is used by the server's
// recordClientJobResultWithContext during Phase 7 to eliminate the
// direct s.store.PutClientDeployment callsite. When no store is wired
// (nil), the call is a no-op (in-memory mode for tests).
//
// Phase 8 will replace this with a Repository-backed path once the
// Repository.SaveDeployments API is enriched to support upsert-one.
func (s *Service) PersistDeployment(ctx context.Context, d Deployment) error {
	if s.store == nil {
		return nil
	}
	return s.store.PutClientDeployment(ctx, DeploymentToRecord(d))
}

// --- Phase 7: server-legacy bridge ---

// MirrorUsageEntry is the per-(client, agent) usage value returned by
// MirrorSnapshot. It mirrors usageMirror but is exported so the server
// package can read it to sync its own legacy maps during Phase 7.
type MirrorUsageEntry struct {
	ClientID         ClientID
	TrafficUsedBytes uint64
	UniqueIPsUsed    int
	ActiveTCPConns   int
	ActiveUniqueIPs  int
	ObservedAt       time.Time
	LastSeq          uint64
}

// MirrorState is the full snapshot of the V2 in-memory mirror, returned by
// MirrorSnapshot. Callers must treat the maps as read-only; they are copies.
type MirrorState struct {
	Clients      map[ClientID]Client
	Assignments  map[ClientID][]Assignment
	Deployments  map[ClientID]map[string]Deployment
	Usage        map[ClientID]map[string]MirrorUsageEntry
	LastUsageSeq map[string]uint64 // per-agent
}

// MirrorSnapshot returns a deep copy of the current V2 mirror state.
// Used by the server's restoreStoredClients bridge during Phase 7 so
// the server's legacy maps are synced from the domain service rather
// than queried directly from storage.Store. Safe to call from any
// goroutine; acquires the read lock internally.
func (s *Service) MirrorSnapshot() MirrorState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clients := make(map[ClientID]Client, len(s.mirrorClients))
	for k, v := range s.mirrorClients {
		clients[k] = v
	}

	assignments := make(map[ClientID][]Assignment, len(s.mirrorAssignments))
	for k, v := range s.mirrorAssignments {
		cp := make([]Assignment, len(v))
		copy(cp, v)
		assignments[k] = cp
	}

	deployments := make(map[ClientID]map[string]Deployment, len(s.mirrorDeployments))
	for k, byAgent := range s.mirrorDeployments {
		cp := make(map[string]Deployment, len(byAgent))
		for agentID, d := range byAgent {
			cp[agentID] = d
		}
		deployments[k] = cp
	}

	usage := make(map[ClientID]map[string]MirrorUsageEntry, len(s.mirrorUsage))
	for k, byAgent := range s.mirrorUsage {
		cp := make(map[string]MirrorUsageEntry, len(byAgent))
		for agentID, u := range byAgent {
			cp[agentID] = MirrorUsageEntry{
				ClientID:         u.ClientID,
				TrafficUsedBytes: u.TrafficUsedBytes,
				UniqueIPsUsed:    u.UniqueIPsUsed,
				ActiveTCPConns:   u.ActiveTCPConns,
				ActiveUniqueIPs:  u.ActiveUniqueIPs,
				ObservedAt:       u.ObservedAt,
				LastSeq:          u.LastSeq,
			}
		}
		usage[k] = cp
	}

	lastUsageSeq := make(map[string]uint64, len(s.mirrorLastUsageSeq))
	for agentID, seq := range s.mirrorLastUsageSeq {
		lastUsageSeq[agentID] = seq
	}

	return MirrorState{
		Clients:      clients,
		Assignments:  assignments,
		Deployments:  deployments,
		Usage:        usage,
		LastUsageSeq: lastUsageSeq,
	}
}

// HasRepo reports whether the service was wired with a Repository
// (i.e. constructed via NewServiceV2). Used by the server to decide
// whether to delegate persistence operations to the service.
func (s *Service) HasRepo() bool {
	return s.repo != nil
}

// applyUsageMirrorLocked updates the mirror maps. Must be called with
// s.mu held for writing.
func (s *Service) applyUsageMirrorLocked(u Usage) {
	if s.mirrorUsage[u.ClientID] == nil {
		s.mirrorUsage[u.ClientID] = make(map[string]usageMirror)
	}
	s.mirrorUsage[u.ClientID][u.AgentID] = usageMirror{
		ClientID:         u.ClientID,
		TrafficUsedBytes: u.TrafficUsedBytes,
		UniqueIPsUsed:    u.UniqueIPsUsed,
		ActiveTCPConns:   u.ActiveTCPConns,
		ActiveUniqueIPs:  u.ActiveUniqueIPs,
		ObservedAt:       u.ObservedAt,
		LastSeq:          u.LastSeq,
	}
	if u.LastSeq > s.mirrorLastUsageSeq[u.AgentID] {
		s.mirrorLastUsageSeq[u.AgentID] = u.LastSeq
	}
}
