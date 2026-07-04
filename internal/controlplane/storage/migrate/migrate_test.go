package migrate

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
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

	if summary.Users != 1 || summary.FleetGroups != 1 {
		t.Fatalf("summary = %+v, want copied user/group counts", summary)
	}
	if summary.UserAppearance != 1 {
		t.Fatalf("summary.UserAppearance = %d, want %d", summary.UserAppearance, 1)
	}
	if summary.Agents != 1 || summary.Instances != 1 || summary.Jobs != 1 || summary.JobTargets != 1 {
		t.Fatalf("summary = %+v, want copied agent/instance/job counts", summary)
	}
	if summary.AuditEvents != 1 || summary.MetricSnapshots != 1 || summary.EnrollmentTokens != 1 || summary.AgentCertificateRecoveryGrants != 1 {
		t.Fatalf("summary = %+v, want copied audit/metric/token/recovery counts", summary)
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

	appearance, err := target.GetUserAppearance(context.Background(), "user-000001")
	if err != nil {
		t.Fatalf("target.GetUserAppearance() error = %v", err)
	}
	if appearance.Theme != "dark" {
		t.Fatalf("target.GetUserAppearance() Theme = %q, want %q", appearance.Theme, "dark")
	}
	if appearance.Density != "compact" {
		t.Fatalf("target.GetUserAppearance() Density = %q, want %q", appearance.Density, "compact")
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
	if tokens.RevokedAt == nil {
		t.Fatal("target enrollment token RevokedAt = nil, want copied revoked timestamp")
	}

	grant, err := target.GetAgentCertificateRecoveryGrant(context.Background(), "agent-000001")
	if err != nil {
		t.Fatalf("target.GetAgentCertificateRecoveryGrant() error = %v", err)
	}
	if grant.IssuedBy != "user-000001" {
		t.Fatalf("target.GetAgentCertificateRecoveryGrant() IssuedBy = %q, want %q", grant.IssuedBy, "user-000001")
	}
	if grant.UsedAt == nil {
		t.Fatal("target agent recovery grant UsedAt = nil, want copied used timestamp")
	}

	authority, err := target.GetCertificateAuthority(context.Background())
	if err != nil {
		t.Fatalf("target.GetCertificateAuthority() error = %v", err)
	}
	if authority.CAPEM != "ca-pem" {
		t.Fatalf("target.GetCertificateAuthority() CAPEM = %q, want %q", authority.CAPEM, "ca-pem")
	}
	if authority.PrivateKeyPEM != "ca-key-pem" {
		t.Fatalf("target.GetCertificateAuthority() PrivateKeyPEM = %q, want %q", authority.PrivateKeyPEM, "ca-key-pem")
	}

	assertExtraTableCounts(t, summary)
	assertExtraTableRoundTrip(t, target)
}

func assertExtraTableCounts(t *testing.T, summary Summary) {
	t.Helper()

	checks := []struct {
		name string
		got  int
	}{
		{"AgentRevocations", summary.AgentRevocations},
		{"AgentFallbackState", summary.AgentFallbackState},
		{"IntegrationProviders", summary.IntegrationProviders},
		{"FleetGroupIntegrations", summary.FleetGroupIntegrations},
		{"UserFleetGroupScopes", summary.UserFleetGroupScopes},
		{"ClientUsage", summary.ClientUsage},
		{"ClientIPHistory", summary.ClientIPHistory},
		{"Sessions", summary.Sessions},
		{"CPSecrets", summary.CPSecrets},
		{"DiscoveredClients", summary.DiscoveredClients},
		{"WebhookEndpoints", summary.WebhookEndpoints},
		{"WebhookOutbox", summary.WebhookOutbox},
		{"RuntimeSettings", summary.RuntimeSettings},
	}
	for _, c := range checks {
		if c.got != 1 {
			t.Errorf("summary.%s = %d, want 1", c.name, c.got)
		}
	}
	if summary.UpdateConfig != 2 {
		t.Errorf("summary.UpdateConfig = %d, want 2 (settings + geoip_state)", summary.UpdateConfig)
	}
}

func assertExtraTableRoundTrip(t *testing.T, target storage.MigrationStore) {
	t.Helper()

	ctx := context.Background()

	revocations, err := target.ListAgentRevocations(ctx)
	if err != nil || len(revocations) != 1 {
		t.Fatalf("target.ListAgentRevocations() = %+v, err = %v; want 1 row", revocations, err)
	}

	fallback, err := target.GetAgentFallbackState(ctx, "agent-000001")
	if err != nil {
		t.Fatalf("target.GetAgentFallbackState() error = %v", err)
	}
	if fallback.AgentID != "agent-000001" {
		t.Fatalf("fallback.AgentID = %q, want agent-000001", fallback.AgentID)
	}

	providers, err := target.ListIntegrationProviders(ctx)
	if err != nil || len(providers) != 1 {
		t.Fatalf("target.ListIntegrationProviders() = %+v, err = %v; want 1", providers, err)
	}

	scopes, err := target.ListAllUserFleetGroupScopes(ctx)
	if err != nil || len(scopes) != 1 {
		t.Fatalf("target.ListAllUserFleetGroupScopes() = %+v, err = %v; want 1", scopes, err)
	}
	if scopes[0].GrantedBy != "user-000001" {
		t.Errorf("scope GrantedBy = %q, want user-000001 (provenance lost)", scopes[0].GrantedBy)
	}

	usage, err := target.ListClientUsage(ctx)
	if err != nil || len(usage) != 1 {
		t.Fatalf("target.ListClientUsage() = %+v, err = %v; want 1", usage, err)
	}
	if usage[0].TrafficUsedBytes != 4096 {
		t.Errorf("usage TrafficUsedBytes = %d, want 4096", usage[0].TrafficUsedBytes)
	}

	session, err := target.GetSession(ctx, "sess-000001")
	if err != nil {
		t.Fatalf("target.GetSession() error = %v", err)
	}
	if session.UserID != "user-000001" {
		t.Errorf("session UserID = %q, want user-000001", session.UserID)
	}

	secret, err := target.GetCPSecret(ctx, "csrf_seed")
	if err != nil {
		t.Fatalf("target.GetCPSecret() error = %v", err)
	}
	if string(secret) != string([]byte{0x00, 0x01, 0x02, 0xff}) {
		t.Errorf("cp_secret value = %v, want raw bytes copied verbatim", secret)
	}

	settings, err := target.GetUpdateSettings(ctx)
	if err != nil {
		t.Fatalf("target.GetUpdateSettings() error = %v", err)
	}
	if string(settings) != `{"channel":"stable"}` {
		t.Errorf("update settings = %q, want copied JSON", settings)
	}

	// Raw-copy tables: assert ciphertext copied verbatim.
	db := target.(rawDBStore).DB()
	var ciphertext string
	if err := db.QueryRowContext(ctx, `SELECT secret_ciphertext FROM webhook_endpoints WHERE id = ?`, "wh-000001").Scan(&ciphertext); err != nil {
		t.Fatalf("query target webhook_endpoints: %v", err)
	}
	if ciphertext != "CIPHERTEXT-VERBATIM" {
		t.Errorf("webhook ciphertext = %q, want verbatim copy", ciphertext)
	}

	var payload string
	if err := db.QueryRowContext(ctx, `SELECT payload FROM webhook_outbox WHERE id = ?`, "ob-000001").Scan(&payload); err != nil {
		t.Fatalf("query target webhook_outbox: %v", err)
	}
	if payload != `{"id":"job-1"}` {
		t.Errorf("webhook_outbox payload = %q, want verbatim copy", payload)
	}

	var valueJSON string
	if err := db.QueryRowContext(ctx, `SELECT value_json FROM runtime_settings WHERE name = ?`, "agents.presence_degraded_after").Scan(&valueJSON); err != nil {
		t.Fatalf("query target runtime_settings: %v", err)
	}
	if valueJSON != `"30s"` {
		t.Errorf("runtime_settings value_json = %q, want verbatim copy", valueJSON)
	}
}

func TestCopyStoreCopiesUserAppearanceWithZeroUpdatedAt(t *testing.T) {
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
	if err := source.PutUserAppearance(context.Background(), storage.UserAppearanceRecord{
		UserID:  "user-000001",
		Theme:   "dark",
		Density: "comfortable",
	}); err != nil {
		t.Fatalf("source.PutUserAppearance() error = %v", err)
	}

	summary, err := copyStore(context.Background(), source, target)
	if err != nil {
		t.Fatalf("copyStore() error = %v", err)
	}
	if summary.UserAppearance != 1 {
		t.Fatalf("summary.UserAppearance = %d, want %d", summary.UserAppearance, 1)
	}

	appearance, err := target.GetUserAppearance(context.Background(), "user-000001")
	if err != nil {
		t.Fatalf("target.GetUserAppearance() error = %v", err)
	}
	if appearance.Theme != "dark" {
		t.Fatalf("target.GetUserAppearance() Theme = %q, want %q", appearance.Theme, "dark")
	}
}

func openSQLiteStore(t *testing.T, path string) storage.MigrationStore {
	t.Helper()

	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}

	return store
}

