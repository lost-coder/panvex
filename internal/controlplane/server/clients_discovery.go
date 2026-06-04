package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/agenttransport"
	"github.com/lost-coder/panvex/internal/controlplane/clients"
	"github.com/lost-coder/panvex/internal/controlplane/discovered"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/uow"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

const (
	discoveredClientStatusPendingReview = "pending_review"
	discoveredClientStatusAdopted       = "adopted"
	discoveredClientStatusIgnored       = "ignored"
)

// ErrAlreadyAdopted is returned by adoptDiscoveredClient when the discovered
// record has already been adopted. Previously this was a generic
// fmt.Errorf("client already adopted"); the sentinel lets tests and HTTP
// handlers detect the condition reliably via errors.Is (P2-LOG-03 / L-11).
var ErrAlreadyAdopted = errors.New("client already adopted")

type discoveredClient struct {
	ID                 string
	AgentID            string
	ClientName         string
	Secret             string
	Status             string
	TotalOctets        uint64
	CurrentConnections int
	ActiveUniqueIPs    int
	ConnectionLinks    []string
	MaxTCPConns        int
	MaxUniqueIPs       int
	DataQuotaBytes     int64
	Expiration         string
	DiscoveredAt       time.Time
	UpdatedAt          time.Time
}

// reconcileDiscoveredClients compares client data returned by an agent against
// the panel's managed clients and creates discovered_client records for unknown users.
func (s *Server) reconcileDiscoveredClients(ctx context.Context, agentID string, records []*gatewayrpc.ClientDetailRecord, telemtUnreachable bool, observedAt time.Time) {
	if telemtUnreachable {
		// The agent could not read Telemt's user list. An empty record set
		// here means "unknown", NOT "zero clients" — do not prune, do not
		// reconcile. The recovery-edge / periodic refresh will re-request a
		// real snapshot once Telemt is back.
		s.logger.Warn("skipping discovery reconcile: agent reported telemt unreachable", "agent_id", agentID)
		return
	}
	if len(records) == 0 {
		return
	}

	managedNames, managedSecrets := s.managedClientIdentifiersForAgent(agentID)

	// seenNames is every user the node currently reports. HandleClientDataRequest
	// always returns the FULL set of configured Telemt users, so this is an
	// authoritative snapshot of what exists on the node right now (IN-M5).
	seenNames := make(map[string]struct{}, len(records))

	var disc, skippedManaged, skippedPanelID int
	for _, record := range records {
		clientName := strings.TrimSpace(record.GetClientName())
		if clientName == "" {
			continue
		}
		seenNames[clientName] = struct{}{}

		// Skip clients that are already managed by the panel.
		if _, managed := managedNames[clientName]; managed {
			skippedManaged++
			continue
		}

		// Skip if the secret matches an already-managed client (same user, different name).
		secret := strings.TrimSpace(record.GetSecret())
		if secret != "" {
			if _, managed := managedSecrets[secret]; managed {
				// IN-L4: secret reuse under a DIFFERENT name on the node is an
				// operator anomaly (it masks a genuinely unmanaged user as
				// "managed"). The name is not in managedNames (checked above),
				// so log it as a conflict instead of silently swallowing it.
				s.logger.Warn("discovered client shares a managed secret under a different name; skipping as managed",
					"agent_id", agentID,
					"client_name", clientName,
					"alert", "discovered_secret_name_conflict",
				)
				skippedManaged++
				continue
			}
		}

		// Skip if panel-assigned client_id is present (means panel created it).
		if strings.TrimSpace(record.GetClientId()) != "" {
			skippedPanelID++
			continue
		}

		disc++
		s.upsertDiscoveredClient(ctx, agentID, record, observedAt)
	}
	s.logger.Info("reconciled discovered clients", "agent_id", agentID, "total", len(records), "new", disc, "managed", skippedManaged, "panel_assigned", skippedPanelID)

	// IN-M5: prune pending discovered records for this agent that the node no
	// longer reports (e.g. the user was removed, or the agent's fleet group
	// changed and its managed clients were rolled off). Safe because we only
	// reach here on a non-empty response (early-return above) and the response
	// is the full user set. Only PENDING records are pruned — adopted/ignored
	// decisions are preserved.
	s.pruneStaleDiscoveredForAgent(ctx, agentID, seenNames)
}

