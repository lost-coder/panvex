package discovered

import (
	"context"
	"time"
)

// Repository is the persistence contract for discovered clients.
type Repository interface {
	Get(ctx context.Context, id DiscoveredID) (DiscoveredClient, error)
	GetByAgentAndName(ctx context.Context, agentID, clientName string) (DiscoveredClient, error)
	List(ctx context.Context) ([]DiscoveredClient, error)
	ListByAgent(ctx context.Context, agentID string) ([]DiscoveredClient, error)
	Save(ctx context.Context, dc DiscoveredClient) error

	UpdateStatus(ctx context.Context, id DiscoveredID, status Status, observedAt time.Time) error
	UpdateStatusBulk(ctx context.Context, ids []DiscoveredID, status Status, observedAt time.Time) error

	Delete(ctx context.Context, id DiscoveredID) error
}
