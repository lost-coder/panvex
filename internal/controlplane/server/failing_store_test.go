package server

import (
	"context"
	"database/sql"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

type failingStore struct {
	storage.MigrationStore

	putAgentErr               error
	listAgentsErr             error
	listUsersErr              error
	putInstanceErr            error
	appendMetricSnapshotErr   error
	appendAuditEventErr       error
	putClientErr              error
	putClientAssignmentErr    error
	putClientDeploymentErr    error
	updateAgentNodeNameErr    error
	deleteAgentErr            error
	deleteInstancesByAgentErr error
}

func (s *failingStore) PutAgent(ctx context.Context, agent storage.AgentRecord) error {
	if s.putAgentErr != nil {
		return s.putAgentErr
	}

	return s.MigrationStore.PutAgent(ctx, agent)
}

func (s *failingStore) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	if s.listAgentsErr != nil {
		return nil, s.listAgentsErr
	}

	return s.MigrationStore.ListAgents(ctx)
}

func (s *failingStore) ListUsers(ctx context.Context) ([]storage.UserRecord, error) {
	if s.listUsersErr != nil {
		return nil, s.listUsersErr
	}

	return s.MigrationStore.ListUsers(ctx)
}

func (s *failingStore) PutInstance(ctx context.Context, instance storage.InstanceRecord) error {
	if s.putInstanceErr != nil {
		return s.putInstanceErr
	}

	return s.MigrationStore.PutInstance(ctx, instance)
}

func (s *failingStore) AppendMetricSnapshot(ctx context.Context, snapshot storage.MetricSnapshotRecord) error {
	if s.appendMetricSnapshotErr != nil {
		return s.appendMetricSnapshotErr
	}

	return s.MigrationStore.AppendMetricSnapshot(ctx, snapshot)
}

func (s *failingStore) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	if s.appendAuditEventErr != nil {
		return s.appendAuditEventErr
	}

	return s.MigrationStore.AppendAuditEvent(ctx, event)
}

func (s *failingStore) PutClient(ctx context.Context, client storage.ClientRecord) error {
	if s.putClientErr != nil {
		return s.putClientErr
	}

	return s.MigrationStore.PutClient(ctx, client)
}

func (s *failingStore) PutClientAssignment(ctx context.Context, assignment storage.ClientAssignmentRecord) error {
	if s.putClientAssignmentErr != nil {
		return s.putClientAssignmentErr
	}

	return s.MigrationStore.PutClientAssignment(ctx, assignment)
}

func (s *failingStore) PutClientDeployment(ctx context.Context, deployment storage.ClientDeploymentRecord) error {
	if s.putClientDeploymentErr != nil {
		return s.putClientDeploymentErr
	}

	return s.MigrationStore.PutClientDeployment(ctx, deployment)
}

func (s *failingStore) UpdateAgentNodeName(ctx context.Context, agentID string, nodeName string) error {
	if s.updateAgentNodeNameErr != nil {
		return s.updateAgentNodeNameErr
	}

	return s.MigrationStore.UpdateAgentNodeName(ctx, agentID, nodeName)
}

func (s *failingStore) DeleteAgent(ctx context.Context, agentID string) error {
	if s.deleteAgentErr != nil {
		return s.deleteAgentErr
	}

	return s.MigrationStore.DeleteAgent(ctx, agentID)
}

func (s *failingStore) DeleteInstancesByAgent(ctx context.Context, agentID string) error {
	if s.deleteInstancesByAgentErr != nil {
		return s.deleteInstancesByAgentErr
	}

	return s.MigrationStore.DeleteInstancesByAgent(ctx, agentID)
}

// DB forwards the underlying store's *sql.DB so that lifecycle.go's
// initStoreBackedSubsystems can detect DB availability and build UoW /
// discoveredRepo even when the concrete store is wrapped in failingStore.
func (s *failingStore) DB() *sql.DB {
	type dbExposer interface {
		DB() *sql.DB
	}
	if e, ok := s.MigrationStore.(dbExposer); ok {
		return e.DB()
	}
	return nil
}
