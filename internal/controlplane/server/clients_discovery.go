package server

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

const (
	discoveredClientStatusPendingReview = "pending_review"
	discoveredClientStatusAdopted       = "adopted"
	discoveredClientStatusIgnored       = "ignored"
)

type discoveredClient struct {
	ID                string
	AgentID           string
	ClientName        string
	Secret            string
	Status            string
	TotalOctets       uint64
	CurrentConnections int
	ActiveUniqueIPs   int
	ConnectionLink    string
	MaxTCPConns       int
	MaxUniqueIPs      int
	DataQuotaBytes    int64
	Expiration        string
	DiscoveredAt      time.Time
	UpdatedAt         time.Time
}

// reconcileDiscoveredClients compares client data returned by an agent against
// the panel's managed clients and creates discovered_client records for unknown users.
func (s *Server) reconcileDiscoveredClients(ctx context.Context, agentID string, records []*gatewayrpc.ClientDetailRecord, observedAt time.Time) {
	if len(records) == 0 {
		return
	}

	managedNames, managedSecrets := s.managedClientIdentifiersForAgent(agentID)

	var discovered, skippedManaged, skippedPanelID int
	for _, record := range records {
		clientName := strings.TrimSpace(record.GetClientName())
		if clientName == "" {
			continue
		}

		// Skip clients that are already managed by the panel.
		if _, managed := managedNames[clientName]; managed {
			skippedManaged++
			continue
		}

		// Skip if the secret matches an already-managed client (same user, different name).
		secret := strings.TrimSpace(record.GetSecret())
		if secret != "" {
			if _, managed := managedSecrets[secret]; managed {
				skippedManaged++
				continue
			}
		}

		// Skip if panel-assigned client_id is present (means panel created it).
		if strings.TrimSpace(record.GetClientId()) != "" {
			skippedPanelID++
			continue
		}

		discovered++
		s.upsertDiscoveredClient(ctx, agentID, record, observedAt)
	}
	s.logger.Info("reconciled discovered clients", "agent_id", agentID, "total", len(records), "new", discovered, "managed", skippedManaged, "panel_assigned", skippedPanelID)
}

// managedClientIdentifiersForAgent returns the set of client names and secrets deployed on an agent.
func (s *Server) managedClientIdentifiersForAgent(agentID string) (names map[string]struct{}, secrets map[string]struct{}) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	names = make(map[string]struct{})
	secrets = make(map[string]struct{})
	for clientID, deployments := range s.clientDeployments {
		if _, ok := deployments[agentID]; !ok {
			continue
		}
		client, ok := s.clients[clientID]
		if !ok || client.DeletedAt != nil {
			continue
		}
		names[client.Name] = struct{}{}
		if client.Secret != "" {
			secrets[client.Secret] = struct{}{}
		}
	}
	return names, secrets
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
		existing      storage.DiscoveredClientRecord
		haveExisting  bool
		existingErr   error
	)
	if s.store != nil {
		existing, existingErr = s.store.GetDiscoveredClientByAgentAndName(ctx, agentID, clientName)
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
		id = existing.ID
	} else {
		s.clientsMu.Lock()
		s.discoveredClientSeq++
		id = newSequenceID("discovered", s.discoveredClientSeq)
		s.clientsMu.Unlock()
	}

	discoveredAt := observedAt.UTC()
	if haveExisting {
		discoveredAt = existing.DiscoveredAt
	}

	status := discoveredClientStatusPendingReview
	if haveExisting {
		// Preserve non-pending status (ignored/adopted) across updates; only
		// refresh mutable observability fields. Without this guard a later
		// reconcile could resurrect an ignored row back to pending_review.
		if existing.Status != discoveredClientStatusPendingReview {
			status = existing.Status
		}
	}

	dc := storage.DiscoveredClientRecord{
		ID:                 id,
		AgentID:            agentID,
		ClientName:         clientName,
		Secret:             record.GetSecret(),
		Status:             status,
		TotalOctets:        record.GetTotalOctets(),
		CurrentConnections: int(record.GetCurrentConnections()),
		ActiveUniqueIPs:    int(record.GetActiveUniqueIps()),
		ConnectionLink:     record.GetConnectionLink(),
		MaxTCPConns:        int(record.GetMaxTcpConns()),
		MaxUniqueIPs:       int(record.GetMaxUniqueIps()),
		DataQuotaBytes:     int64(record.GetDataQuotaBytes()),
		Expiration:         record.GetExpiration(),
		DiscoveredAt:       discoveredAt,
		UpdatedAt:          observedAt.UTC(),
	}

	if s.store != nil {
		if err := s.store.PutDiscoveredClient(ctx, dc); err != nil {
			s.logger.Error("discovered client persistence failed", "client_name", dc.ClientName, "agent_id", agentID, "error", err)
			return
		}
	}

	// Only audit the first-time discovery; subsequent observations of the
	// same (agent, client) are just re-reports of the same finding.
	if !haveExisting {
		s.appendAuditWithContext(ctx, "system", "clients.discovered", dc.ID, map[string]any{
			"agent_id":    agentID,
			"client_name": dc.ClientName,
		})
	}
}