func populateSourceStore(t *testing.T, store storage.MigrationStore, now time.Time) {
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
	if err := store.PutUserAppearance(ctx, storage.UserAppearanceRecord{
		UserID:    "user-000001",
		Theme:     "dark",
		Density:   "compact",
		UpdatedAt: now.Add(5 * time.Second),
	}); err != nil {
		t.Fatalf("PutUserAppearance() error = %v", err)
	}
	fleetGroupID := uuid.NewString()
	if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
		ID:        fleetGroupID,
		Name:      "ams-1",
		Label:     "ams-1",
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("PutFleetGroup() error = %v", err)
	}
	if err := store.PutAgent(ctx, storage.AgentRecord{
		ID:           "agent-000001",
		NodeName:     "node-a",
		FleetGroupID: fleetGroupID,
		Version:      "1.0.0",
		LastSeenAt:   now,
	}); err != nil {
		t.Fatalf("PutAgent() error = %v", err)
	}
	if err := store.PutInstance(ctx, storage.InstanceRecord{
		ID:                "instance-1",
		AgentID:           "agent-000001",
		Name:              "telemt-a",
		Version:           "2026.03",
		ConfigFingerprint: "cfg-1",
		Connections:       42,
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
		PayloadJSON:    `{"scope":"telemt"}`,
	}); err != nil {
		t.Fatalf("PutJob() error = %v", err)
	}
	if err := store.PutJobTarget(ctx, storage.JobTargetRecord{
		JobID:      "job-000001",
		AgentID:    "agent-000001",
		Status:     "failed",
		ResultText: "reload failed",
		ResultJSON: `{"accepted":false}`,
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
		Value:        "token-1",
		FleetGroupID: fleetGroupID,
		IssuedAt:     now,
		ExpiresAt:    now.Add(time.Minute),
		ConsumedAt:   &consumedAt,
		RevokedAt:    &consumedAt,
	}); err != nil {
		t.Fatalf("PutEnrollmentToken() error = %v", err)
	}
	if err := store.PutAgentCertificateRecoveryGrant(ctx, storage.AgentCertificateRecoveryGrantRecord{
		AgentID:   "agent-000001",
		IssuedBy:  "user-000001",
		IssuedAt:  now.Add(10 * time.Second),
		ExpiresAt: now.Add(5 * time.Minute),
		UsedAt:    &consumedAt,
	}); err != nil {
		t.Fatalf("PutAgentCertificateRecoveryGrant() error = %v", err)
	}
	if err := store.PutCertificateAuthority(ctx, storage.CertificateAuthorityRecord{
		CAPEM:         "ca-pem",
		PrivateKeyPEM: "ca-key-pem",
		UpdatedAt:     now.Add(15 * time.Second),
	}); err != nil {
		t.Fatalf("PutCertificateAuthority() error = %v", err)
	}

	populateExtraTables(t, store, fleetGroupID, now)
}

