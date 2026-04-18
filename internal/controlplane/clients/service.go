package clients

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

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
// Lock discipline
//
// Service.mu protects the four in-memory maps (clients, assignments,
// deployments, usage) and the client/assignment sequence counters. The
// caller-supplied agent-topology snapshot (AgentTopology) is produced
// by the server under its own mu lock, so Service never holds mu while
// asking the server for topology. This preserves the documented lock
// ordering (Server.mu -> Service.mu -> Server.metricsAuditMu).
//
// Nil-store mode
//
// When the optional Store dependency is nil, Service acts as a pure
// in-memory store — no persistence is attempted. This matches how the
// server is constructed in unit tests that set Options.Store = nil.
type Service struct {
	store storage.Store
	now   func() time.Time

	mu            sync.RWMutex
	clients       map[string]Client
	assignments   map[string][]Assignment
	deployments   map[string]map[string]Deployment
	usage         map[string]map[string]UsageSnapshot
	lastUsageSeq  map[string]uint64
	clientSeq     uint64
	assignmentSeq uint64
	discoveredSeq uint64
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
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:        store,
		now:          now,
		clients:      make(map[string]Client),
		assignments:  make(map[string][]Assignment),
		deployments:  make(map[string]map[string]Deployment),
		usage:        make(map[string]map[string]UsageSnapshot),
		lastUsageSeq: make(map[string]uint64),
	}
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
	return newSequenceID("client-assignment", s.assignmentSeq)
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
	clientIDs []string,
	assignmentIDs []string,
	discoveredIDs []string,
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range clientIDs {
		s.clientSeq = maxPrefixedSequence(s.clientSeq, "client", id)
	}
	for _, id := range assignmentIDs {
		s.assignmentSeq = maxPrefixedSequence(s.assignmentSeq, "client-assignment", id)
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
	sort.Slice(result, func(left int, right int) bool {
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
	sort.Slice(assignments, func(left int, right int) bool {
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
	sort.Slice(deployments, func(left int, right int) bool {
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
	s.clients[client.ID] = client
	s.assignments[client.ID] = append([]Assignment(nil), assignments...)
	nextDeployments := make(map[string]Deployment, len(deployments))
	for _, deployment := range deployments {
		nextDeployments[deployment.AgentID] = deployment
	}
	s.deployments[client.ID] = nextDeployments
}

// ReplaceState persists the client + assignments + deployments to the
// Service's store (when present) and mirrors them in memory. Returns
// storage errors unchanged; under nil-store mode only the in-memory
// mirror is updated.
func (s *Service) ReplaceState(ctx context.Context, client Client, assignments []Assignment, deployments []Deployment) error {
	if s.store != nil {
		if err := PersistState(ctx, s.store, client, assignments, deployments); err != nil {
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
		for _, assignment := range s.assignments[clientID] {
			matches := false
			switch assignment.TargetType {
			case TargetTypeAgent:
				if assignment.AgentID == agentID {
					matches = true
				}
			case TargetTypeFleetGroup:
				if agentFleetGroupID != "" && assignment.FleetGroupID == agentFleetGroupID {
					matches = true
				}
			}
			if matches {
				if client.Name != "" {
					names[client.Name] = struct{}{}
				}
				if client.Secret != "" {
					secrets[client.Secret] = struct{}{}
				}
				break
			}
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
		ClientID:         clientID,
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

	var maxSeqSeen uint64
	for _, snapshot := range snapshots {
		if snapshot.Seq > maxSeqSeen {
			maxSeqSeen = snapshot.Seq
		}
	}
	prior := s.lastUsageSeq[agentID]
	// Legacy agent (seq == 0): accumulate unconditionally. Else: require
	// strictly increasing seq unless seq == 1 (agent restart baseline).
	if maxSeqSeen != 0 && prior != 0 {
		if maxSeqSeen <= prior && maxSeqSeen != 1 {
			return
		}
	}
	for _, snapshot := range snapshots {
		if snapshot.ClientID == "" {
			continue
		}
		if onlyKnownClients != nil {
			if _, ok := onlyKnownClients[snapshot.ClientID]; !ok {
				continue
			}
		}
		byAgent, ok := s.usage[snapshot.ClientID]
		if !ok {
			byAgent = make(map[string]UsageSnapshot)
			s.usage[snapshot.ClientID] = byAgent
		}
		byAgent[agentID] = snapshot
	}
	if maxSeqSeen != 0 {
		s.lastUsageSeq[agentID] = maxSeqSeen
	}
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
		s.clients[client.ID] = client
		s.clientSeq = maxPrefixedSequence(s.clientSeq, "client", client.ID)
	}
	for _, record := range assignmentRecords {
		assignment := AssignmentFromRecord(record)
		s.assignments[assignment.ClientID] = append(s.assignments[assignment.ClientID], assignment)
		s.assignmentSeq = maxPrefixedSequence(s.assignmentSeq, "client-assignment", assignment.ID)
	}
	for _, record := range deploymentRecords {
		deployment := DeploymentFromRecord(record)
		byAgent, ok := s.deployments[deployment.ClientID]
		if !ok {
			byAgent = make(map[string]Deployment)
			s.deployments[deployment.ClientID] = byAgent
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
