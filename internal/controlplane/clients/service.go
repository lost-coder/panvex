package clients

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/secretvault"
)

const seqClientAssignment = "client-assignment"

// usageMirror is the in-Service snapshot of (client, agent) usage.
// Distinct from clients.Usage (Repository row type) and
// clients.UsageSnapshot (the handler-facing value type) to avoid a name
// collision.
type usageMirror struct {
	ClientID           ClientID
	TrafficUsedBytes   uint64
	UniqueIPsUsed      int
	ActiveTCPConns     int
	ActiveUniqueIPs    int
	QuotaUsedBytes     uint64
	QuotaLastResetUnix uint64
	ObservedAt         time.Time
	LastSeq            uint64
}

// snapshot projects the in-memory usageMirror row onto the handler-facing
// UsageSnapshot value type. Seq carries the mirror's LastSeq.
func (u usageMirror) snapshot() UsageSnapshot {
	return UsageSnapshot{
		ClientID:           u.ClientID,
		TrafficUsedBytes:   u.TrafficUsedBytes,
		UniqueIPsUsed:      u.UniqueIPsUsed,
		ActiveTCPConns:     u.ActiveTCPConns,
		ActiveUniqueIPs:    u.ActiveUniqueIPs,
		QuotaUsedBytes:     u.QuotaUsedBytes,
		QuotaLastResetUnix: u.QuotaLastResetUnix,
		ObservedAt:         u.ObservedAt,
		Seq:                u.LastSeq,
	}
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
}

// ServiceUoW is the unit-of-work interface that clients.Service
// accepts. It is structurally equivalent to uow.UnitOfWork but scoped
// to the two repositories Service needs (Clients + Discovered).
// Callers provide an adapter that delegates to the real uow.UnitOfWork.
type ServiceUoW interface {
	Do(ctx context.Context, fn func(rs ClientsRepoSet) error) error
}

// Service is the orchestration entry point for managed clients. It owns
// the in-memory mirror of clients, assignments, deployments, and live
// usage snapshots — backed by a clients.Repository + UnitOfWork — and
// provides the read/write surface that the control-plane HTTP and gRPC
// handlers consume.
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
// Service.mu protects the in-memory mirror maps (mirrorClients,
// mirrorAssignments, mirrorDeployments, mirrorUsage, mirrorLastUsageSeq)
// and the client/assignment/discovered sequence counters. The
// caller-supplied agent-topology snapshot (AgentTopology) is produced
// by the server under its own mu lock, so Service never holds mu while
// asking the server for topology. This preserves the documented lock
// ordering (Server.mu -> Service.mu -> Server.metricsAuditMu).
type Service struct {
	now   func() time.Time
	vault *secretvault.Vault

	mu            sync.RWMutex
	clientSeq     uint64
	assignmentSeq uint64
	discoveredSeq uint64

	// Repository + UoW + in-memory mirror: the only client-state path.
	repo           Repository
	discoveredRepo discovered.Repository
	uow            ServiceUoW

	mirrorClients      map[ClientID]Client
	mirrorAssignments  map[ClientID][]Assignment
	mirrorDeployments  map[ClientID]map[string]Deployment // outer=ClientID, inner=AgentID
	mirrorUsage        map[ClientID]map[string]usageMirror
	mirrorLastUsageSeq map[string]uint64 // per-agent
}

// ServiceConfig carries the dependencies for NewServiceV2: a
// clients.Repository, a discovered.Repository, a UoW, and the vault.
type ServiceConfig struct {
	Repo           Repository
	DiscoveredRepo discovered.Repository
	UoW            ServiceUoW
	Vault          *secretvault.Vault
	Now            func() time.Time
}

// NewServiceV2 constructs a Service with the full dependency set: a
// clients.Repository, a discovered.Repository, and a UoW. The in-memory
// mirror maps are pre-allocated; call Service.Restore to populate them
// from the Repository.
//
// (C3 renames this to NewService once the legacy constructors are gone.)
func NewServiceV2(cfg ServiceConfig) *Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		now:   now,
		vault: cfg.Vault,

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