// populateExtraTables seeds one row into each table the L-5 migration
// expansion added (Tier 1 + Tier 2 typed tables and the raw-copy tables).
func populateExtraTables(t *testing.T, store storage.MigrationStore, fleetGroupID string, now time.Time) {
	t.Helper()

	ctx := context.Background()
	consumedAt := now.Add(30 * time.Second)

	if err := store.PutClient(ctx, storage.ClientRecord{
		ID:               "client-000001",
		Name:             "vip",
		SecretCiphertext: "secret-ct",
		Enabled:          true,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("PutClient() error = %v", err)
	}
	if err := store.PutDiscoveredClient(ctx, storage.DiscoveredClientRecord{
		ID:           "disc-000001",
		AgentID:      "agent-000001",
		ClientName:   "found-1",
		Secret:       "found-secret",
		Status:       "pending_review",
		DiscoveredAt: now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("PutDiscoveredClient() error = %v", err)
	}
	if err := store.PutAgentRevocation(ctx, storage.AgentRevocationRecord{
		AgentID:       "agent-000001",
		RevokedAt:     now,
		CertExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("PutAgentRevocation() error = %v", err)
	}
	if err := store.PutAgentFallbackState(ctx, storage.AgentFallbackStateRecord{
		AgentID:   "agent-000001",
		EnteredAt: now,
	}); err != nil {
		t.Fatalf("PutAgentFallbackState() error = %v", err)
	}
	if err := store.CreateIntegrationProvider(ctx, storage.IntegrationProviderRecord{
		ID:        "prov-000001",
		Kind:      "cloudflare",
		Label:     "cf-main",
		Config:    []byte(`{"token":"x"}`),
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("CreateIntegrationProvider() error = %v", err)
	}
	providerID := "prov-000001"
	if err := store.CreateFleetGroupIntegration(ctx, storage.FleetGroupIntegrationRecord{
		ID:           "fgi-000001",
		FleetGroupID: fleetGroupID,
		Kind:         "cloudflare",
		ProviderID:   &providerID,
		Config:       []byte(`{"zone":"z"}`),
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("CreateFleetGroupIntegration() error = %v", err)
	}
	if err := store.SetUserFleetGroupScopes(ctx, "user-000001", []string{fleetGroupID}, "user-000001", now); err != nil {
		t.Fatalf("SetUserFleetGroupScopes() error = %v", err)
	}
	if err := store.UpsertClientUsage(ctx, storage.ClientUsageRecord{
		ClientID:         "client-000001",
		AgentID:          "agent-000001",
		TrafficUsedBytes: 4096,
		UniqueIPsUsed:    3,
		AgentBootID:      "boot-1",
		LastTotalBytes:   7,
		ObservedAt:       now,
	}); err != nil {
		t.Fatalf("UpsertClientUsage() error = %v", err)
	}
	if err := store.UpsertClientIPHistory(ctx, storage.ClientIPHistoryRecord{
		AgentID:   "agent-000001",
		ClientID:  "client-000001",
		IPAddress: "203.0.113.7",
		FirstSeen: now,
		LastSeen:  consumedAt,
	}); err != nil {
		t.Fatalf("UpsertClientIPHistory() error = %v", err)
	}
	if err := store.PutSession(ctx, storage.SessionRecord{
		ID:         "sess-000001",
		UserID:     "user-000001",
		CreatedAt:  now,
		LastSeenAt: consumedAt,
	}); err != nil {
		t.Fatalf("PutSession() error = %v", err)
	}
	if err := store.PutCPSecret(ctx, "csrf_seed", []byte{0x00, 0x01, 0x02, 0xff}); err != nil {
		t.Fatalf("PutCPSecret() error = %v", err)
	}
	if err := store.PutUpdateSettings(ctx, []byte(`{"channel":"stable"}`)); err != nil {
		t.Fatalf("PutUpdateSettings() error = %v", err)
	}
	if err := store.PutGeoIPState(ctx, []byte(`{"db":"loaded"}`)); err != nil {
		t.Fatalf("PutGeoIPState() error = %v", err)
	}

	populateRawTables(t, store, now)
}

// populateRawTables seeds the raw-copy tables (ciphertext / registry
// tables) directly via SQL since they live outside the typed
// MigrationStore surface.
func populateRawTables(t *testing.T, store storage.MigrationStore, now time.Time) {
	t.Helper()

	db := store.(rawDBStore).DB()
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `
		INSERT INTO webhook_endpoints
			(id, name, url, secret_ciphertext, event_filter, allow_private, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "wh-000001", "alerts", "https://example.test/hook", "CIPHERTEXT-VERBATIM", "jobs.*", 0, 1, now, now); err != nil {
		t.Fatalf("insert webhook_endpoints: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO webhook_outbox
			(id, endpoint_id, event_action, payload, attempt, next_attempt_at, last_error, dead, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "ob-000001", "wh-000001", "jobs.create", `{"id":"job-1"}`, 0, now, "", 0, now); err != nil {
		t.Fatalf("insert webhook_outbox: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO runtime_settings (name, value_json, updated_at, updated_by)
		VALUES (?, ?, ?, ?)
	`, "agents.presence_degraded_after", `"30s"`, now.Unix(), "admin"); err != nil {
		t.Fatalf("insert runtime_settings: %v", err)
	}
}
