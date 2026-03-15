package storagetest

import (
	"context"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
)

// OpenStore constructs a fresh storage backend for one contract test run.
type OpenStore func(t *testing.T) storage.Store

// RunStoreContract verifies that a storage backend satisfies the shared persistence contract.
func RunStoreContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("user create and load round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		user := storage.UserRecord{
			ID:           "user-000001",
			Username:     "admin",
			PasswordHash: "argon2id$hash",
			Role:         "admin",
			TotpSecret:   "SECRET",
			CreatedAt:    time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC),
		}

		if err := store.PutUser(ctx, user); err != nil {
			t.Fatalf("PutUser() error = %v", err)
		}

		byUsername, err := store.GetUserByUsername(ctx, user.Username)
		if err != nil {
			t.Fatalf("GetUserByUsername() error = %v", err)
		}

		if byUsername.ID != user.ID {
			t.Fatalf("GetUserByUsername() ID = %q, want %q", byUsername.ID, user.ID)
		}

		byID, err := store.GetUserByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetUserByID() error = %v", err)
		}

		if byID.Username != user.Username {
			t.Fatalf("GetUserByID() Username = %q, want %q", byID.Username, user.Username)
		}

		users, err := store.ListUsers(ctx)
		if err != nil {
			t.Fatalf("ListUsers() error = %v", err)
		}

		if len(users) != 1 {
			t.Fatalf("len(ListUsers()) = %d, want 1", len(users))
		}
	})

	t.Run("enrollment token create and use round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		token := storage.EnrollmentTokenRecord{
			Value:         "token-value",
			EnvironmentID: "prod",
			FleetGroupID:  "default",
			IssuedAt:      time.Date(2026, time.March, 15, 8, 5, 0, 0, time.UTC),
			ExpiresAt:     time.Date(2026, time.March, 15, 8, 15, 0, 0, time.UTC),
		}

		if err := store.PutEnrollmentToken(ctx, token); err != nil {
			t.Fatalf("PutEnrollmentToken() error = %v", err)
		}

		stored, err := store.GetEnrollmentToken(ctx, token.Value)
		if err != nil {
			t.Fatalf("GetEnrollmentToken() error = %v", err)
		}

		if stored.EnvironmentID != token.EnvironmentID {
			t.Fatalf("GetEnrollmentToken() EnvironmentID = %q, want %q", stored.EnvironmentID, token.EnvironmentID)
		}

		consumedAt := time.Date(2026, time.March, 15, 8, 10, 0, 0, time.UTC)
		consumed, err := store.ConsumeEnrollmentToken(ctx, token.Value, consumedAt)
		if err != nil {
			t.Fatalf("ConsumeEnrollmentToken() error = %v", err)
		}

		if consumed.ConsumedAt == nil || !consumed.ConsumedAt.Equal(consumedAt) {
			t.Fatalf("ConsumeEnrollmentToken() ConsumedAt = %v, want %v", consumed.ConsumedAt, consumedAt)
		}
	})

	t.Run("agent and instance snapshot persistence round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		environment := storage.EnvironmentRecord{
			ID:        "prod",
			Name:      "Production",
			CreatedAt: time.Date(2026, time.March, 15, 8, 20, 0, 0, time.UTC),
		}
		group := storage.FleetGroupRecord{
			ID:            "default",
			EnvironmentID: environment.ID,
			Name:          "Default",
			CreatedAt:     environment.CreatedAt,
		}
		agent := storage.AgentRecord{
			ID:            "agent-000001",
			NodeName:      "node-a",
			EnvironmentID: environment.ID,
			FleetGroupID:  group.ID,
			Version:       "dev",
			ReadOnly:      false,
			LastSeenAt:    time.Date(2026, time.March, 15, 8, 25, 0, 0, time.UTC),
		}
		instance := storage.InstanceRecord{
			ID:                "instance-000001",
			AgentID:           agent.ID,
			Name:              "telemt-main",
			Version:           "1.0.0",
			ConfigFingerprint: "cfg-1",
			ConnectedUsers:    42,
			ReadOnly:          false,
			UpdatedAt:         agent.LastSeenAt,
		}

		if err := store.PutEnvironment(ctx, environment); err != nil {
			t.Fatalf("PutEnvironment() error = %v", err)
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}

		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		if err := store.PutInstance(ctx, instance); err != nil {
			t.Fatalf("PutInstance() error = %v", err)
		}

		agents, err := store.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents() error = %v", err)
		}

		if len(agents) != 1 {
			t.Fatalf("len(ListAgents()) = %d, want 1", len(agents))
		}

		instances, err := store.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances() error = %v", err)
		}

		if len(instances) != 1 {
			t.Fatalf("len(ListInstances()) = %d, want 1", len(instances))
		}

		if instances[0].AgentID != agent.ID {
			t.Fatalf("ListInstances()[0].AgentID = %q, want %q", instances[0].AgentID, agent.ID)
		}
	})

	t.Run("job and job target persistence round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		job := storage.JobRecord{
			ID:             "job-000001",
			Action:         "runtime.reload",
			ActorID:        "user-000001",
			Status:         "queued",
			CreatedAt:      time.Date(2026, time.March, 15, 8, 30, 0, 0, time.UTC),
			TTL:            time.Minute,
			IdempotencyKey: "reload-1",
		}
		target := storage.JobTargetRecord{
			JobID:      job.ID,
			AgentID:    "agent-000001",
			Status:     "queued",
			UpdatedAt:  job.CreatedAt,
			ResultText: "",
		}

		if err := store.PutJob(ctx, job); err != nil {
			t.Fatalf("PutJob() error = %v", err)
		}

		if err := store.PutJobTarget(ctx, target); err != nil {
			t.Fatalf("PutJobTarget() error = %v", err)
		}

		storedJob, err := store.GetJobByIdempotencyKey(ctx, job.IdempotencyKey)
		if err != nil {
			t.Fatalf("GetJobByIdempotencyKey() error = %v", err)
		}

		if storedJob.ID != job.ID {
			t.Fatalf("GetJobByIdempotencyKey() ID = %q, want %q", storedJob.ID, job.ID)
		}

		targets, err := store.ListJobTargets(ctx, job.ID)
		if err != nil {
			t.Fatalf("ListJobTargets() error = %v", err)
		}

		if len(targets) != 1 {
			t.Fatalf("len(ListJobTargets()) = %d, want 1", len(targets))
		}
	})

	t.Run("audit append and list round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		event := storage.AuditEventRecord{
			ID:        "audit-000001",
			ActorID:   "user-000001",
			Action:    "auth.login",
			TargetID:  "session-000001",
			CreatedAt: time.Date(2026, time.March, 15, 8, 35, 0, 0, time.UTC),
			Details: map[string]any{
				"username": "admin",
			},
		}

		if err := store.AppendAuditEvent(ctx, event); err != nil {
			t.Fatalf("AppendAuditEvent() error = %v", err)
		}

		events, err := store.ListAuditEvents(ctx)
		if err != nil {
			t.Fatalf("ListAuditEvents() error = %v", err)
		}

		if len(events) != 1 {
			t.Fatalf("len(ListAuditEvents()) = %d, want 1", len(events))
		}
	})

	t.Run("metric append and list round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		snapshot := storage.MetricSnapshotRecord{
			ID:         "metric-000001",
			AgentID:    "agent-000001",
			InstanceID: "instance-000001",
			CapturedAt: time.Date(2026, time.March, 15, 8, 40, 0, 0, time.UTC),
			Values: map[string]uint64{
				"connected_users": 42,
			},
		}

		if err := store.AppendMetricSnapshot(ctx, snapshot); err != nil {
			t.Fatalf("AppendMetricSnapshot() error = %v", err)
		}

		snapshots, err := store.ListMetricSnapshots(ctx)
		if err != nil {
			t.Fatalf("ListMetricSnapshots() error = %v", err)
		}

		if len(snapshots) != 1 {
			t.Fatalf("len(ListMetricSnapshots()) = %d, want 1", len(snapshots))
		}
	})
}