func (s *Server) listDiscoveredClients(ctx context.Context) ([]discoveredClient, error) {
	if s.store == nil {
		return nil, nil
	}

	records, err := s.store.ListDiscoveredClients(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]discoveredClient, 0, len(records))
	for _, r := range records {
		result = append(result, discoveredClientFromRecord(r))
	}
	return result, nil
}

func (s *Server) adoptDiscoveredClient(ctx context.Context, id string, actorID string, observedAt time.Time) (managedClient, error) {
	if s.store == nil {
		return managedClient{}, storage.ErrNotFound
	}

	record, err := s.store.GetDiscoveredClient(ctx, id)
	if err != nil {
		return managedClient{}, err
	}

	if record.Status == discoveredClientStatusAdopted {
		return managedClient{}, fmt.Errorf("client already adopted")
	}

	observedAt = observedAt.UTC()

	// Validate secret.
	secret := strings.TrimSpace(record.Secret)
	if secret != "" {
		if !isValidHexSecret(secret) {
			return managedClient{}, fmt.Errorf("invalid secret format: must be 32 hex characters")
		}
	} else {
		secret, err = randomHexString(16)
		if err != nil {
			return managedClient{}, err
		}
	}

	expirationRFC3339, err := normalizedExpiration(record.Expiration)
	if err != nil {
		return managedClient{}, err
	}

	// Check if a managed client with the same name+secret already exists
	// (e.g. adopted from a different node). If so, merge by adding an
	// assignment and deployment to the existing client instead of creating
	// a duplicate.
	if existing, ok := s.findManagedClientByNameAndSecret(record.ClientName, secret); ok {
		s.logger.Info("adopting discovered client into existing managed client", "discovered_id", id, "client_id", existing.ID, "client_name", record.ClientName, "agent_id", record.AgentID)
		return s.mergeAdoptIntoExistingClient(ctx, existing, record, actorID, id, observedAt)
	}
	s.logger.Info("adopting discovered client as new managed client", "discovered_id", id, "client_name", record.ClientName, "agent_id", record.AgentID, "traffic_bytes", record.TotalOctets, "active_ips", record.ActiveUniqueIPs)

	// Build managed client — no deployment job, user already exists on server.
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

	// Assign to the specific agent the client was discovered on.
	assignments := []managedClientAssignment{
		{
			ID:         s.nextClientAssignmentID(),
			ClientID:   client.ID,
			TargetType: clientAssignmentTargetAgent,
			AgentID:    record.AgentID,
			CreatedAt:  observedAt,
		},
	}

	// Deployment is already applied — user exists on server.
	appliedAt := observedAt
	deployments := []managedClientDeployment{
		{
			ClientID:         client.ID,
			AgentID:          record.AgentID,
			DesiredOperation: "adopt",
			Status:           clientDeploymentStatusSucceeded,
			ConnectionLink:   record.ConnectionLink,
			LastAppliedAt:    &appliedAt,
			UpdatedAt:        observedAt,
		},
	}

	if err := s.replaceClientStateWithContext(ctx, client, assignments, deployments); err != nil {
		return managedClient{}, err
	}

	// Seed live usage with the stats Telemt already reported for this user.
	s.seedClientUsage(client.ID, record.AgentID, record.TotalOctets, record.CurrentConnections, record.ActiveUniqueIPs, observedAt)

	// Mark this record and any other discovered records with the same secret as adopted.
	if err := s.store.UpdateDiscoveredClientStatus(ctx, id, discoveredClientStatusAdopted, observedAt.UTC()); err != nil {
		s.logger.Error("failed to update discovered client status", "error", err)
	}
	if record.Secret != "" {
		s.markDuplicateDiscoveredClientsAdopted(ctx, id, record.Secret, observedAt)
	}

	s.appendAuditWithContext(ctx, actorID, "clients.adopted", id, map[string]any{
		"client_name": record.ClientName,
		"client_id":   client.ID,
	})

	return client, nil
}