// pruneStaleDiscoveredForAgent deletes pending discovered records owned by the
// agent whose client_name is absent from seenNames (the node's current full
// user set). Best-effort: list/delete failures are logged, not fatal.
func (s *Server) pruneStaleDiscoveredForAgent(ctx context.Context, agentID string, seenNames map[string]struct{}) {
	if s.discoveredRepo == nil {
		return
	}
	all, err := s.discoveredRepo.List(ctx)
	if err != nil {
		s.logger.Warn("pruneStaleDiscoveredForAgent: list failed", "agent_id", agentID, "error", err)
		return
	}
	pruned := 0
	for _, dc := range all {
		if dc.AgentID != agentID || dc.Status != discovered.StatusPending {
			continue
		}
		if _, seen := seenNames[dc.ClientName]; seen {
			continue
		}
		if err := s.discoveredRepo.Delete(ctx, dc.ID); err != nil {
			s.logger.Warn("pruneStaleDiscoveredForAgent: delete failed",
				"agent_id", agentID, "discovered_id", string(dc.ID), "error", err)
			continue
		}
		pruned++
	}
	if pruned > 0 {
		s.logger.Info("pruned stale discovered clients", "agent_id", agentID, "pruned", pruned)
	}
}

// managedClientIdentifiersForAgent returns the set of client names and secrets deployed on an agent.
func (s *Server) managedClientIdentifiersForAgent(agentID string) (names map[string]struct{}, secrets map[string]struct{}) {
	return s.clientsSvc.MirrorIdentifiersForAgent(agentID)
}

func (s *Server) upsertDiscoveredClient(ctx context.Context, agentID string, record *gatewayrpc.ClientDetailRecord, observedAt time.Time) {
	clientName := record.GetClientName()

	// P2-LOG-02 / L-10: before inserting a brand-new row, check whether a
	// discovered_clients row already exists for (agent_id, client_name).
	// If yes and it is still pending_review, update the existing row in
	// place — every agent reconnect triggers a FULL_SNAPSHOT, and without
	// this dedupe the pending-review list would grow unbounded. The
	// underlying UNIQUE (agent_id, client_name) constraint is a
	// belt-and-suspenders guard; this code path also avoids burning a new
	// sequence ID each time and keeps the audit log free of spurious
	// "clients.discovered" events for the same user.
	var (
		existing     discovered.DiscoveredClient
		haveExisting bool
		existingErr  error
	)
	if s.discoveredRepo != nil {
		existing, existingErr = s.discoveredRepo.GetByAgentAndName(ctx, agentID, clientName)
		switch {
		case existingErr == nil:
			haveExisting = true
		case errors.Is(existingErr, storage.ErrNotFound):
			// no-op: fall through to insert path
		default:
			s.logger.Error("discovered client lookup failed", "client_name", clientName, "agent_id", agentID, "error", existingErr)
			return
		}
	}

	var id string
	if haveExisting {
		id = string(existing.ID)
	} else {
		id = s.clientsSvc.NextDiscoveredID()
	}

	firstSeen := observedAt.UTC()
	if haveExisting {
		firstSeen = existing.FirstSeen
	}

	status := discovered.StatusPending
	if haveExisting {
		// Preserve non-pending status (ignored/adopted) across updates; only
		// refresh mutable observability fields. Without this guard a later
		// reconcile could resurrect an ignored row back to pending_review.
		if existing.Status != discovered.StatusPending {
			status = existing.Status
		}
	}

	dc := discovered.DiscoveredClient{
		ID:                 discovered.DiscoveredID(id),
		AgentID:            agentID,
		ClientName:         clientName,
		Secret:             record.GetSecret(),
		Status:             status,
		TotalOctets:        record.GetTotalOctets(),
		CurrentConnections: uint32(record.GetCurrentConnections()), //nolint:gosec
		ActiveUniqueIPs:    uint32(record.GetActiveUniqueIps()),    //nolint:gosec
		ConnectionLinks:    record.GetConnectionLinks(),
		MaxTCPConns:        int(record.GetMaxTcpConns()),      //nolint:gosec
		MaxUniqueIPs:       int(record.GetMaxUniqueIps()),     //nolint:gosec
		DataQuotaBytes:     int64(record.GetDataQuotaBytes()), //nolint:gosec
		Expiration:         record.GetExpiration(),
		FirstSeen:          firstSeen,
		UpdatedAt:          observedAt.UTC(),
	}

	if s.discoveredRepo != nil {
		if err := s.discoveredRepo.Save(ctx, dc); err != nil {
			s.logger.Error("discovered client persistence failed", "client_name", dc.ClientName, "agent_id", agentID, "error", err)
			return
		}
	}

	// Only audit the first-time discovery; subsequent observations of the
	// same (agent, client) are just re-reports of the same finding.
	if !haveExisting {
		s.appendAuditWithContext(ctx, "system", "clients.discovered", id, map[string]any{
			"agent_id":    agentID,
			"client_name": dc.ClientName,
		})
	}
}

