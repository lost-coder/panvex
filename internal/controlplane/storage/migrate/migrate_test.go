package migrate

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestRunRejectsNonSQLiteSource(t *testing.T) {
	if _, err := Run(context.Background(), Options{
		SourceDriver: "postgres",
		SourceDSN:    "postgres://source",
		TargetDriver: "postgres",
		TargetDSN:    "postgres://target",
	}); err == nil {
		t.Fatal("Run() error = nil, want source driver validation failure")
	}
}

func TestRunRejectsNonPostgresTarget(t *testing.T) {
	if _, err := Run(context.Background(), Options{
		SourceDriver: "sqlite",
		SourceDSN:    "source.db",
		TargetDriver: "sqlite",
		TargetDSN:    "target.db",
	}); err == nil {
		t.Fatal("Run() error = nil, want target driver validation failure")
	}
}

func TestCopyRejectsNonEmptyTarget(t *testing.T) {
	source := openSQLiteStore(t, filepath.Join(t.TempDir(), "source.db"))
	defer source.Close()
	target := openSQLiteStore(t, filepath.Join(t.TempDir(), "target.db"))
	defer target.Close()

	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	if err := source.PutUser(context.Background(), storage.UserRecord{
		ID:           "user-000001",
		Username:     "admin",
		PasswordHash: "hash",
		Role:         "admin",
		TotpEnabled:  true,
		TotpSecret:   "secret",
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("source.PutUser() error = %v", err)
	}
	if err := target.PutUser(context.Background(), storage.UserRecord{
		ID:           "user-000002",
		Username:     "existing",
		PasswordHash: "hash",
		Role:         "admin",
		TotpEnabled:  true,
		TotpSecret:   "secret",
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("target.PutUser() error = %v", err)
	}

	if _, err := copyStore(context.Background(), source, target); err == nil {
		t.Fatal("copyStore() error = nil, want target emptiness failure")
	}
}

func TestCopyStoreCopiesAllPersistentEntities(t *testing.T) {
	source := openSQLiteStore(t, filepath.Join(t.TempDir(), "source.db"))
	defer source.Close()
	target := openSQLiteStore(t, filepath.Join(t.TempDir(), "target.db"))
	defer target.Close()

	now := time.Date(2026, time.March, 15, 12, 0, 0, 0, time.UTC)
	populateSourceStore(t, source, now)

	summary, err := copyStore(context.Background(), source, target)
	if err != nil {
		t.Fatalf("copyStore() error = %v", err)
	}

	if summary.Users != 1 || summary.Environments != 1 || summary.FleetGroups != 1 {
		t.Fatalf("summary = %+v, want copied user/environment/group counts", summary)
	}
	if summary.Agents != 1 || summary.Instances != 1 || summary.Jobs != 1 || summary.JobTargets != 1 {
		t.Fatalf("summary = %+v, want copied agent/instance/job counts", summary)
	}
	if summary.AuditEvents != 1 || summary.MetricSnapshots != 1 || summary.EnrollmentTokens != 1 {
		t.Fatalf("summary = %+v, want copied audit/metric/token counts", summary)
	}

	users, err := target.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("target.ListUsers() error = %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("len(target.ListUsers()) = %d, want %d", len(users), 1)
	}

	if !users[0].TotpEnabled {
		t.Fatal("target.ListUsers()[0].TotpEnabled = false, want true")
	}

	jobs, err := target.ListJobs(context.Background())
	if err != nil {
		t.Fatalf("target.ListJobs() error = %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(target.ListJobs()) = %d, want %d", len(jobs), 1)
	}

	jobTargets, err := target.ListJobTargets(context.Background(), "job-000001")
	if err != nil {
		t.Fatalf("target.ListJobTargets() error = %v", err)
	}
	if len(jobTargets) != 1 {
		t.Fatalf("len(target.ListJobTargets()) = %d, want %d", len(jobTargets), 1)
	}

	tokens, err := target.GetEnrollmentToken(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("target.GetEnrollmentToken() error = %v", err)
	}
	if tokens.ConsumedAt == nil {
		t.Fatal("target enrollment token ConsumedAt = nil, want copied consumed timestamp")
	}
}

func openSQLiteStore(t *testing.T, path string) storage.Store {
	t.Helper()

	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}

	return store
}

func populateSourceStore(t *testing.T, store storage.Store, now time.Time) {
	t.Helper()

	ctx := context.Background()
	consumedAt := now.Add(30 * time.Second)

	if err := store.PutUser(ctx, storage.UserRecord{
		ID:           "user-000001",
		Username:     "admin",
		PasswordHash: "hash",
		Role:         "admin",
		TotpEnabled:  true,
		TotpSecret:   "secret",
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("PutUser() error = %v", err)
	}
	if err := store.PutEnvironment(ctx, storage.EnvironmentRecord{
		ID:        "prod",
		Name:      "prod",
		CreatedAt: now,
	}); err != nil {
		t.Fatalf("PutEnvironment() error = %v", err)
	}
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:            "ams-1",
		EnvironmentID: "prod",
		Name:          "ams-1",
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:            "agent-000001",
		NodeName:      "node-a",
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		Version:       "1.0.0",
		LastSeenAt:    now,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
	if err := store.PutInstance(ctx, storage.InstanceRecord{
		ID:                "instance-1",
		AgentID:           "agent-000001",
		Name:              "telemt-a",
		Version:           "2026.03",
		ConfigFingerprint: "cfg-1",
		ConnectedUsers:    42,
		UpdatedAt:         now,
	}); err != nil {
		t.Fatalf("PutInstance() error = %v", err)
	}
	if err := store.PutJob(ctx, storage.JobRecord{
		ID:             "job-000001",
		Action:         "runtime.reload",
		ActorID:        "user-000001",
		Status:         "failed",
		CreatedAt:      now,
		TTL:            time.Minute,
		IdempotencyKey: "reload-1",
	}); err != nil {
		t.Fatalf("PutJob() error = %v", err)
	}
	if err := store.PutJobTarget(ctx, storage.JobTargetRecord{
		JobID:      "job-000001",
		AgentID:    "agent-000001",
		Status:     "failed",
		ResultText: "reload failed",
		UpdatedAt:  now.Add(10 * time.Second),
	}); err != nil {
		t.Fatalf("PutJobTarget() error = %v", err)
	}
	if err := store.AppendAuditEvent(ctx, storage.AuditEventRecord{
		ID:        "audit-000001",
		ActorID:   "user-000001",
		Action:    "jobs.create",
		TargetID:  "job-000001",
		CreatedAt: now,
		Details: map[string]any{
			"action": "runtime.reload",
		},
	}); err != nil {
		t.Fatalf("AppendAuditEvent() error = %v", err)
	}
	if err := store.AppendMetricSnapshot(ctx, storage.MetricSnapshotRecord{
		ID:         "metric-000001",
		AgentID:    "agent-000001",
		CapturedAt: now,
		Values: map[string]uint64{
			"requests_total": 128,
		},
	}); err != nil {
		t.Fatalf("AppendMetricSnapshot() error = %v", err)
	}
	if err := store.PutEnrollmentToken(ctx, storage.EnrollmentTokenRecord{
		Value:         "token-1",
		EnvironmentID: "prod",
		FleetGroupID:  "ams-1",
		IssuedAt:      now,
		ExpiresAt:     now.Add(time.Minute),
		ConsumedAt:    &consumedAt,
	}); err != nil {
		t.Fatalf("PutEnrollmentToken() error = %v", err)
	}
}
