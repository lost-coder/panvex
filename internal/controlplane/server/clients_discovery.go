package server

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/gatewayrpc"
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

	for _, record := range records {
		clientName := strings.TrimSpace(record.GetClientName())
		if clientName == "" {
			continue
		}

		// Skip clients that are already managed by the panel.
		if _, managed := managedNames[clientName]; managed {
			continue
		}

		// Skip if the secret matches an already-managed client (same user, different name).
		secret := strings.TrimSpace(record.GetSecret())
		if secret != "" {
			if _, managed := managedSecrets[secret]; managed {
				continue
			}
		}

		// Skip if panel-assigned client_id is present (means panel created it).
		if strings.TrimSpace(record.GetClientId()) != "" {
			continue
		}

		s.upsertDiscoveredClient(ctx, agentID, record, observedAt)
	}
}

// managedClientIdentifiersForAgent returns the set of client names and secrets deployed on an agent.
func (s *Server) managedClientIdentifiersForAgent(agentID string) (names map[string]struct{}, secrets map[string]struct{}) {
	s.mu.RLock()
	defer s.mu.RUnlock()

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
	s.mu.Lock()
	s.discoveredClientSeq++
	id := newSequenceID("discovered", s.discoveredClientSeq)
	s.mu.Unlock()

	dc := storage.DiscoveredClientRecord{
		ID:                 id,
		AgentID:            agentID,
		ClientName:         record.GetClientName(),
		Secret:             record.GetSecret(),
		Status:             discoveredClientStatusPendingReview,
		TotalOctets:        record.GetTotalOctets(),
		CurrentConnections: int(record.GetCurrentConnections()),
		ActiveUniqueIPs:    int(record.GetActiveUniqueIps()),
		ConnectionLink:     record.GetConnectionLink(),
		MaxTCPConns:        int(record.GetMaxTcpConns()),
		MaxUniqueIPs:       int(record.GetMaxUniqueIps()),
		DataQuotaBytes:     int64(record.GetDataQuotaBytes()),
		Expiration:         record.GetExpiration(),
		DiscoveredAt:       observedAt.UTC(),
		UpdatedAt:          observedAt.UTC(),
	}

	if s.store != nil {
		if err := s.store.PutDiscoveredClient(ctx, dc); err != nil {
			s.logger.Error("discovered client persistence failed", "client_name", dc.ClientName, "agent_id", agentID, "error", err)
			return
		}
	}

	s.appendAuditWithContext(ctx, "system", "clients.discovered", dc.ID, map[string]any{
		"agent_id":    agentID,
		"client_name": dc.ClientName,
	})
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