func (s *Server) listDiscoveredClients(ctx context.Context) ([]discoveredClient, error) {
	if s.discoveredRepo == nil {
		return nil, nil
	}

	recs, err := s.discoveredRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]discoveredClient, 0, len(recs))
	for _, r := range recs {
		result = append(result, discoveredClientFromDomain(r))
	}
	return result, nil
}

func (s *Server) adoptDiscoveredClient(ctx context.Context, id, actorID string, observedAt time.Time) (managedClient, error) {
	// P2-LOG-03 / L-11: serialize the whole read-check-create-mark sequence
	// under adoptMu so that two concurrent adopts of the same discovered
	// record cannot both pass the status check and each create a managed
	// client. The lock also covers mergeAdoptIntoExistingClient (called
	// below) which closes P2-LOG-04 / L-12.
	s.adoptMu.Lock()
	defer s.adoptMu.Unlock()
	return s.adoptDiscoveredClientLocked(ctx, id, actorID, observedAt)
}

// adoptDiscoveredClientLocked runs the adopt logic without acquiring
// adoptMu. Callers (adoptDiscoveredClient, bulkAdoptDiscoveredClients)
// MUST hold adoptMu. Splitting this out lets bulk-adopt take the lock
// once for the whole batch instead of churning it per id.
func (s *Server) adoptDiscoveredClientLocked(ctx context.Context, id, actorID string, observedAt time.Time) (managedClient, error) {
	if s.discoveredRepo == nil {
		return managedClient{}, storage.ErrNotFound
	}

	// Fresh read under the lock — do NOT trust a record fetched before the
	// mutex was acquired; another goroutine may have already flipped the
	// status to "adopted" while we were waiting.
	record, err := s.discoveredRepo.Get(ctx, discovered.DiscoveredID(id))
	if err != nil {
		return managedClient{}, err
	}

	if record.Status == discovered.StatusAdopted {
		return managedClient{}, ErrAlreadyAdopted
	}

	observedAt = observedAt.UTC()

	secret, err := normalizedAdoptSecret(record.Secret)
	if err != nil {
		return managedClient{}, err
	}

	expirationRFC3339, err := normalizedExpiration(record.Expiration)
	if err != nil {
		return managedClient{}, err
	}

	// Same logical client (name+secret) may have been discovered on
	// multiple nodes — fetch every still-pending sibling so the resulting
	// managed client is scoped to ALL of them on first adopt. Without
	// this, the panel ends up with a client scoped only to the agent
	// whose record was passed in, and the auto-flip below would mark the
	// other discovered rows "adopted" before they were ever processed.
	siblings := s.collectAdoptSiblings(ctx, record)

	// Check if a managed client with the same name+secret already exists
	// (e.g. adopted from a different node). If so, merge by adding an
	// assignment and deployment to the existing client instead of creating
	// a duplicate.
	if existing, ok := s.findManagedClientByNameAndSecret(record.ClientName, secret); ok {
		s.logger.Info("adopting discovered client into existing managed client", "discovered_id", id, "client_id", existing.ID, "client_name", record.ClientName, "agent_id", record.AgentID, "siblings", len(siblings))
		return s.mergeAdoptIntoExistingClient(ctx, existing, record, siblings, actorID, id, observedAt)
	}
	s.logger.Info("adopting discovered client as new managed client", "discovered_id", id, "client_name", record.ClientName, "agent_id", record.AgentID, "traffic_bytes", record.TotalOctets, "active_ips", record.ActiveUniqueIPs, "siblings", len(siblings))

	client, assignments, deployments := s.buildAdoptedClientState(record, siblings, secret, expirationRFC3339, observedAt)

	if err := s.persistAdoptedClient(ctx, id, client, assignments, deployments, observedAt); err != nil {
		return managedClient{}, err
	}

	// Update the in-memory mirror now that the commit succeeded. If this
	// fails (it can't today; replaceClientStateInMemory never errors when
	// the store portion is skipped), the reconciler will catch up on the
	// next snapshot.
	s.replaceClientStateInMemory(client, assignments, deployments)

	// Seed live usage with the stats Telemt already reported for this user
	// — primary record plus every sibling we just folded in.
	s.seedClientUsage(ctx, string(client.ID), record.AgentID, record.TotalOctets, int(record.CurrentConnections), int(record.ActiveUniqueIPs), observedAt) //nolint:gosec
	for _, sib := range siblings {
		s.seedClientUsage(ctx, string(client.ID), sib.AgentID, sib.TotalOctets, int(sib.CurrentConnections), int(sib.ActiveUniqueIPs), observedAt) //nolint:gosec
	}

	s.appendAuditWithContext(ctx, actorID, "clients.adopted", id, map[string]any{
		"client_name":     record.ClientName,
		"client_id":       client.ID,
		"sibling_records": len(siblings),
	})

	return client, nil
}

