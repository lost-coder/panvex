package server

import (
	"context"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

type failingStore struct {
	storage.Store

	putAgentErr             error
	listAgentsErr           error
	listUsersErr            error
	putInstanceErr          error
	appendMetricSnapshotErr error
	appendAuditEventErr     error
	putClientErr            error
	putClientAssignmentErr  error
	putClientDeploymentErr  error
}

func (s *failingStore) PutAgent(ctx context.Context, agent storage.AgentRecord) error {
	if s.putAgentErr != nil {
		return s.putAgentErr
	}

	return s.Store.PutAgent(ctx, agent)
}

func (s *failingStore) ListAgents(ctx context.Context) ([]storage.AgentRecord, error) {
	if s.listAgentsErr != nil {
		return nil, s.listAgentsErr
	}

	return s.Store.ListAgents(ctx)
}

func (s *failingStore) ListUsers(ctx context.Context) ([]storage.UserRecord, error) {
	if s.listUsersErr != nil {
		return nil, s.listUsersErr
	}

	return s.Store.ListUsers(ctx)
}

func (s *failingStore) PutInstance(ctx context.Context, instance storage.InstanceRecord) error {
	if s.putInstanceErr != nil {
		return s.putInstanceErr
	}

	return s.Store.PutInstance(ctx, instance)
}

func (s *failingStore) AppendMetricSnapshot(ctx context.Context, snapshot storage.MetricSnapshotRecord) error {
	if s.appendMetricSnapshotErr != nil {
		return s.appendMetricSnapshotErr
	}

	return s.Store.AppendMetricSnapshot(ctx, snapshot)
}

func (s *failingStore) AppendAuditEvent(ctx context.Context, event storage.AuditEventRecord) error {
	if s.appendAuditEventErr != nil {
		return s.appendAuditEventErr
	}

	return s.Store.AppendAuditEvent(ctx, event)
}

func (s *failingStore) PutClient(ctx context.Context, client storage.ClientRecord) error {
	if s.putClientErr != nil {
		return s.putClientErr
	}

	return s.Store.PutClient(ctx, client)
}

func (s *failingStore) PutClientAssignment(ctx context.Context, assignment storage.ClientAssignmentRecord) error {
	if s.putClientAssignmentErr != nil {
		return s.putClientAssignmentErr
	}

	return s.Store.PutClientAssignment(ctx, assignment)
}

func (s *failingStore) PutClientDeployment(ctx context.Context, deployment storage.ClientDeploymentRecord) error {
	if s.putClientDeploymentErr != nil {
		return s.putClientDeploymentErr
	}

	return s.Store.PutClientDeployment(ctx, deployment)
}