// markDuplicateDiscoveredClientsAdopted marks all other discovered clients with the same
// secret as adopted, since they represent the same user on different servers.
func (s *Server) markDuplicateDiscoveredClientsAdopted(ctx context.Context, excludeID string, secret string, observedAt time.Time) {
	if s.store == nil {
		return
	}
	all, err := s.store.ListDiscoveredClients(ctx)
	if err != nil {
		return
	}
	for _, dc := range all {
		if dc.ID == excludeID || dc.Secret != secret || dc.Status == discoveredClientStatusAdopted {
			continue
		}
		if err := s.store.UpdateDiscoveredClientStatus(ctx, dc.ID, discoveredClientStatusAdopted, observedAt.UTC()); err != nil {
			s.logger.Error("failed to mark duplicate discovered client as adopted", "discovered_client_id", dc.ID, "error", err)
		}
	}
}

// findManagedClientByNameAndSecret returns an existing managed client matching
// both name and secret. Used to detect when a discovered client on a new node
// corresponds to an already-adopted client from another node.
func (s *Server) findManagedClientByNameAndSecret(name string, secret string) (managedClient, bool) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, client := range s.clients {
		if client.DeletedAt != nil {
			continue
		}
		if client.Name == name && client.Secret == secret {
			return client, true
		}
	}
	return managedClient{}, false
}

// mergeAdoptIntoExistingClient adds an assignment and deployment for a new agent
// to an already-managed client, and seeds usage from the discovered record.
func (s *Server) mergeAdoptIntoExistingClient(
	ctx context.Context,
	existing managedClient,
	record storage.DiscoveredClientRecord,
	actorID string,
	discoveredID string,
	observedAt time.Time,
) (managedClient, error) {
	// Build new assignment for this agent.
	newAssignment := managedClientAssignment{
		ID:         s.nextClientAssignmentID(),
		ClientID:   existing.ID,
		TargetType: clientAssignmentTargetAgent,
		AgentID:    record.AgentID,
		CreatedAt:  observedAt,
	}

	// Build deployment record.
	appliedAt := observedAt
	newDeployment := managedClientDeployment{
		ClientID:         existing.ID,
		AgentID:          record.AgentID,
		DesiredOperation: "adopt",
		Status:           clientDeploymentStatusSucceeded,
		ConnectionLink:   record.ConnectionLink,
		LastAppliedAt:    &appliedAt,
		UpdatedAt:        observedAt,
	}

	// Merge with existing assignments and deployments.
	s.clientsMu.RLock()
	existingAssignments := append([]managedClientAssignment(nil), s.clientAssignments[existing.ID]...)
	existingDeployments := make([]managedClientDeployment, 0, len(s.clientDeployments[existing.ID])+1)
	for _, d := range s.clientDeployments[existing.ID] {
		existingDeployments = append(existingDeployments, d)
	}
	s.clientsMu.RUnlock()

	existingAssignments = append(existingAssignments, newAssignment)
	existingDeployments = append(existingDeployments, newDeployment)

	existing.UpdatedAt = observedAt
	if err := s.replaceClientStateWithContext(ctx, existing, existingAssignments, existingDeployments); err != nil {
		return managedClient{}, err
	}

	// Seed usage from this agent's Telemt data.
	s.seedClientUsage(existing.ID, record.AgentID, record.TotalOctets, record.CurrentConnections, record.ActiveUniqueIPs, observedAt)

	// Mark discovered record as adopted.
	if err := s.store.UpdateDiscoveredClientStatus(ctx, discoveredID, discoveredClientStatusAdopted, observedAt.UTC()); err != nil {
		s.logger.Error("failed to update discovered client status", "error", err)
	}
	if record.Secret != "" {
		s.markDuplicateDiscoveredClientsAdopted(ctx, discoveredID, record.Secret, observedAt)
	}

	s.appendAuditWithContext(ctx, actorID, "clients.adopted_merge", discoveredID, map[string]any{
		"client_name": record.ClientName,
		"client_id":   existing.ID,
		"agent_id":    record.AgentID,
	})

	return existing, nil
}