// ReplaceState atomically persists client + assignments + deployments via
// the Repository and mirrors them in memory. Thin alias over SaveState
// retained for the server-side call sites.
func (s *Service) ReplaceState(ctx context.Context, client Client, assignments []Assignment, deployments []Deployment) error {
	return s.SaveState(ctx, client, assignments, deployments)
}

// --- Repository-backed mirror methods ---

// Restore loads all clients (and their assignments, deployments, usage)
// from the Repository into the in-memory mirror. Idempotent: subsequent
// calls overwrite the mirror with the latest snapshot.
//
// Phase 6.2: Service-owned mirror replaces the historical Server-owned
// mirror in clients_state.go. The legacy restoreStoredClients on Server
// delegates to this once Phase 7 lands.
func (s *Service) Restore(ctx context.Context) error {
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
		// The Repository always stores the secret encrypted
		// (secret_ciphertext / PVS2:). Save/SaveState keep the plaintext
		// in the mirror, so the handler-facing mirror must hold plaintext
		// here too — otherwise every client-apply job built after a
		// restart ships the ciphertext to telemt, which rejects it
		// ("secret must be exactly 32 hex characters"). decryptSecret is a
		// no-op for plaintext/dev installs and reverses encryptSecret.
		// decryptSecretFully also heals any row a pre-fix save double-wrapped.
		plaintext, err := s.decryptSecretFully(c.Secret)
		if err != nil {
			return fmt.Errorf("clients.Service.Restore: decrypt secret for %s: %w", c.ID, err)
		}
		c.Secret = plaintext
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
			ClientID:           u.ClientID,
			TrafficUsedBytes:   u.TrafficUsedBytes,
			UniqueIPsUsed:      u.UniqueIPsUsed,
			ActiveTCPConns:     u.ActiveTCPConns,
			ActiveUniqueIPs:    u.ActiveUniqueIPs,
			QuotaUsedBytes:     u.QuotaUsedBytes,
			QuotaLastResetUnix: u.QuotaLastResetUnix,
			ObservedAt:         u.ObservedAt,
			LastSeq:            u.LastSeq,
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

// EncryptSecret seals the plaintext secret using the vault's
// DomainClientSecret key. Delegates to encryptSecret. Exposed as a
// public method so server-package code (e.g. persistAdoptedClient) can
// encrypt at the correct boundary without importing secretvault directly.
func (s *Service) EncryptSecret(plaintext string) (string, error) {
	return s.encryptSecret(plaintext)
}

// encryptSecret seals the plaintext secret via the vault's
// DomainClientSecret key. A nil or disabled vault is a no-op.
// An empty plaintext is returned unchanged.
func (s *Service) encryptSecret(plaintext string) (string, error) {
	if s.vault == nil || !s.vault.Enabled() || plaintext == "" {
		return plaintext, nil
	}
	// Idempotency guard: never re-encrypt a value that already carries a
	// vault prefix. A valid 32-hex client secret can never look encrypted,
	// so a prefixed input means a ciphertext leaked into a save path (e.g.
	// a pre-decrypt-on-load mirror that still held ciphertext). Encrypting
	// it again would double-wrap the secret and corrupt the row — exactly
	// the failure that shipped PVS2: ciphertext to telemt.
	if secretvault.IsEncrypted(plaintext) {
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

// decryptSecretFully reverses encryptSecret and additionally heals rows
// that an earlier bug double-wrapped: a save that ran while the in-memory
// mirror still held ciphertext re-encrypted it (PVS2:PVS2:…). A correct
// secret decrypts to a plaintext that no longer carries a vault prefix,
// so a single-encrypted value stops after one pass; a double-wrapped one
// needs two. Bounded so genuinely corrupt data fails loudly via the
// final decrypt error rather than spinning.
func (s *Service) decryptSecretFully(value string) (string, error) {
	const maxLayers = 4
	out := value
	for range maxLayers {
		if s.vault == nil || !s.vault.Enabled() || !secretvault.IsEncrypted(out) {
			return out, nil
		}
		pt, err := s.decryptSecret(out)
		if err != nil {
			return "", err
		}
		if pt == out {
			// Defensive: decrypt made no progress; avoid an infinite loop.
			return out, nil
		}
		out = pt
	}
	return out, nil
}

// Save persists the client (encrypted) via the Repository inside a UoW
// transaction, then updates the in-memory mirror with the plaintext
// Client. The Repository always stores ciphertext for Secret.
func (s *Service) Save(ctx context.Context, c Client) error {
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
// creates the managed client, and flips the discovered status to
// Adopted. Mirror is updated on success only.
//
// Audit is intentionally NOT written inside this transaction. Audit is a
// cross-cutting concern owned by the single serialized server-side
// hash-chainer (appendAudit*); writing audit through the UoW bypassed
// that chainer and broke the tamper-evident chain (C-1b). The caller is
// responsible for emitting the "clients.adopted" audit event via the
// server append path after this returns — exactly as the live
// discovery-adopt HTTP handler already does.
func (s *Service) AdoptDiscovered(ctx context.Context, in AdoptInput) (Client, error) {
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
	// Update the in-memory mirror (the live accumulator) unconditionally,
	// BEFORE attempting the persist. Client usage totals are cumulative
	// absolutes and the seq cursor advances unconditionally upstream
	// (server.shouldApplyClientUsageDelta -> SetMirrorLastUsageSeq). If the
	// mirror total were gated on DB success, a failed persist would leave the
	// cursor advanced but the total stale, permanently dropping this delta's
	// bytes from the running total (the cumulative DB self-heal relies on the
	// in-memory total being correct). The DB error is still propagated below
	// so callers can alert (client_usage_persist_failed); the next successful
	// in-order snapshot carries the new absolute and self-heals the DB row.
	s.applyUsageMirror(u)
	return s.repo.UpsertUsage(ctx, u)
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
	// Update the in-memory mirror (the live accumulator) unconditionally,
	// BEFORE attempting the persist. See UpsertUsage for the rationale: usage
	// totals are cumulative absolutes and the seq cursor advances upstream
	// regardless of DB success, so gating the mirror total on persist success
	// would permanently drop a failed delta's bytes. The DB error is still
	// propagated so callers can alert (client_usage_persist_failed); the next
	// successful snapshot's absolute self-heals the DB row.
	s.mu.Lock()
	for _, u := range batch {
		s.applyUsageMirrorLocked(u)
	}
	s.mu.Unlock()
	return s.repo.UpsertUsageBulk(ctx, batch)
}

// applyUsageMirror acquires the write lock and delegates to
// applyUsageMirrorLocked.
func (s *Service) applyUsageMirror(u Usage) {
	s.mu.Lock()
	s.applyUsageMirrorLocked(u)
	s.mu.Unlock()
}

// PersistDeployment writes a single deployment record via the
// Repository and updates the in-memory mirror.
func (s *Service) PersistDeployment(ctx context.Context, d Deployment) error {
	if err := s.repo.PutDeployment(ctx, d); err != nil {
		return err
	}
	s.mu.Lock()
	if s.mirrorDeployments[d.ClientID] == nil {
		s.mirrorDeployments[d.ClientID] = make(map[string]Deployment)
	}
	s.mirrorDeployments[d.ClientID][d.AgentID] = d
	s.mu.Unlock()
	return nil
}

// --- Phase 7: server-legacy bridge ---

// MirrorUsageEntry is the per-(client, agent) usage value returned by
// MirrorSnapshot. It mirrors usageMirror but is exported so the server
// package can read it to sync its own legacy maps during Phase 7.
type MirrorUsageEntry struct {
	ClientID           ClientID
	TrafficUsedBytes   uint64
	UniqueIPsUsed      int
	ActiveTCPConns     int
	ActiveUniqueIPs    int
	QuotaUsedBytes     uint64
	QuotaLastResetUnix uint64
	ObservedAt         time.Time
	LastSeq            uint64
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
				ClientID:           u.ClientID,
				TrafficUsedBytes:   u.TrafficUsedBytes,
				UniqueIPsUsed:      u.UniqueIPsUsed,
				ActiveTCPConns:     u.ActiveTCPConns,
				ActiveUniqueIPs:    u.ActiveUniqueIPs,
				QuotaUsedBytes:     u.QuotaUsedBytes,
				QuotaLastResetUnix: u.QuotaLastResetUnix,
				ObservedAt:         u.ObservedAt,
				LastSeq:            u.LastSeq,
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

// ZeroLiveGaugesForAgent zeros the live connection/IP gauges in the
// mirror for every client this agent owns a usage row for but did NOT
// report in the current snapshot. Accumulated traffic and the persisted
// quota fields are preserved — only the instantaneous gauges are reset.
//
// This is the mirror-side counterpart of the server's
// zeroLiveGaugesForUntouchedClients. Like that path it is mirror-only:
// the zeroed gauges are derived per-tick and are never persisted, so no
// Repository write is performed. seen is the set of client IDs the agent
// included in the snapshot just applied. No-op when the service was not
// wired with a Repository (mirror unused).
func (s *Service) ZeroLiveGaugesForAgent(agentID string, seen map[string]struct{}) {
	if s.repo == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for clientID, byAgent := range s.mirrorUsage {
		if _, ok := seen[string(clientID)]; ok {
			continue
		}
		entry, ok := byAgent[agentID]
		if !ok {
			continue
		}
		entry.ActiveTCPConns = 0
		entry.ActiveUniqueIPs = 0
		byAgent[agentID] = entry
	}
}

// DropAgentUsageMirror removes every (client, agent) usage row owned by
// the given agent from the mirror, prunes any inner maps left empty, and
// clears the agent's per-agent seq cursor. Mirror-side counterpart of the
// server's purgeAgentInMemory usage cleanup (used when an agent is
// deregistered / forgotten).
//
// Mirror-only by design: the DB client_usage rows are intentionally NOT
// deleted here. The deregister flow (server.persistAgentDeregister) removes
// only the agent's instances, the agent row, and a revocation record — it
// never touches client_usage. This preserves the pre-B3 behaviour, where
// deregistration purged the in-memory maps but left the persisted usage rows
// in place. If reaping orphaned client_usage rows is ever wanted it is a
// separate concern (a Repository-level delete in the deregister transaction),
// not a side effect of this mirror cleanup. No-op without a Repository.
func (s *Service) DropAgentUsageMirror(agentID string) {
	if s.repo == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for clientID, byAgent := range s.mirrorUsage {
		if _, ok := byAgent[agentID]; !ok {
			continue
		}
		delete(byAgent, agentID)
		if len(byAgent) == 0 {
			delete(s.mirrorUsage, clientID)
		}
	}
	delete(s.mirrorLastUsageSeq, agentID)
}

// SeedUsageMirror writes a usage row into the mirror for the given
// (client, agent) pair, but only when no row already exists. Mirror-only
// (no persistence): this backs the restore-time discovered-client usage
// fallback, which historically seeded the server map from
// discovered_clients.total_octets without write-through. No-op without a
// Repository.
func (s *Service) SeedUsageMirror(clientID, agentID string, trafficBytes uint64, activeConns, activeUniqueIPs int, observedAt time.Time) {
	if s.repo == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cid := ClientID(clientID)
	byAgent, ok := s.mirrorUsage[cid]
	if !ok {
		byAgent = make(map[string]usageMirror)
		s.mirrorUsage[cid] = byAgent
	}
	if _, exists := byAgent[agentID]; exists {
		return
	}
	byAgent[agentID] = usageMirror{
		ClientID:         cid,
		TrafficUsedBytes: trafficBytes,
		UniqueIPsUsed:    activeUniqueIPs,
		ActiveTCPConns:   activeConns,
		ActiveUniqueIPs:  activeUniqueIPs,
		ObservedAt:       observedAt,
	}
}

// applyUsageMirrorLocked updates the mirror maps. Must be called with
// s.mu held for writing.
func (s *Service) applyUsageMirrorLocked(u Usage) {
	if s.mirrorUsage[u.ClientID] == nil {
		s.mirrorUsage[u.ClientID] = make(map[string]usageMirror)
	}
	s.mirrorUsage[u.ClientID][u.AgentID] = usageMirror{
		ClientID:           u.ClientID,
		TrafficUsedBytes:   u.TrafficUsedBytes,
		UniqueIPsUsed:      u.UniqueIPsUsed,
		ActiveTCPConns:     u.ActiveTCPConns,
		ActiveUniqueIPs:    u.ActiveUniqueIPs,
		QuotaUsedBytes:     u.QuotaUsedBytes,
		QuotaLastResetUnix: u.QuotaLastResetUnix,
		ObservedAt:         u.ObservedAt,
		LastSeq:            u.LastSeq,
	}
	if u.LastSeq > s.mirrorLastUsageSeq[u.AgentID] {
		s.mirrorLastUsageSeq[u.AgentID] = u.LastSeq
	}
}

// --- D1 (C1): mirror-backed reads/mutations for the server package ---
//
// These replace the Server-owned client maps (s.clients / s.clientUsage /
// s.clientDeployments / s.clientAssignments) that C1 removed. Each reads or
// mutates the V2 mirror under s.mu, so the mirror is the single owner of
// client/usage/deployment state. All are no-ops / zero-values when the
// service was not wired with a Repository (mirror unused).

// AgentTotalTrafficMirror sums TrafficUsedBytes across every client this
// agent has a usage row for in the mirror. Mirror-side counterpart of the
// server's agentTotalTrafficLocked; used by the telemetry summaries.
func (s *Service) AgentTotalTrafficMirror(agentID string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total uint64
	for _, byAgent := range s.mirrorUsage {
		if u, ok := byAgent[agentID]; ok {
			total += u.TrafficUsedBytes
		}
	}
	return total
}

// UsageByAgentMirror returns a defensive copy of the per-(client, agent)
// usage map for one client, projected to UsageSnapshot. Mirror-side
// counterpart of the server's clientUsageByAgent.
func (s *Service) UsageByAgentMirror(clientID string) map[string]UsageSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byAgent := s.mirrorUsage[ClientID(clientID)]
	out := make(map[string]UsageSnapshot, len(byAgent))
	for agentID, u := range byAgent {
		out[agentID] = u.snapshot()
	}
	return out
}

// MirrorClientExists reports whether a non-tombstone-agnostic client row
// exists in the mirror for the given ID. (Tombstones are kept in the mirror;
// callers that need to skip them check DeletedAt themselves.)
func (s *Service) MirrorClientExists(clientID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.mirrorClients[ClientID(clientID)]
	return ok
}

// MirrorDeployment returns the deployment for a (client, agent) pair from
// the mirror. ok=false when the pair is not tracked.
func (s *Service) MirrorDeployment(clientID, agentID string) (Deployment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byAgent, ok := s.mirrorDeployments[ClientID(clientID)]
	if !ok {
		return Deployment{}, false
	}
	d, ok := byAgent[agentID]
	return d, ok
}

// FindMirrorClientByNameAndSecret returns the first non-deleted client in
// the mirror whose name and secret both match. Mirror-side counterpart of
// the server's findManagedClientByNameAndSecret.
func (s *Service) FindMirrorClientByNameAndSecret(name, secret string) (Client, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.mirrorClients {
		if c.DeletedAt != nil {
			continue
		}
		if c.Name == name && c.Secret == secret {
			return c, true
		}
	}
	return Client{}, false
}

// MirrorIdentifiersForAgent returns the set of client names and secrets
// deployed on an agent according to the mirror's deployment map. Mirror-side
// counterpart of the server's managedClientIdentifiersForAgent.
func (s *Service) MirrorIdentifiersForAgent(agentID string) (names, secrets map[string]struct{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names = make(map[string]struct{})
	secrets = make(map[string]struct{})
	for clientID, byAgent := range s.mirrorDeployments {
		if _, ok := byAgent[agentID]; !ok {
			continue
		}
		c, ok := s.mirrorClients[clientID]
		if !ok || c.DeletedAt != nil {
			continue
		}
		names[c.Name] = struct{}{}
		if c.Secret != "" {
			secrets[c.Secret] = struct{}{}
		}
	}
	return names, secrets
}

// ResolveMirrorIDByName resolves a panel client ID from the mirror's
// client + assignment maps. Mirror-side counterpart of the server's
// resolveClientIDByName.
func (s *Service) ResolveMirrorIDByName(agentID, agentFleetGroupID, clientName string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	clientsByID := make(map[string]Client, len(s.mirrorClients))
	assignmentsByClient := make(map[string][]Assignment, len(s.mirrorAssignments))
	for id, c := range s.mirrorClients {
		clientsByID[string(id)] = c
	}
	for id, as := range s.mirrorAssignments {
		assignmentsByClient[string(id)] = as
	}
	return ResolveIDByName(clientsByID, assignmentsByClient, agentID, agentFleetGroupID, clientName)
}

// MirrorAssignmentsAndDeployments returns defensive copies of the
// assignment slice and deployment list for one client from the mirror.
// Used by the merge-adopt path to snapshot existing state.
func (s *Service) MirrorAssignmentsAndDeployments(clientID string) ([]Assignment, []Deployment) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cid := ClientID(clientID)
	assignments := append([]Assignment(nil), s.mirrorAssignments[cid]...)
	depMap := s.mirrorDeployments[cid]
	deployments := make([]Deployment, 0, len(depMap))
	for _, d := range depMap {
		deployments = append(deployments, d)
	}
	return assignments, deployments
}

// MirrorLastUsageSeq returns the highest usage seq recorded for an agent in
// the mirror. Zero when the agent has no usage rows.
func (s *Service) MirrorLastUsageSeq(agentID string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mirrorLastUsageSeq[agentID]
}

// SetMirrorLastUsageSeq records the per-agent usage seq cursor in the mirror.
// Used by the seq-dedup path (shouldApplyClientUsageDelta) when an agent
// restart rewinds the cursor or a duplicate batch is skipped without a
// usage-row write that would otherwise advance it via UpsertUsageBulk.
func (s *Service) SetMirrorLastUsageSeq(agentID string, seq uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mirrorLastUsageSeq[agentID] = seq
}

// ReplaceMirrorInMemory updates the V2 mirror's client + assignments +
// deployments for one client without touching the Repository. Callers that
// drive persistence through a UnitOfWork (e.g. server.persistAdoptedClient)
// apply this only after the transaction commits. The client's Secret must be
// plaintext — the mirror holds plaintext so apply-jobs ship the real secret.
func (s *Service) ReplaceMirrorInMemory(client Client, assignments []Assignment, deployments []Deployment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cid := client.ID
	s.mirrorClients[cid] = client
	s.mirrorAssignments[cid] = append([]Assignment(nil), assignments...)
	next := make(map[string]Deployment, len(deployments))
	for _, d := range deployments {
		next[d.AgentID] = d
	}
	s.mirrorDeployments[cid] = next
}

// MirrorUsageEntryFor returns the current usage snapshot for a (client,
// agent) pair from the mirror. ok=false when the pair is not tracked.
func (s *Service) MirrorUsageEntryFor(clientID, agentID string) (UsageSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	byAgent, ok := s.mirrorUsage[ClientID(clientID)]
	if !ok {
		return UsageSnapshot{}, false
	}
	u, ok := byAgent[agentID]
	if !ok {
		return UsageSnapshot{}, false
	}
	return u.snapshot(), true
}