// collectAdoptSiblings returns every still-pending discovered record
// that shares (ClientName, Secret) with the primary record. Rows
// already adopted/ignored are skipped, as is the primary itself. An
// empty secret never matches siblings (we cannot trust name alone to
// represent the same Telemt user).
func (s *Server) collectAdoptSiblings(ctx context.Context, primary discovered.DiscoveredClient) []discovered.DiscoveredClient {
	if s.discoveredRepo == nil || strings.TrimSpace(primary.Secret) == "" {
		return nil
	}
	all, err := s.discoveredRepo.List(ctx)
	if err != nil {
		s.logger.Warn("collectAdoptSiblings: list discovered failed", "error", err)
		return nil
	}
	siblings := make([]discovered.DiscoveredClient, 0)
	seenAgents := map[string]struct{}{primary.AgentID: {}}
	for _, dc := range all {
		if dc.ID == primary.ID || dc.Status == discovered.StatusAdopted {
			continue
		}
		if dc.Secret != primary.Secret || dc.ClientName != primary.ClientName {
			continue
		}
		if _, dup := seenAgents[dc.AgentID]; dup {
			continue
		}
		seenAgents[dc.AgentID] = struct{}{}
		siblings = append(siblings, dc)
	}
	return siblings
}

// bulkAdoptDiscoveredClients adopts every id in a single locked
// session. Holding adoptMu across the whole batch (rather than
// re-acquiring per id) keeps siblings stable while the loop walks
// the list and lets the per-call duplicate-flip naturally short-
// circuit subsequent ids belonging to the same logical client.
//
// Returns one BulkAdoptResult per input id in the same order. The
// outer caller decides whether to keep the bulk request as a single
// rate-limited HTTP unit; this method makes no rate-limit decisions
// itself.
func (s *Server) bulkAdoptDiscoveredClients(ctx context.Context, ids []string, actorID string, observedAt time.Time) []BulkAdoptResult {
	if len(ids) == 0 {
		return nil
	}
	s.adoptMu.Lock()
	defer s.adoptMu.Unlock()

	results := make([]BulkAdoptResult, 0, len(ids))
	for _, id := range ids {
		client, err := s.adoptDiscoveredClientLocked(ctx, id, actorID, observedAt)
		switch {
		case err == nil:
			results = append(results, BulkAdoptResult{
				ID:       id,
				Status:   "adopted",
				ClientID: string(client.ID),
				Name:     client.Name,
			})
		case errors.Is(err, ErrAlreadyAdopted):
			// Either a previous iteration in this same bulk pulled this
			// record in as a sibling (correct, expected outcome) or a
			// concurrent adopt got there first. Either way the discovered
			// row is now resolved — surface a non-error status so the UI
			// can count it as "handled" rather than failed.
			results = append(results, BulkAdoptResult{ID: id, Status: "already_adopted"})
		case errors.Is(err, storage.ErrNotFound):
			results = append(results, BulkAdoptResult{ID: id, Status: "error", Message: "not found"})
		default:
			s.logger.Error("bulk adopt: per-id failure", "discovered_id", id, "error", err)
			results = append(results, BulkAdoptResult{ID: id, Status: "error", Message: err.Error()})
		}
	}
	return results
}