// seedClientUsage initializes the in-memory usage for a client on a specific
// agent with the values reported by Telemt at discovery time.
func (s *Server) seedClientUsage(clientID, agentID string, trafficBytes uint64, connections, uniqueIPs int, observedAt time.Time) {
	s.clientsMu.Lock()
	if s.clientUsage[clientID] == nil {
		s.clientUsage[clientID] = make(map[string]clientUsageSnapshot)
	}
	s.clientUsage[clientID][agentID] = clientUsageSnapshot{
		ClientID:         clientID,
		TrafficUsedBytes: trafficBytes,
		UniqueIPsUsed:    uniqueIPs,
		ActiveTCPConns:   connections,
		ActiveUniqueIPs:  uniqueIPs,
		ObservedAt:       observedAt,
	}
	s.clientsMu.Unlock()
}

func (s *Server) ignoreDiscoveredClient(ctx context.Context, id string, actorID string, observedAt time.Time) error {
	if s.store == nil {
		return storage.ErrNotFound
	}

	if err := s.store.UpdateDiscoveredClientStatus(ctx, id, discoveredClientStatusIgnored, observedAt.UTC()); err != nil {
		return err
	}

	s.appendAuditWithContext(ctx, actorID, "clients.discovery_ignored", id, nil)
	return nil
}

func (s *Server) restoreStoredDiscoveredClients() error {
	if s.store == nil {
		return nil
	}

	records, err := s.store.ListDiscoveredClients(context.Background())
	if err != nil {
		return err
	}

	for _, record := range records {
		s.discoveredClientSeq = maxPrefixedSequence(s.discoveredClientSeq, "discovered", record.ID)
	}
	return nil
}

// sendClientDataRequest sends a FULL_SNAPSHOT request to the agent stream.
func sendClientDataRequest(stream gatewayrpc.AgentGateway_ConnectServer, requestID string) error {
	return stream.Send(&gatewayrpc.ConnectServerMessage{
		Body: &gatewayrpc.ConnectServerMessage_ClientDataRequest{
			ClientDataRequest: &gatewayrpc.ClientDataRequest{
				Type:      gatewayrpc.ClientDataRequest_FULL_SNAPSHOT,
				RequestId: requestID,
			},
		},
	})
}

func discoveredClientFromRecord(r storage.DiscoveredClientRecord) discoveredClient {
	return discoveredClient{
		ID:                 r.ID,
		AgentID:            r.AgentID,
		ClientName:         r.ClientName,
		Secret:             r.Secret,
		Status:             r.Status,
		TotalOctets:        r.TotalOctets,
		CurrentConnections: r.CurrentConnections,
		ActiveUniqueIPs:    r.ActiveUniqueIPs,
		ConnectionLink:     r.ConnectionLink,
		MaxTCPConns:        r.MaxTCPConns,
		MaxUniqueIPs:       r.MaxUniqueIPs,
		DataQuotaBytes:     r.DataQuotaBytes,
		Expiration:         r.Expiration,
		DiscoveredAt:       r.DiscoveredAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

func sortDiscoveredClientsByName(clients []discoveredClient) {
	sort.Slice(clients, func(i, j int) bool {
		return clients[i].ClientName < clients[j].ClientName
	})
}
