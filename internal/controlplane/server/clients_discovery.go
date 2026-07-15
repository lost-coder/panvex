package server

import (
	"context"
	"sort"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/discovered"
)

const (
	discoveredClientStatusPendingReview = "pending_review"
	discoveredClientStatusAdopted       = "adopted"
	discoveredClientStatusIgnored       = "ignored"
)

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

// listDiscoveredClients is the server-side presentation wrapper over the
// clients.Service domain read (ListDiscovered): it maps each domain
// discovered.DiscoveredClient into the server-local discoveredClient
// viewmodel consumed by the HTTP handlers.
func (s *Server) listDiscoveredClients(ctx context.Context) ([]discoveredClient, error) {
	recs, err := s.clientsSvc.ListDiscovered(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]discoveredClient, 0, len(recs))
	for _, r := range recs {
		result = append(result, discoveredClientFromDomain(r))
	}
	return result, nil
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

func sortDiscoveredClientsByName(clients []discoveredClient) {
	sort.Slice(clients, func(i, j int) bool {
		return clients[i].ClientName < clients[j].ClientName
	})
}