// BulkAdoptResult is the per-id outcome from bulkAdoptDiscoveredClients.
// Status is one of: "adopted" (new client or merged into existing),
// "already_adopted" (resolved via a sibling earlier in the batch or by
// a concurrent caller), "error" (Message holds details).
type BulkAdoptResult struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	ClientID string `json:"client_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Message  string `json:"message,omitempty"`
}

// normalizedAdoptSecret validates the secret carried on the discovered record
// and falls back to a freshly generated one if absent. Splitting this out
// flattens adoptDiscoveredClient's leading guard chain.
func normalizedAdoptSecret(raw string) (string, error) {
	secret := strings.TrimSpace(raw)
	if secret == "" {
		return randomHexString(16)
	}
	if !isValidHexSecret(secret) {
		return "", fmt.Errorf("invalid secret format: must be 32 hex characters")
	}
	return secret, nil
}

// buildAdoptedClientState assembles the managedClient + initial
// assignments + initial deployments for a freshly-adopted discovered
// record. When siblings is non-empty, every sibling's agent_id is
// included so the resulting managed client is scoped to every node
// where Telemt was already running this user.
func (s *Server) buildAdoptedClientState(record discovered.DiscoveredClient, siblings []discovered.DiscoveredClient, secret string, expirationRFC3339 string, observedAt time.Time) (managedClient, []managedClientAssignment, []managedClientDeployment) { // gitleaks:allow — `secret` is a function parameter name, not a value
	client := managedClient{
		ID:                s.nextClientID(),
		Name:              record.ClientName,
		Secret:            secret,
		Enabled:           true,
		MaxTCPConns:       record.MaxTCPConns,
		MaxUniqueIPs:      record.MaxUniqueIPs,
		DataQuotaBytes:    record.DataQuotaBytes,
		ExpirationRFC3339: expirationRFC3339,
		CreatedAt:         observedAt,
		UpdatedAt:         observedAt,
	}

	appliedAt := observedAt
	assignments := make([]managedClientAssignment, 0, 1+len(siblings))
	deployments := make([]managedClientDeployment, 0, 1+len(siblings))

	addAgent := func(agentID string, connectionLinks []string) {
		assignments = append(assignments, managedClientAssignment{
			ID:         s.nextClientAssignmentID(),
			ClientID:   client.ID,
			TargetType: clientAssignmentTargetAgent,
			AgentID:    agentID,
			CreatedAt:  observedAt,
		})
		deployments = append(deployments, managedClientDeployment{
			ClientID:         client.ID,
			AgentID:          agentID,
			DesiredOperation: "adopt",
			Status:           clientDeploymentStatusSucceeded,
			ConnectionLinks:  connectionLinks,
			LastAppliedAt:    &appliedAt,
			UpdatedAt:        observedAt,
		})
	}

	addAgent(record.AgentID, record.ConnectionLinks)
	for _, sib := range siblings {
		if sib.AgentID == "" || sib.AgentID == record.AgentID {
			continue
		}
		addAgent(sib.AgentID, sib.ConnectionLinks)
	}
	return client, assignments, deployments
}

// persistAdoptedClient performs the atomic write of the new managed client,
// flips the discovered row, and bulk-marks duplicates of the same secret.
// P2-ARCH-01: all writes share one UoW.Do transaction so a partial failure
// cannot leave the system half-converted.
func (s *Server) persistAdoptedClient(ctx context.Context, discoveredID string, client managedClient, assignments []managedClientAssignment, deployments []managedClientDeployment, observedAt time.Time) error {
	encryptedSecret, err := s.clientsSvc.EncryptSecret(client.Secret)
	if err != nil {
		return fmt.Errorf("persistAdoptedClient: encrypt secret: %w", err)
	}
	toStore := client
	toStore.Secret = encryptedSecret

	return s.uow.Do(ctx, func(rs uow.RepoSet) error {
		// Re-read the discovered record inside the tx so another
		// concurrent adopt on a different control-plane instance cannot
		// slip past the pre-check. adoptMu covers the current instance;
		// the tx + re-read covers cross-instance racing.
		freshRecord, err := rs.Discovered().Get(ctx, discovered.DiscoveredID(discoveredID))
		if err != nil {
			return err
		}
		if freshRecord.Status == discovered.StatusAdopted {
			return ErrAlreadyAdopted
		}

		if err := rs.Clients().Save(ctx, toStore); err != nil {
			return err
		}
		if err := rs.Clients().SaveAssignments(ctx, client.ID, assignments); err != nil {
			return err
		}
		if err := rs.Clients().SaveDeployments(ctx, client.ID, deployments); err != nil {
			return err
		}
		if err := rs.Discovered().UpdateStatus(ctx, discovered.DiscoveredID(discoveredID), discovered.StatusAdopted, observedAt.UTC()); err != nil {
			return err
		}
		if freshRecord.Secret != "" {
			if err := markDuplicateDiscoveredClientsAdoptedUoW(ctx, rs, discoveredID, freshRecord.ClientName, freshRecord.Secret, observedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// markDuplicateDiscoveredClientsAdopted marks all other discovered clients with the same
// secret as adopted, since they represent the same user on different servers.
func (s *Server) markDuplicateDiscoveredClientsAdopted(ctx context.Context, excludeID, name, secret string, observedAt time.Time) {
	if s.discoveredRepo == nil {
		return
	}
	all, err := s.discoveredRepo.List(ctx)
	if err != nil {
		return
	}
	// Q2.U-P-10: collect all duplicate IDs and flip them in a single
	// SQL UPDATE instead of N round-trips.
	ids := collectDuplicateDiscoveredIDs(all, excludeID, name, secret)
	if len(ids) == 0 {
		return
	}
	discIDs := make([]discovered.DiscoveredID, len(ids))
	for i, id := range ids {
		discIDs[i] = discovered.DiscoveredID(id)
	}
	if err := s.discoveredRepo.UpdateStatusBulk(ctx, discIDs, discovered.StatusAdopted, observedAt.UTC()); err != nil {
		s.logger.Error("bulk mark duplicate discovered clients adopted failed", "count", len(ids), "error", err)
	}
}

// markDuplicateDiscoveredClientsAdoptedUoW is the UoW-bound twin of
// markDuplicateDiscoveredClientsAdopted. It operates via the
// caller-provided RepoSet (tx-bound inside uow.Do) so the duplicate-flip
// lands in the same atomic unit as the primary adopt writes. Unlike the
// untransacted variant it surfaces errors to the caller — inside a UoW.Do,
// failing one write must abort the whole transaction.
func markDuplicateDiscoveredClientsAdoptedUoW(ctx context.Context, rs uow.RepoSet, excludeID, name, secret string, observedAt time.Time) error {
	all, err := rs.Discovered().List(ctx)
	if err != nil {
		return err
	}
	ids := collectDuplicateDiscoveredIDs(all, excludeID, name, secret)
	if len(ids) == 0 {
		return nil
	}
	discIDs := make([]discovered.DiscoveredID, len(ids))
	for i, id := range ids {
		discIDs[i] = discovered.DiscoveredID(id)
	}
	return rs.Discovered().UpdateStatusBulk(ctx, discIDs, discovered.StatusAdopted, observedAt.UTC())
}

// collectDuplicateDiscoveredIDs returns IDs of every non-adopted
// duplicate of (name, secret) excluding the primary adopt target. Lifted
// into a helper so the tx and non-tx paths share the same filter logic.
//
// IN-H7: the match requires BOTH name and secret, matching
// collectAdoptSiblings. Matching on secret alone flipped a discovered
// record with the SAME secret but a DIFFERENT name to "adopted" without
// ever attaching it to a managed client — silently hiding a genuinely
// unmanaged proxy user (secret reuse under a different name).
func collectDuplicateDiscoveredIDs(all []discovered.DiscoveredClient, excludeID, name, secret string) []string {
	if secret == "" {
		return nil
	}
	ids := make([]string, 0)
	for _, dc := range all {
		if string(dc.ID) == excludeID || dc.Secret != secret || dc.ClientName != name || dc.Status == discovered.StatusAdopted {
			continue
		}
		ids = append(ids, string(dc.ID))
	}
	return ids
}

// findManagedClientByNameAndSecret returns an existing managed client matching
// both name and secret. Used to detect when a discovered client on a new node
// corresponds to an already-adopted client from another node.
func (s *Server) findManagedClientByNameAndSecret(name, secret string) (managedClient, bool) {
	return s.clientsSvc.MirrorFindClientByNameAndSecret(name, secret)
}

// mergeAdoptIntoExistingClient adds an assignment and deployment for a new agent
// to an already-managed client, and seeds usage from the discovered record.
//
// LOCKING: callers MUST hold s.adoptMu. The RLock/RUnlock below reads the
// current assignment/deployment lists for the existing client, but before
// P2-LOG-04 the lock was released before replaceClientStateWithContext ran
// — a concurrent mutation between the read and the replace would be
// silently overwritten. With adoptMu held, all adopt-path writes are
// serialized so the snapshot taken here cannot be invalidated by another
// adopt/merge. (Audit finding L-12 / M-C6.)
func (s *Server) mergeAdoptIntoExistingClient(
	ctx context.Context,
	existing managedClient,
	record discovered.DiscoveredClient,
	siblings []discovered.DiscoveredClient,
	actorID string,
	discoveredID string,
	observedAt time.Time,
) (managedClient, error) {
	// Snapshot current assignments/deployments and build a set of agents
	// already covered so we don't append duplicates when adding the
	// primary record + siblings.
	existingAssignments, existingDeployments := s.clientsSvc.MirrorAssignmentsAndDeployments(string(existing.ID))

	covered := make(map[string]struct{}, len(existingAssignments))
	for _, a := range existingAssignments {
		if a.TargetType == clientAssignmentTargetAgent && a.AgentID != "" {
			covered[a.AgentID] = struct{}{}
		}
	}

	appliedAt := observedAt
	addAgent := func(agentID string, connectionLinks []string) {
		if agentID == "" {
			return
		}
		if _, ok := covered[agentID]; ok {
			return
		}
		covered[agentID] = struct{}{}
		existingAssignments = append(existingAssignments, managedClientAssignment{
			ID:         s.nextClientAssignmentID(),
			ClientID:   existing.ID,
			TargetType: clientAssignmentTargetAgent,
			AgentID:    agentID,
			CreatedAt:  observedAt,
		})
		existingDeployments = append(existingDeployments, managedClientDeployment{
			ClientID:         existing.ID,
			AgentID:          agentID,
			DesiredOperation: "adopt",
			Status:           clientDeploymentStatusSucceeded,
			ConnectionLinks:  connectionLinks,
			LastAppliedAt:    &appliedAt,
			UpdatedAt:        observedAt,
		})
	}

	addAgent(record.AgentID, record.ConnectionLinks)
	for _, sib := range siblings {
		addAgent(sib.AgentID, sib.ConnectionLinks)
	}

	existing.UpdatedAt = observedAt
	if err := s.replaceClientStateWithContext(ctx, existing, existingAssignments, existingDeployments); err != nil {
		return managedClient{}, err
	}

	// Seed usage from primary + siblings.
	s.seedClientUsage(ctx, string(existing.ID), record.AgentID, record.TotalOctets, int(record.CurrentConnections), int(record.ActiveUniqueIPs), observedAt) //nolint:gosec
	for _, sib := range siblings {
		if sib.AgentID == record.AgentID {
			continue
		}
		s.seedClientUsage(ctx, string(existing.ID), sib.AgentID, sib.TotalOctets, int(sib.CurrentConnections), int(sib.ActiveUniqueIPs), observedAt) //nolint:gosec
	}

	// Mark discovered record as adopted.
	if s.discoveredRepo != nil {
		if err := s.discoveredRepo.UpdateStatus(ctx, discovered.DiscoveredID(discoveredID), discovered.StatusAdopted, observedAt.UTC()); err != nil {
			s.logger.Error("failed to update discovered client status", "error", err)
		}
	}
	if record.Secret != "" {
		s.markDuplicateDiscoveredClientsAdopted(ctx, discoveredID, record.ClientName, record.Secret, observedAt)
	}

	s.appendAuditWithContext(ctx, actorID, "clients.adopted_merge", discoveredID, map[string]any{
		"client_name":     record.ClientName,
		"client_id":       existing.ID,
		"agent_id":        record.AgentID,
		"sibling_records": len(siblings),
	})

	return existing, nil
}

// seedClientUsage initializes the in-memory usage for a client on a specific
// agent with the values reported by Telemt at discovery time.
func (s *Server) seedClientUsage(ctx context.Context, clientID, agentID string, trafficBytes uint64, connections, uniqueIPs int, observedAt time.Time) {
	lastSeq := s.clientsSvc.MirrorLastUsageSeq(agentID)

	if s.clientsSvc != nil && s.clientsSvc.HasRepo() {
		if err := s.clientsSvc.UpsertUsage(ctx, clients.Usage{
			ClientID:         clients.ClientID(clientID),
			AgentID:          agentID,
			TrafficUsedBytes: trafficBytes,
			UniqueIPsUsed:    uniqueIPs,
			ActiveTCPConns:   connections,
			ActiveUniqueIPs:  uniqueIPs,
			LastSeq:          lastSeq,
			ObservedAt:       observedAt,
		}); err != nil {
			s.logger.Warn("persist client_usage (seed)",
				"client_id", clientID, "agent_id", agentID, "error", err)
		}
	}
}

func (s *Server) ignoreDiscoveredClient(ctx context.Context, id, actorID string, observedAt time.Time) error {
	if s.discoveredRepo == nil {
		return storage.ErrNotFound
	}

	if err := s.discoveredRepo.UpdateStatus(ctx, discovered.DiscoveredID(id), discovered.StatusIgnored, observedAt.UTC()); err != nil {
		return err
	}

	s.appendAuditWithContext(ctx, actorID, "clients.discovery_ignored", id, nil)
	return nil
}

func (s *Server) restoreStoredDiscoveredClients() error {
	if s.discoveredRepo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(s.serverCtx, 30*time.Second)
	defer cancel()
	recs, err := s.discoveredRepo.List(ctx)
	if err != nil {
		return err
	}
	discoveredIDs := make([]string, 0, len(recs))
	for _, r := range recs {
		discoveredIDs = append(discoveredIDs, string(r.ID))
	}
	// Seed the clients.Service discovered-seq cursor so the next
	// NextDiscoveredID returns a value strictly greater than any persisted ID.
	s.clientsSvc.RecoverSequencesFromRecords(nil, nil, discoveredIDs)
	return nil
}

// sendClientDataRequest sends a FULL_SNAPSHOT request to the agent stream.
func sendClientDataRequest(sess agenttransport.AgentSession, requestID string) error {
	return sess.Send(&gatewayrpc.ConnectServerMessage{
		Body: &gatewayrpc.ConnectServerMessage_ClientDataRequest{
			ClientDataRequest: &gatewayrpc.ClientDataRequest{
				Type:      gatewayrpc.ClientDataRequest_FULL_SNAPSHOT,
				RequestId: requestID,
			},
		},
	})
}

// discoveredClientFromDomain converts a discovered.DiscoveredClient domain value
// into the server-local discoveredClient view type.
func discoveredClientFromDomain(r discovered.DiscoveredClient) discoveredClient {
	return discoveredClient{
		ID:                 string(r.ID),
		AgentID:            r.AgentID,
		ClientName:         r.ClientName,
		Secret:             r.Secret,
		Status:             string(r.Status),
		TotalOctets:        r.TotalOctets,
		CurrentConnections: int(r.CurrentConnections), //nolint:gosec
		ActiveUniqueIPs:    int(r.ActiveUniqueIPs),    //nolint:gosec
		ConnectionLinks:    r.ConnectionLinks,
		MaxTCPConns:        r.MaxTCPConns,
		MaxUniqueIPs:       r.MaxUniqueIPs,
		DataQuotaBytes:     r.DataQuotaBytes,
		Expiration:         r.Expiration,
		DiscoveredAt:       r.FirstSeen,
		UpdatedAt:          r.UpdatedAt,
	}
}

// discoveredClientFromStorageRecord maps a storage.DiscoveredClientRecord to a
// discovered.DiscoveredClient for the tx-path functions that still receive a
// storage record (persistAdoptedClient's Transact closure).
func discoveredClientFromStorageRecord(r storage.DiscoveredClientRecord) discovered.DiscoveredClient {
	return discovered.DiscoveredClient{
		ID:                 discovered.DiscoveredID(r.ID),
		AgentID:            r.AgentID,
		ClientName:         r.ClientName,
		Secret:             r.Secret,
		Status:             discovered.Status(r.Status),
		TotalOctets:        r.TotalOctets,
		CurrentConnections: uint32(r.CurrentConnections), //nolint:gosec
		ActiveUniqueIPs:    uint32(r.ActiveUniqueIPs),    //nolint:gosec
		ConnectionLinks:    r.ConnectionLinks,
		MaxTCPConns:        r.MaxTCPConns,
		MaxUniqueIPs:       r.MaxUniqueIPs,
		DataQuotaBytes:     r.DataQuotaBytes,
		Expiration:         r.Expiration,
		FirstSeen:          r.DiscoveredAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

func sortDiscoveredClientsByName(clients []discoveredClient) {
	sort.Slice(clients, func(i, j int) bool {
		return clients[i].ClientName < clients[j].ClientName
	})
}
