package storagetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// OpenStore constructs a fresh storage backend for one contract test run.
type OpenStore func(t *testing.T) storage.Store

// RunStoreContract verifies that a storage backend satisfies the shared persistence contract.
func RunStoreContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("client, assignment, and deployment round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 17, 8, 51, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-000001",
			NodeName:     "node-a",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 17, 8, 52, 0, 0, time.UTC),
		}
		deletedAt := time.Date(2026, time.March, 17, 9, 25, 0, 0, time.UTC)
		lastAppliedAt := time.Date(2026, time.March, 17, 9, 20, 0, 0, time.UTC)
		client := storage.ClientRecord{
			ID:               "client-000001",
			Name:             "alice",
			SecretCiphertext: "enc:alice-secret",
			UserADTag:        "0123456789abcdef0123456789abcdef",
			Enabled:          true,
			MaxTCPConns:      4,
			MaxUniqueIPs:     2,
			DataQuotaBytes:   1073741824,
			ExpirationRFC3339: "2026-03-31T00:00:00Z",
			CreatedAt:        time.Date(2026, time.March, 17, 9, 0, 0, 0, time.UTC),
			UpdatedAt:        time.Date(2026, time.March, 17, 9, 10, 0, 0, time.UTC),
			DeletedAt:        &deletedAt,
		}
		groupAssignment := storage.ClientAssignmentRecord{
			ID:           "assignment-000001",
			ClientID:     client.ID,
			TargetType:   "fleet_group",
			FleetGroupID: "default",
			CreatedAt:    time.Date(2026, time.March, 17, 9, 11, 0, 0, time.UTC),
		}
		nodeAssignment := storage.ClientAssignmentRecord{
			ID:         "assignment-000002",
			ClientID:   client.ID,
			TargetType: "agent",
			AgentID:    agent.ID,
			CreatedAt:  time.Date(2026, time.March, 17, 9, 12, 0, 0, time.UTC),
		}
		deployment := storage.ClientDeploymentRecord{
			ClientID:         client.ID,
			AgentID:          agent.ID,
			DesiredOperation: "client.create",
			Status:           "succeeded",
			LastError:        "",
			ConnectionLink:   "tg://proxy?server=node-a&secret=alice",
			LastAppliedAt:    &lastAppliedAt,
			UpdatedAt:        time.Date(2026, time.March, 17, 9, 21, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}

		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		if err := store.PutClient(ctx, client); err != nil {
			t.Fatalf("PutClient() error = %v", err)
		}

		if err := store.PutClientAssignment(ctx, groupAssignment); err != nil {
			t.Fatalf("PutClientAssignment(group) error = %v", err)
		}

		if err := store.PutClientAssignment(ctx, nodeAssignment); err != nil {
			t.Fatalf("PutClientAssignment(node) error = %v", err)
		}

		if err := store.PutClientDeployment(ctx, deployment); err != nil {
			t.Fatalf("PutClientDeployment() error = %v", err)
		}

		storedClient, err := store.GetClientByID(ctx, client.ID)
		if err != nil {
			t.Fatalf("GetClientByID() error = %v", err)
		}

		if storedClient.Name != client.Name {
			t.Fatalf("GetClientByID() Name = %q, want %q", storedClient.Name, client.Name)
		}
		if storedClient.SecretCiphertext != client.SecretCiphertext {
			t.Fatalf("GetClientByID() SecretCiphertext = %q, want %q", storedClient.SecretCiphertext, client.SecretCiphertext)
		}
		if storedClient.UserADTag != client.UserADTag {
			t.Fatalf("GetClientByID() UserADTag = %q, want %q", storedClient.UserADTag, client.UserADTag)
		}
		if storedClient.DataQuotaBytes != client.DataQuotaBytes {
			t.Fatalf("GetClientByID() DataQuotaBytes = %d, want %d", storedClient.DataQuotaBytes, client.DataQuotaBytes)
		}
		if storedClient.DeletedAt == nil || !storedClient.DeletedAt.Equal(deletedAt) {
			t.Fatalf("GetClientByID() DeletedAt = %v, want %v", storedClient.DeletedAt, deletedAt)
		}

		clients, err := store.ListClients(ctx)
		if err != nil {
			t.Fatalf("ListClients() error = %v", err)
		}
		if len(clients) != 1 {
			t.Fatalf("len(ListClients()) = %d, want 1", len(clients))
		}

		assignments, err := store.ListClientAssignments(ctx, client.ID)
		if err != nil {
			t.Fatalf("ListClientAssignments() error = %v", err)
		}
		if len(assignments) != 2 {
			t.Fatalf("len(ListClientAssignments()) = %d, want 2", len(assignments))
		}
		if err := store.DeleteClientAssignments(ctx, client.ID); err != nil {
			t.Fatalf("DeleteClientAssignments() error = %v", err)
		}
		assignments, err = store.ListClientAssignments(ctx, client.ID)
		if err != nil {
			t.Fatalf("ListClientAssignments() after delete error = %v", err)
		}
		if len(assignments) != 0 {
			t.Fatalf("len(ListClientAssignments()) after delete = %d, want 0", len(assignments))
		}

		deployments, err := store.ListClientDeployments(ctx, client.ID)
		if err != nil {
			t.Fatalf("ListClientDeployments() error = %v", err)
		}
		if len(deployments) != 1 {
			t.Fatalf("len(ListClientDeployments()) = %d, want 1", len(deployments))
		}
		if deployments[0].ConnectionLink != deployment.ConnectionLink {
			t.Fatalf("ListClientDeployments()[0].ConnectionLink = %q, want %q", deployments[0].ConnectionLink, deployment.ConnectionLink)
		}
		if deployments[0].LastAppliedAt == nil || !deployments[0].LastAppliedAt.Equal(lastAppliedAt) {
			t.Fatalf("ListClientDeployments()[0].LastAppliedAt = %v, want %v", deployments[0].LastAppliedAt, lastAppliedAt)
		}
	})

	t.Run("panel settings round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		settings := storage.PanelSettingsRecord{
			HTTPPublicURL:      "https://panel.example.com",
			GRPCPublicEndpoint: "panel.example.com:8443",
			UpdatedAt:          time.Date(2026, time.March, 16, 18, 0, 0, 0, time.UTC),
		}

		if err := store.PutPanelSettings(ctx, settings); err != nil {
			t.Fatalf("PutPanelSettings() error = %v", err)
		}

		stored, err := store.GetPanelSettings(ctx)
		if err != nil {
			t.Fatalf("GetPanelSettings() error = %v", err)
		}

		if stored.HTTPPublicURL != settings.HTTPPublicURL {
			t.Fatalf("GetPanelSettings() HTTPPublicURL = %q, want %q", stored.HTTPPublicURL, settings.HTTPPublicURL)
		}
		if stored.GRPCPublicEndpoint != settings.GRPCPublicEndpoint {
			t.Fatalf("GetPanelSettings() GRPCPublicEndpoint = %q, want %q", stored.GRPCPublicEndpoint, settings.GRPCPublicEndpoint)
		}
		if !stored.UpdatedAt.Equal(settings.UpdatedAt) {
			t.Fatalf("GetPanelSettings() UpdatedAt = %v, want %v", stored.UpdatedAt, settings.UpdatedAt)
		}
	})

	t.Run("retention settings round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()

		// An unwritten store must report ErrNotFound so the caller
		// (server.New) can fall back to defaults.
		if _, err := store.GetRetentionSettings(ctx); err == nil {
			t.Fatalf("GetRetentionSettings() on empty store = nil error, want ErrNotFound")
		}

		settings := storage.RetentionSettings{
			TSRawSeconds:          7200,
			TSHourlySeconds:       86400,
			TSDCSeconds:           3600,
			IPHistorySeconds:      1209600,
			EventSeconds:          3600,
			AuditEventSeconds:     2592000,
			MetricSnapshotSeconds: 604800,
		}

		if err := store.PutRetentionSettings(ctx, settings); err != nil {
			t.Fatalf("PutRetentionSettings() error = %v", err)
		}

		stored, err := store.GetRetentionSettings(ctx)
		if err != nil {
			t.Fatalf("GetRetentionSettings() error = %v", err)
		}

		if stored != settings {
			t.Fatalf("GetRetentionSettings() = %+v, want %+v", stored, settings)
		}

		// Overwrite must replace the previous blob rather than merge.
		replacement := storage.RetentionSettings{
			TSRawSeconds:          120,
			TSHourlySeconds:       240,
			TSDCSeconds:           360,
			IPHistorySeconds:      480,
			EventSeconds:          600,
			AuditEventSeconds:     720,
			MetricSnapshotSeconds: 840,
		}
		if err := store.PutRetentionSettings(ctx, replacement); err != nil {
			t.Fatalf("PutRetentionSettings(replacement) error = %v", err)
		}
		got, err := store.GetRetentionSettings(ctx)
		if err != nil {
			t.Fatalf("GetRetentionSettings(after overwrite) error = %v", err)
		}
		if got != replacement {
			t.Fatalf("GetRetentionSettings(after overwrite) = %+v, want %+v", got, replacement)
		}
	})

	t.Run("certificate authority round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		authority := storage.CertificateAuthorityRecord{
			CAPEM:         "ca-pem",
			PrivateKeyPEM: "ca-key-pem",
			UpdatedAt:     time.Date(2026, time.March, 16, 18, 10, 0, 0, time.UTC),
		}

		if err := store.PutCertificateAuthority(ctx, authority); err != nil {
			t.Fatalf("PutCertificateAuthority() error = %v", err)
		}

		stored, err := store.GetCertificateAuthority(ctx)
		if err != nil {
			t.Fatalf("GetCertificateAuthority() error = %v", err)
		}

		if stored.CAPEM != authority.CAPEM {
			t.Fatalf("GetCertificateAuthority() CAPEM = %q, want %q", stored.CAPEM, authority.CAPEM)
		}
		if stored.PrivateKeyPEM != authority.PrivateKeyPEM {
			t.Fatalf("GetCertificateAuthority() PrivateKeyPEM = %q, want %q", stored.PrivateKeyPEM, authority.PrivateKeyPEM)
		}
		if !stored.UpdatedAt.Equal(authority.UpdatedAt) {
			t.Fatalf("GetCertificateAuthority() UpdatedAt = %v, want %v", stored.UpdatedAt, authority.UpdatedAt)
		}
	})

	t.Run("user create and load round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		user := storage.UserRecord{
			ID:           "user-000001",
			Username:     "admin",
			PasswordHash: "argon2id$hash",
			Role:         "admin",
			TotpEnabled:  true,
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

		if !byID.TotpEnabled {
			t.Fatal("GetUserByID() TotpEnabled = false, want true")
		}

		users, err := store.ListUsers(ctx)
		if err != nil {
			t.Fatalf("ListUsers() error = %v", err)
		}

		if len(users) != 1 {
			t.Fatalf("len(ListUsers()) = %d, want 1", len(users))
		}

		if !users[0].TotpEnabled {
			t.Fatal("ListUsers()[0].TotpEnabled = false, want true")
		}
	})

	t.Run("user delete removes persisted record", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		user := storage.UserRecord{
			ID:           "user-000002",
			Username:     "operator",
			PasswordHash: "argon2id$hash",
			Role:         "operator",
			TotpEnabled:  false,
			TotpSecret:   "",
			CreatedAt:    time.Date(2026, time.March, 15, 8, 10, 0, 0, time.UTC),
		}

		if err := store.PutUser(ctx, user); err != nil {
			t.Fatalf("PutUser() error = %v", err)
		}

		if err := store.DeleteUser(ctx, user.ID); err != nil {
			t.Fatalf("DeleteUser() error = %v", err)
		}

		if _, err := store.GetUserByID(ctx, user.ID); err != storage.ErrNotFound {
			t.Fatalf("GetUserByID() after DeleteUser error = %v, want %v", err, storage.ErrNotFound)
		}
	})

	t.Run("user appearance defaults and round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()

		defaultAppearance, err := store.GetUserAppearance(ctx, "user-appearance-default")
		if err != nil {
			t.Fatalf("GetUserAppearance(default) error = %v", err)
		}
		if defaultAppearance.UserID != "user-appearance-default" {
			t.Fatalf("GetUserAppearance(default) UserID = %q, want %q", defaultAppearance.UserID, "user-appearance-default")
		}
		if defaultAppearance.Theme != "system" {
			t.Fatalf("GetUserAppearance(default) Theme = %q, want %q", defaultAppearance.Theme, "system")
		}
		if defaultAppearance.Density != "comfortable" {
			t.Fatalf("GetUserAppearance(default) Density = %q, want %q", defaultAppearance.Density, "comfortable")
		}
		if defaultAppearance.HelpMode != "basic" {
			t.Fatalf("GetUserAppearance(default) HelpMode = %q, want %q", defaultAppearance.HelpMode, "basic")
		}
		if !defaultAppearance.UpdatedAt.IsZero() {
			t.Fatalf("GetUserAppearance(default) UpdatedAt = %v, want zero time", defaultAppearance.UpdatedAt)
		}

		firstAppearance := storage.UserAppearanceRecord{
			UserID:    "user-appearance-1",
			Theme:     "dark",
			Density:   "compact",
			HelpMode:  "full",
			UpdatedAt: time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC),
		}
		secondAppearance := storage.UserAppearanceRecord{
			UserID:    "user-appearance-2",
			Theme:     "light",
			Density:   "comfortable",
			HelpMode:  "off",
			UpdatedAt: time.Date(2026, time.March, 21, 10, 5, 0, 0, time.UTC),
		}

		if err := store.PutUser(ctx, storage.UserRecord{
			ID:           firstAppearance.UserID,
			Username:     "appearance-one",
			PasswordHash: "argon2id$appearance-one",
			Role:         "viewer",
			CreatedAt:    time.Date(2026, time.March, 21, 9, 55, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("PutUser(first appearance user) error = %v", err)
		}
		if err := store.PutUser(ctx, storage.UserRecord{
			ID:           secondAppearance.UserID,
			Username:     "appearance-two",
			PasswordHash: "argon2id$appearance-two",
			Role:         "operator",
			CreatedAt:    time.Date(2026, time.March, 21, 9, 56, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("PutUser(second appearance user) error = %v", err)
		}

		if err := store.PutUserAppearance(ctx, firstAppearance); err != nil {
			t.Fatalf("PutUserAppearance(first) error = %v", err)
		}
		if err := store.PutUserAppearance(ctx, secondAppearance); err != nil {
			t.Fatalf("PutUserAppearance(second) error = %v", err)
		}

		storedFirstAppearance, err := store.GetUserAppearance(ctx, firstAppearance.UserID)
		if err != nil {
			t.Fatalf("GetUserAppearance(first) error = %v", err)
		}
		if storedFirstAppearance.Theme != firstAppearance.Theme {
			t.Fatalf("GetUserAppearance(first) Theme = %q, want %q", storedFirstAppearance.Theme, firstAppearance.Theme)
		}
		if storedFirstAppearance.Density != firstAppearance.Density {
			t.Fatalf("GetUserAppearance(first) Density = %q, want %q", storedFirstAppearance.Density, firstAppearance.Density)
		}
		if storedFirstAppearance.HelpMode != firstAppearance.HelpMode {
			t.Fatalf("GetUserAppearance(first) HelpMode = %q, want %q", storedFirstAppearance.HelpMode, firstAppearance.HelpMode)
		}
		if !storedFirstAppearance.UpdatedAt.Equal(firstAppearance.UpdatedAt) {
			t.Fatalf("GetUserAppearance(first) UpdatedAt = %v, want %v", storedFirstAppearance.UpdatedAt, firstAppearance.UpdatedAt)
		}

		storedSecondAppearance, err := store.GetUserAppearance(ctx, secondAppearance.UserID)
		if err != nil {
			t.Fatalf("GetUserAppearance(second) error = %v", err)
		}
		if storedSecondAppearance.Theme != secondAppearance.Theme {
			t.Fatalf("GetUserAppearance(second) Theme = %q, want %q", storedSecondAppearance.Theme, secondAppearance.Theme)
		}
		if storedSecondAppearance.Density != secondAppearance.Density {
			t.Fatalf("GetUserAppearance(second) Density = %q, want %q", storedSecondAppearance.Density, secondAppearance.Density)
		}
		if storedSecondAppearance.HelpMode != secondAppearance.HelpMode {
			t.Fatalf("GetUserAppearance(second) HelpMode = %q, want %q", storedSecondAppearance.HelpMode, secondAppearance.HelpMode)
		}
		if !storedSecondAppearance.UpdatedAt.Equal(secondAppearance.UpdatedAt) {
			t.Fatalf("GetUserAppearance(second) UpdatedAt = %v, want %v", storedSecondAppearance.UpdatedAt, secondAppearance.UpdatedAt)
		}

		appearances, err := store.ListUserAppearances(ctx)
		if err != nil {
			t.Fatalf("ListUserAppearances() error = %v", err)
		}
		if len(appearances) != 2 {
			t.Fatalf("len(ListUserAppearances()) = %d, want %d", len(appearances), 2)
		}

		if err := store.DeleteUser(ctx, firstAppearance.UserID); err != nil {
			t.Fatalf("DeleteUser(first appearance user) error = %v", err)
		}
		appearances, err = store.ListUserAppearances(ctx)
		if err != nil {
			t.Fatalf("ListUserAppearances() after DeleteUser error = %v", err)
		}
		if len(appearances) != 1 {
			t.Fatalf("len(ListUserAppearances()) after DeleteUser = %d, want %d", len(appearances), 1)
		}
	})

	t.Run("enrollment token create and use round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// P2-DB-03: enrollment_tokens.fleet_group_id is a FK (ON DELETE
		// SET NULL); the referenced fleet group must exist before we can
		// persist a token that points at it.
		if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		token := storage.EnrollmentTokenRecord{
			Value:        "token-value",
			FleetGroupID: "default",
			IssuedAt:     time.Date(2026, time.March, 15, 8, 5, 0, 0, time.UTC),
			ExpiresAt:    time.Date(2026, time.March, 15, 8, 15, 0, 0, time.UTC),
		}

		if err := store.PutEnrollmentToken(ctx, token); err != nil {
			t.Fatalf("PutEnrollmentToken() error = %v", err)
		}

		loadedToken, err := store.GetEnrollmentToken(ctx, token.Value)
		if err != nil {
			t.Fatalf("GetEnrollmentToken() error = %v", err)
		}
		if loadedToken.FleetGroupID != token.FleetGroupID {
			t.Fatalf("GetEnrollmentToken() FleetGroupID = %q, want %q", loadedToken.FleetGroupID, token.FleetGroupID)
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

	t.Run("enrollment token revoke state round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// P2-DB-03: see note on preceding token test — the fleet group
		// must exist for the FK constraint to accept the token.
		if err := store.PutFleetGroup(ctx, storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		token := storage.EnrollmentTokenRecord{
			Value:        "token-revoke",
			FleetGroupID: "default",
			IssuedAt:     time.Date(2026, time.March, 15, 8, 30, 0, 0, time.UTC),
			ExpiresAt:    time.Date(2026, time.March, 15, 8, 45, 0, 0, time.UTC),
		}

		if err := store.PutEnrollmentToken(ctx, token); err != nil {
			t.Fatalf("PutEnrollmentToken() error = %v", err)
		}

		revokedAt := time.Date(2026, time.March, 15, 8, 35, 0, 0, time.UTC)
		revoked, err := store.RevokeEnrollmentToken(ctx, token.Value, revokedAt)
		if err != nil {
			t.Fatalf("RevokeEnrollmentToken() error = %v", err)
		}

		if revoked.RevokedAt == nil || !revoked.RevokedAt.Equal(revokedAt) {
			t.Fatalf("RevokeEnrollmentToken() RevokedAt = %v, want %v", revoked.RevokedAt, revokedAt)
		}

		stored, err := store.GetEnrollmentToken(ctx, token.Value)
		if err != nil {
			t.Fatalf("GetEnrollmentToken() error = %v", err)
		}

		if stored.RevokedAt == nil || !stored.RevokedAt.Equal(revokedAt) {
			t.Fatalf("GetEnrollmentToken() RevokedAt = %v, want %v", stored.RevokedAt, revokedAt)
		}
	})

	t.Run("agent certificate recovery grant round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 50, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-000001",
			NodeName:     "node-a",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 15, 8, 55, 0, 0, time.UTC),
		}
		grant := storage.AgentCertificateRecoveryGrantRecord{
			AgentID:   agent.ID,
			IssuedBy:  "user-1",
			IssuedAt:  time.Date(2026, time.March, 15, 9, 0, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, time.March, 15, 9, 15, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}
		if err := store.PutAgentCertificateRecoveryGrant(ctx, grant); err != nil {
			t.Fatalf("PutAgentCertificateRecoveryGrant() error = %v", err)
		}

		loadedGrant, err := store.GetAgentCertificateRecoveryGrant(ctx, grant.AgentID)
		if err != nil {
			t.Fatalf("GetAgentCertificateRecoveryGrant() error = %v", err)
		}
		if loadedGrant.IssuedBy != grant.IssuedBy {
			t.Fatalf("GetAgentCertificateRecoveryGrant() IssuedBy = %q, want %q", loadedGrant.IssuedBy, grant.IssuedBy)
		}
		if !loadedGrant.ExpiresAt.Equal(grant.ExpiresAt) {
			t.Fatalf("GetAgentCertificateRecoveryGrant() ExpiresAt = %v, want %v", loadedGrant.ExpiresAt, grant.ExpiresAt)
		}

		usedAt := time.Date(2026, time.March, 15, 9, 5, 0, 0, time.UTC)
		usedGrant, err := store.UseAgentCertificateRecoveryGrant(ctx, grant.AgentID, usedAt)
		if err != nil {
			t.Fatalf("UseAgentCertificateRecoveryGrant() error = %v", err)
		}
		if usedGrant.UsedAt == nil || !usedGrant.UsedAt.Equal(usedAt) {
			t.Fatalf("UseAgentCertificateRecoveryGrant() UsedAt = %v, want %v", usedGrant.UsedAt, usedAt)
		}

		reloadedGrant, err := store.GetAgentCertificateRecoveryGrant(ctx, grant.AgentID)
		if err != nil {
			t.Fatalf("GetAgentCertificateRecoveryGrant() after use error = %v", err)
		}
		if reloadedGrant.UsedAt == nil || !reloadedGrant.UsedAt.Equal(usedAt) {
			t.Fatalf("GetAgentCertificateRecoveryGrant() after use UsedAt = %v, want %v", reloadedGrant.UsedAt, usedAt)
		}
	})

	t.Run("agent certificate recovery grant revoke state round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 9, 20, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-000002",
			NodeName:     "node-b",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 15, 9, 25, 0, 0, time.UTC),
		}
		grant := storage.AgentCertificateRecoveryGrantRecord{
			AgentID:   agent.ID,
			IssuedBy:  "user-2",
			IssuedAt:  time.Date(2026, time.March, 15, 9, 30, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, time.March, 15, 9, 45, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}
		if err := store.PutAgentCertificateRecoveryGrant(ctx, grant); err != nil {
			t.Fatalf("PutAgentCertificateRecoveryGrant() error = %v", err)
		}

		revokedAt := time.Date(2026, time.March, 15, 9, 35, 0, 0, time.UTC)
		revokedGrant, err := store.RevokeAgentCertificateRecoveryGrant(ctx, grant.AgentID, revokedAt)
		if err != nil {
			t.Fatalf("RevokeAgentCertificateRecoveryGrant() error = %v", err)
		}
		if revokedGrant.RevokedAt == nil || !revokedGrant.RevokedAt.Equal(revokedAt) {
			t.Fatalf("RevokeAgentCertificateRecoveryGrant() RevokedAt = %v, want %v", revokedGrant.RevokedAt, revokedAt)
		}

		storedGrant, err := store.GetAgentCertificateRecoveryGrant(ctx, grant.AgentID)
		if err != nil {
			t.Fatalf("GetAgentCertificateRecoveryGrant() after revoke error = %v", err)
		}
		if storedGrant.RevokedAt == nil || !storedGrant.RevokedAt.Equal(revokedAt) {
			t.Fatalf("GetAgentCertificateRecoveryGrant() after revoke RevokedAt = %v, want %v", storedGrant.RevokedAt, revokedAt)
		}
	})

	t.Run("agent and instance snapshot persistence round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 15, 8, 20, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-000001",
			NodeName:     "node-a",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 15, 8, 25, 0, 0, time.UTC),
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
			PayloadJSON:    `{"scope":"telemt"}`,
		}
		target := storage.JobTargetRecord{
			JobID:      job.ID,
			AgentID:    "agent-000001",
			Status:     "queued",
			UpdatedAt:  job.CreatedAt,
			ResultText: "",
			ResultJSON: `{"accepted":true}`,
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
		if storedJob.PayloadJSON != job.PayloadJSON {
			t.Fatalf("GetJobByIdempotencyKey() PayloadJSON = %q, want %q", storedJob.PayloadJSON, job.PayloadJSON)
		}

		targets, err := store.ListJobTargets(ctx, job.ID)
		if err != nil {
			t.Fatalf("ListJobTargets() error = %v", err)
		}

		if len(targets) != 1 {
			t.Fatalf("len(ListJobTargets()) = %d, want 1", len(targets))
		}
		if targets[0].ResultJSON != target.ResultJSON {
			t.Fatalf("ListJobTargets()[0].ResultJSON = %q, want %q", targets[0].ResultJSON, target.ResultJSON)
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

		events, err := store.ListAuditEvents(ctx, 0)
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
		// P2-DB-03: metric_snapshots.agent_id now has ON DELETE CASCADE
		// FK to agents(id); the referenced agent must exist.
		if err := store.PutAgent(ctx, storage.AgentRecord{
			ID:         "agent-000001",
			NodeName:   "node-metric",
			LastSeenAt: time.Date(2026, time.March, 15, 8, 40, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}
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

	// P2-REL-04 / finding M-R2: audit_events must be prunable by cutoff so
	// the retention worker can bound table growth.
	t.Run("audit prune deletes rows older than cutoff", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		baseTime := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)

		seed := []storage.AuditEventRecord{
			{ID: "audit-old-1", ActorID: "u", Action: "a", TargetID: "t", CreatedAt: baseTime.Add(-72 * time.Hour), Details: map[string]any{"k": "1"}},
			{ID: "audit-old-2", ActorID: "u", Action: "a", TargetID: "t", CreatedAt: baseTime.Add(-48 * time.Hour), Details: map[string]any{"k": "2"}},
			{ID: "audit-keep-1", ActorID: "u", Action: "a", TargetID: "t", CreatedAt: baseTime.Add(-12 * time.Hour), Details: map[string]any{"k": "3"}},
			{ID: "audit-keep-2", ActorID: "u", Action: "a", TargetID: "t", CreatedAt: baseTime, Details: map[string]any{"k": "4"}},
		}
		for _, e := range seed {
			if err := store.AppendAuditEvent(ctx, e); err != nil {
				t.Fatalf("AppendAuditEvent(%s) error = %v", e.ID, err)
			}
		}

		cutoff := baseTime.Add(-24 * time.Hour)
		pruned, err := store.PruneAuditEvents(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneAuditEvents() error = %v", err)
		}
		if pruned != 2 {
			t.Fatalf("PruneAuditEvents() pruned = %d, want 2", pruned)
		}

		events, err := store.ListAuditEvents(ctx, 0)
		if err != nil {
			t.Fatalf("ListAuditEvents() error = %v", err)
		}
		if len(events) != 2 {
			t.Fatalf("len(ListAuditEvents()) after prune = %d, want 2", len(events))
		}
		for _, e := range events {
			if e.CreatedAt.Before(cutoff) {
				t.Fatalf("retained event %q has CreatedAt %v before cutoff %v", e.ID, e.CreatedAt, cutoff)
			}
		}

		// A second call with the same cutoff is a no-op.
		pruned2, err := store.PruneAuditEvents(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneAuditEvents(second) error = %v", err)
		}
		if pruned2 != 0 {
			t.Fatalf("PruneAuditEvents(second) pruned = %d, want 0", pruned2)
		}
	})

	// P2-REL-05: metric_snapshots must be prunable by captured_at cutoff.
	t.Run("metric snapshot prune deletes rows older than cutoff", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		baseTime := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)

		// P2-DB-03: metric_snapshots.agent_id has a CASCADE FK — seed the
		// agent so the inserts do not trip the constraint.
		if err := store.PutAgent(ctx, storage.AgentRecord{
			ID:         "a1",
			NodeName:   "node-prune",
			LastSeenAt: baseTime,
		}); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		seed := []storage.MetricSnapshotRecord{
			{ID: "metric-old-1", AgentID: "a1", InstanceID: "i1", CapturedAt: baseTime.Add(-72 * time.Hour), Values: map[string]uint64{"x": 1}},
			{ID: "metric-old-2", AgentID: "a1", InstanceID: "i1", CapturedAt: baseTime.Add(-48 * time.Hour), Values: map[string]uint64{"x": 2}},
			{ID: "metric-keep-1", AgentID: "a1", InstanceID: "i1", CapturedAt: baseTime.Add(-12 * time.Hour), Values: map[string]uint64{"x": 3}},
			{ID: "metric-keep-2", AgentID: "a1", InstanceID: "i1", CapturedAt: baseTime, Values: map[string]uint64{"x": 4}},
		}
		for _, m := range seed {
			if err := store.AppendMetricSnapshot(ctx, m); err != nil {
				t.Fatalf("AppendMetricSnapshot(%s) error = %v", m.ID, err)
			}
		}

		cutoff := baseTime.Add(-24 * time.Hour)
		pruned, err := store.PruneMetricSnapshots(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneMetricSnapshots() error = %v", err)
		}
		if pruned != 2 {
			t.Fatalf("PruneMetricSnapshots() pruned = %d, want 2", pruned)
		}

		snapshots, err := store.ListMetricSnapshots(ctx)
		if err != nil {
			t.Fatalf("ListMetricSnapshots() error = %v", err)
		}
		if len(snapshots) != 2 {
			t.Fatalf("len(ListMetricSnapshots()) after prune = %d, want 2", len(snapshots))
		}
		for _, m := range snapshots {
			if m.CapturedAt.Before(cutoff) {
				t.Fatalf("retained snapshot %q has CapturedAt %v before cutoff %v", m.ID, m.CapturedAt, cutoff)
			}
		}

		// A second call with the same cutoff is a no-op.
		pruned2, err := store.PruneMetricSnapshots(ctx, cutoff)
		if err != nil {
			t.Fatalf("PruneMetricSnapshots(second) error = %v", err)
		}
		if pruned2 != 0 {
			t.Fatalf("PruneMetricSnapshots(second) pruned = %d, want 0", pruned2)
		}
	})

	t.Run("telemetry current-state round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 28, 10, 0, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-telemetry-1",
			NodeName:     "telemt-a",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 28, 10, 1, 0, 0, time.UTC),
		}
		runtime := storage.TelemetryRuntimeCurrentRecord{
			AgentID:                   agent.ID,
			ObservedAt:                time.Date(2026, time.March, 28, 10, 2, 0, 0, time.UTC),
			State:                     "fresh",
			StateReason:               "",
			ReadOnly:                  false,
			AcceptingNewConnections:   true,
			MERuntimeReady:            true,
			ME2DCFallbackEnabled:      true,
			UseMiddleProxy:            false,
			StartupStatus:             "ready",
			StartupStage:              "steady_state",
			StartupProgressPct:        100,
			InitializationStatus:      "ready",
			Degraded:                  false,
			InitializationStage:       "steady_state",
			InitializationProgressPct: 100,
			TransportMode:             "direct",
			CurrentConnections:        120,
			CurrentConnectionsME:      70,
			CurrentConnectionsDirect:  50,
			ActiveUsers:               95,
			UptimeSeconds:             3600,
			ConnectionsTotal:          1024,
			ConnectionsBadTotal:       12,
			HandshakeTimeoutsTotal:    2,
			ConfiguredUsers:           4096,
			DCCoveragePct:             83,
			HealthyUpstreams:          2,
			TotalUpstreams:            3,
		}
		dcs := []storage.TelemetryRuntimeDCRecord{
			{
				AgentID:            agent.ID,
				DC:                 2,
				ObservedAt:         runtime.ObservedAt,
				AvailableEndpoints: 4,
				AvailablePct:       100,
				RequiredWriters:    6,
				AliveWriters:       5,
				CoveragePct:        83.3,
				RTTMs:              42,
				Load:               0.7,
			},
		}
		upstreams := []storage.TelemetryRuntimeUpstreamRecord{
			{
				AgentID:            agent.ID,
				UpstreamID:         1,
				ObservedAt:         runtime.ObservedAt,
				RouteKind:          "direct",
				Address:            "fra-core-01:443",
				Healthy:            true,
				Fails:              0,
				EffectiveLatencyMs: 19,
			},
		}
		events := []storage.TelemetryRuntimeEventRecord{
			{
				AgentID:    agent.ID,
				Sequence:   41,
				ObservedAt: runtime.ObservedAt,
				Timestamp:  time.Date(2026, time.March, 28, 10, 1, 30, 0, time.UTC),
				EventType:  "dc_quorum_warning",
				Context:    "DC 2 coverage dropped below quorum",
				Severity:   "warn",
			},
		}
		diagnostics := storage.TelemetryDiagnosticsCurrentRecord{
			AgentID:             agent.ID,
			ObservedAt:          time.Date(2026, time.March, 28, 10, 2, 30, 0, time.UTC),
			State:               "fresh",
			StateReason:         "",
			SystemInfoJSON:      `{"version":"1.0.0"}`,
			EffectiveLimitsJSON: `{"max_tcp_conns":4}`,
			SecurityPostureJSON: `{"read_only":false}`,
			MinimalAllJSON:      `{"enabled":true}`,
			MEPoolJSON:          `{"enabled":true}`,
		}
		security := storage.TelemetrySecurityInventoryCurrentRecord{
			AgentID:      agent.ID,
			ObservedAt:   time.Date(2026, time.March, 28, 10, 3, 0, 0, time.UTC),
			State:        "fresh",
			StateReason:  "",
			Enabled:      true,
			EntriesTotal: 2,
			EntriesJSON:  `["10.0.0.0/24","192.168.0.0/24"]`,
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}
		if err := store.PutTelemetryRuntimeCurrent(ctx, runtime); err != nil {
			t.Fatalf("PutTelemetryRuntimeCurrent() error = %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeDCs(ctx, agent.ID, dcs); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeDCs() error = %v", err)
		}
		if err := store.ReplaceTelemetryRuntimeUpstreams(ctx, agent.ID, upstreams); err != nil {
			t.Fatalf("ReplaceTelemetryRuntimeUpstreams() error = %v", err)
		}
		if err := store.AppendTelemetryRuntimeEvents(ctx, agent.ID, events); err != nil {
			t.Fatalf("AppendTelemetryRuntimeEvents() error = %v", err)
		}
		if err := store.PutTelemetryDiagnosticsCurrent(ctx, diagnostics); err != nil {
			t.Fatalf("PutTelemetryDiagnosticsCurrent() error = %v", err)
		}
		if err := store.PutTelemetrySecurityInventoryCurrent(ctx, security); err != nil {
			t.Fatalf("PutTelemetrySecurityInventoryCurrent() error = %v", err)
		}

		storedRuntime, err := store.GetTelemetryRuntimeCurrent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("GetTelemetryRuntimeCurrent() error = %v", err)
		}
		if storedRuntime.CurrentConnections != runtime.CurrentConnections {
			t.Fatalf("GetTelemetryRuntimeCurrent() CurrentConnections = %d, want %d", storedRuntime.CurrentConnections, runtime.CurrentConnections)
		}

		storedRuntimes, err := store.ListTelemetryRuntimeCurrent(ctx)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeCurrent() error = %v", err)
		}
		if len(storedRuntimes) != 1 {
			t.Fatalf("len(ListTelemetryRuntimeCurrent()) = %d, want 1", len(storedRuntimes))
		}

		storedDCs, err := store.ListTelemetryRuntimeDCs(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeDCs() error = %v", err)
		}
		if len(storedDCs) != 1 {
			t.Fatalf("len(ListTelemetryRuntimeDCs()) = %d, want 1", len(storedDCs))
		}
		if storedDCs[0].CoveragePct != dcs[0].CoveragePct {
			t.Fatalf("ListTelemetryRuntimeDCs()[0].CoveragePct = %v, want %v", storedDCs[0].CoveragePct, dcs[0].CoveragePct)
		}

		storedUpstreams, err := store.ListTelemetryRuntimeUpstreams(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeUpstreams() error = %v", err)
		}
		if len(storedUpstreams) != 1 {
			t.Fatalf("len(ListTelemetryRuntimeUpstreams()) = %d, want 1", len(storedUpstreams))
		}
		if storedUpstreams[0].Address != upstreams[0].Address {
			t.Fatalf("ListTelemetryRuntimeUpstreams()[0].Address = %q, want %q", storedUpstreams[0].Address, upstreams[0].Address)
		}

		storedEvents, err := store.ListTelemetryRuntimeEvents(ctx, agent.ID, 10)
		if err != nil {
			t.Fatalf("ListTelemetryRuntimeEvents() error = %v", err)
		}
		if len(storedEvents) != 1 {
			t.Fatalf("len(ListTelemetryRuntimeEvents()) = %d, want 1", len(storedEvents))
		}
		if storedEvents[0].EventType != events[0].EventType {
			t.Fatalf("ListTelemetryRuntimeEvents()[0].EventType = %q, want %q", storedEvents[0].EventType, events[0].EventType)
		}

		storedDiagnostics, err := store.GetTelemetryDiagnosticsCurrent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("GetTelemetryDiagnosticsCurrent() error = %v", err)
		}
		if storedDiagnostics.SystemInfoJSON != diagnostics.SystemInfoJSON {
			t.Fatalf("GetTelemetryDiagnosticsCurrent() SystemInfoJSON = %q, want %q", storedDiagnostics.SystemInfoJSON, diagnostics.SystemInfoJSON)
		}

		storedSecurity, err := store.GetTelemetrySecurityInventoryCurrent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("GetTelemetrySecurityInventoryCurrent() error = %v", err)
		}
		if storedSecurity.EntriesTotal != security.EntriesTotal {
			t.Fatalf("GetTelemetrySecurityInventoryCurrent() EntriesTotal = %d, want %d", storedSecurity.EntriesTotal, security.EntriesTotal)
		}
	})

	t.Run("session put get delete round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// P2-DB-03: sessions.user_id is now a CASCADE FK to users(id).
		// Seed the owning user before persisting the session.
		if err := store.PutUser(ctx, storage.UserRecord{
			ID:           "user-001",
			Username:     "session-user",
			PasswordHash: "argon2id$hash",
			Role:         "admin",
			CreatedAt:    time.Date(2026, time.April, 15, 9, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("PutUser() error = %v", err)
		}
		session := storage.SessionRecord{
			ID:        "sess-001",
			UserID:    "user-001",
			CreatedAt: time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC),
		}

		if err := store.PutSession(ctx, session); err != nil {
			t.Fatalf("PutSession() error = %v", err)
		}

		got, err := store.GetSession(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetSession() error = %v", err)
		}
		if got.UserID != session.UserID {
			t.Fatalf("GetSession().UserID = %q, want %q", got.UserID, session.UserID)
		}

		sessions, err := store.ListSessions(ctx)
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("len(ListSessions()) = %d, want 1", len(sessions))
		}

		if err := store.DeleteSession(ctx, session.ID); err != nil {
			t.Fatalf("DeleteSession() error = %v", err)
		}

		_, err = store.GetSession(ctx, session.ID)
		if err == nil {
			t.Fatal("GetSession() after delete returned nil error, want ErrNotFound")
		}
	})

	t.Run("session delete expired removes old sessions", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		// P2-DB-03: seed users referenced by the sessions below.
		for _, u := range []storage.UserRecord{
			{ID: "user-001", Username: "session-u1", PasswordHash: "h", Role: "admin", CreatedAt: time.Date(2026, time.April, 14, 7, 0, 0, 0, time.UTC)},
			{ID: "user-002", Username: "session-u2", PasswordHash: "h", Role: "admin", CreatedAt: time.Date(2026, time.April, 15, 11, 0, 0, 0, time.UTC)},
		} {
			if err := store.PutUser(ctx, u); err != nil {
				t.Fatalf("PutUser(%s) error = %v", u.ID, err)
			}
		}
		old := storage.SessionRecord{
			ID:        "sess-old",
			UserID:    "user-001",
			CreatedAt: time.Date(2026, time.April, 14, 8, 0, 0, 0, time.UTC),
		}
		fresh := storage.SessionRecord{
			ID:        "sess-fresh",
			UserID:    "user-002",
			CreatedAt: time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC),
		}

		if err := store.PutSession(ctx, old); err != nil {
			t.Fatalf("PutSession(old) error = %v", err)
		}
		if err := store.PutSession(ctx, fresh); err != nil {
			t.Fatalf("PutSession(fresh) error = %v", err)
		}

		cutoff := time.Date(2026, time.April, 15, 0, 0, 0, 0, time.UTC)
		if err := store.DeleteExpiredSessions(ctx, cutoff); err != nil {
			t.Fatalf("DeleteExpiredSessions() error = %v", err)
		}

		sessions, err := store.ListSessions(ctx)
		if err != nil {
			t.Fatalf("ListSessions() error = %v", err)
		}
		if len(sessions) != 1 {
			t.Fatalf("len(ListSessions()) after expiry = %d, want 1", len(sessions))
		}
		if sessions[0].ID != fresh.ID {
			t.Fatalf("remaining session ID = %q, want %q", sessions[0].ID, fresh.ID)
		}
	})

	t.Run("discovered client put list and delete round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-dc-001",
			NodeName:     "node-dc",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.April, 15, 10, 1, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}

		dc := storage.DiscoveredClientRecord{
			ID:           "dc-001",
			AgentID:      agent.ID,
			ClientName:   "external-user",
			Secret:       "abc123",
			Status:       "new",
			DiscoveredAt: time.Date(2026, time.April, 15, 10, 5, 0, 0, time.UTC),
			UpdatedAt:    time.Date(2026, time.April, 15, 10, 5, 0, 0, time.UTC),
		}

		if err := store.PutDiscoveredClient(ctx, dc); err != nil {
			t.Fatalf("PutDiscoveredClient() error = %v", err)
		}

		list, err := store.ListDiscoveredClients(ctx)
		if err != nil {
			t.Fatalf("ListDiscoveredClients() error = %v", err)
		}
		if len(list) != 1 {
			t.Fatalf("len(ListDiscoveredClients()) = %d, want 1", len(list))
		}

		byAgent, err := store.ListDiscoveredClientsByAgent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListDiscoveredClientsByAgent() error = %v", err)
		}
		if len(byAgent) != 1 {
			t.Fatalf("len(ListDiscoveredClientsByAgent()) = %d, want 1", len(byAgent))
		}

		got, err := store.GetDiscoveredClient(ctx, dc.ID)
		if err != nil {
			t.Fatalf("GetDiscoveredClient() error = %v", err)
		}
		if got.ClientName != dc.ClientName {
			t.Fatalf("GetDiscoveredClient().ClientName = %q, want %q", got.ClientName, dc.ClientName)
		}

		updatedAt := time.Date(2026, time.April, 15, 10, 10, 0, 0, time.UTC)
		if err := store.UpdateDiscoveredClientStatus(ctx, dc.ID, "ignored", updatedAt); err != nil {
			t.Fatalf("UpdateDiscoveredClientStatus() error = %v", err)
		}
		got, _ = store.GetDiscoveredClient(ctx, dc.ID)
		if got.Status != "ignored" {
			t.Fatalf("status after update = %q, want %q", got.Status, "ignored")
		}

		if err := store.DeleteDiscoveredClient(ctx, dc.ID); err != nil {
			t.Fatalf("DeleteDiscoveredClient() error = %v", err)
		}
		_, err = store.GetDiscoveredClient(ctx, dc.ID)
		if err == nil {
			t.Fatal("GetDiscoveredClient() after delete returned nil error, want ErrNotFound")
		}
	})

	t.Run("GetDiscoveredClientByAgentAndName", func(t *testing.T) {
		// P2-LOG-02: the reconcile path relies on this lookup to dedupe
		// repeated FULL_SNAPSHOT reports. Verify the natural-key lookup
		// returns the correct row when it exists and ErrNotFound otherwise.
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC),
		}
		agentA := storage.AgentRecord{
			ID:           "agent-dc-nk-A",
			NodeName:     "node-A",
			FleetGroupID: group.ID,
			Version:      "dev",
			LastSeenAt:   time.Date(2026, time.April, 15, 10, 1, 0, 0, time.UTC),
		}
		agentB := storage.AgentRecord{
			ID:           "agent-dc-nk-B",
			NodeName:     "node-B",
			FleetGroupID: group.ID,
			Version:      "dev",
			LastSeenAt:   time.Date(2026, time.April, 15, 10, 1, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agentA); err != nil {
			t.Fatalf("PutAgent(A) error = %v", err)
		}
		if err := store.PutAgent(ctx, agentB); err != nil {
			t.Fatalf("PutAgent(B) error = %v", err)
		}

		ts := time.Date(2026, time.April, 15, 10, 5, 0, 0, time.UTC)

		// Nothing yet -> ErrNotFound.
		if _, err := store.GetDiscoveredClientByAgentAndName(ctx, agentA.ID, "alpha"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetDiscoveredClientByAgentAndName() before insert error = %v, want ErrNotFound", err)
		}

		// Insert one row on agentA and another with the SAME client_name on
		// agentB: the lookup must scope by agent_id and not collide across
		// agents.
		dcA := storage.DiscoveredClientRecord{
			ID:           "dc-nk-A-alpha",
			AgentID:      agentA.ID,
			ClientName:   "alpha",
			Secret:       "secretA",
			Status:       "pending_review",
			DiscoveredAt: ts,
			UpdatedAt:    ts,
		}
		dcB := storage.DiscoveredClientRecord{
			ID:           "dc-nk-B-alpha",
			AgentID:      agentB.ID,
			ClientName:   "alpha",
			Secret:       "secretB",
			Status:       "pending_review",
			DiscoveredAt: ts,
			UpdatedAt:    ts,
		}
		if err := store.PutDiscoveredClient(ctx, dcA); err != nil {
			t.Fatalf("PutDiscoveredClient(A) error = %v", err)
		}
		if err := store.PutDiscoveredClient(ctx, dcB); err != nil {
			t.Fatalf("PutDiscoveredClient(B) error = %v", err)
		}

		gotA, err := store.GetDiscoveredClientByAgentAndName(ctx, agentA.ID, "alpha")
		if err != nil {
			t.Fatalf("GetDiscoveredClientByAgentAndName(A) error = %v", err)
		}
		if gotA.ID != dcA.ID {
			t.Fatalf("GetDiscoveredClientByAgentAndName(A).ID = %q, want %q", gotA.ID, dcA.ID)
		}
		if gotA.Secret != "secretA" {
			t.Fatalf("GetDiscoveredClientByAgentAndName(A).Secret = %q, want %q", gotA.Secret, "secretA")
		}

		gotB, err := store.GetDiscoveredClientByAgentAndName(ctx, agentB.ID, "alpha")
		if err != nil {
			t.Fatalf("GetDiscoveredClientByAgentAndName(B) error = %v", err)
		}
		if gotB.ID != dcB.ID {
			t.Fatalf("GetDiscoveredClientByAgentAndName(B).ID = %q, want %q", gotB.ID, dcB.ID)
		}

		// Unknown name on a known agent -> ErrNotFound.
		if _, err := store.GetDiscoveredClientByAgentAndName(ctx, agentA.ID, "does-not-exist"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetDiscoveredClientByAgentAndName(missing name) error = %v, want ErrNotFound", err)
		}

		// Unknown agent -> ErrNotFound.
		if _, err := store.GetDiscoveredClientByAgentAndName(ctx, "agent-nobody", "alpha"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetDiscoveredClientByAgentAndName(missing agent) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("update config settings and state round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		settings := json.RawMessage(`{"auto_update":true,"channel":"stable"}`)
		state := json.RawMessage(`{"latest_version":"v1.2.3","checked_at":"2026-04-15T10:00:00Z"}`)

		if err := store.PutUpdateSettings(ctx, settings); err != nil {
			t.Fatalf("PutUpdateSettings() error = %v", err)
		}
		if err := store.PutUpdateState(ctx, state); err != nil {
			t.Fatalf("PutUpdateState() error = %v", err)
		}

		gotSettings, err := store.GetUpdateSettings(ctx)
		if err != nil {
			t.Fatalf("GetUpdateSettings() error = %v", err)
		}
		if string(gotSettings) != string(settings) {
			t.Fatalf("GetUpdateSettings() = %s, want %s", gotSettings, settings)
		}

		gotState, err := store.GetUpdateState(ctx)
		if err != nil {
			t.Fatalf("GetUpdateState() error = %v", err)
		}
		if string(gotState) != string(state) {
			t.Fatalf("GetUpdateState() = %s, want %s", gotState, state)
		}
	})

	t.Run("telemetry detail boost round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "default",
			Name:      "Default",
			CreatedAt: time.Date(2026, time.March, 28, 11, 0, 0, 0, time.UTC),
		}
		agent := storage.AgentRecord{
			ID:           "agent-boost-1",
			NodeName:     "telemt-b",
			FleetGroupID: group.ID,
			Version:      "dev",
			ReadOnly:     false,
			LastSeenAt:   time.Date(2026, time.March, 28, 11, 1, 0, 0, time.UTC),
		}
		boost := storage.TelemetryDetailBoostRecord{
			AgentID:   agent.ID,
			ExpiresAt: time.Date(2026, time.March, 28, 11, 10, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, time.March, 28, 11, 2, 0, 0, time.UTC),
		}

		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent() error = %v", err)
		}
		if err := store.PutTelemetryDetailBoost(ctx, boost); err != nil {
			t.Fatalf("PutTelemetryDetailBoost() error = %v", err)
		}

		boosts, err := store.ListTelemetryDetailBoosts(ctx)
		if err != nil {
			t.Fatalf("ListTelemetryDetailBoosts() error = %v", err)
		}
		if len(boosts) != 1 {
			t.Fatalf("len(ListTelemetryDetailBoosts()) = %d, want 1", len(boosts))
		}
		if boosts[0].AgentID != boost.AgentID {
			t.Fatalf("ListTelemetryDetailBoosts()[0].AgentID = %q, want %q", boosts[0].AgentID, boost.AgentID)
		}
		if !boosts[0].ExpiresAt.Equal(boost.ExpiresAt) {
			t.Fatalf("ListTelemetryDetailBoosts()[0].ExpiresAt = %v, want %v", boosts[0].ExpiresAt, boost.ExpiresAt)
		}

		if err := store.DeleteTelemetryDetailBoost(ctx, boost.AgentID); err != nil {
			t.Fatalf("DeleteTelemetryDetailBoost() error = %v", err)
		}
		boosts, err = store.ListTelemetryDetailBoosts(ctx)
		if err != nil {
			t.Fatalf("ListTelemetryDetailBoosts() after delete error = %v", err)
		}
		if len(boosts) != 0 {
			t.Fatalf("len(ListTelemetryDetailBoosts()) after delete = %d, want 0", len(boosts))
		}
	})

	// --- Transact contract (P2-ARCH-01) ---

	t.Run("Transact commits on nil return", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "tx-commit-group",
			Name:      "tx-commit-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		client := storage.ClientRecord{
			ID:        "tx-commit-client",
			Name:      "tx-commit-client",
			SecretCiphertext: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			CreatedAt: group.CreatedAt,
			UpdatedAt: group.CreatedAt,
		}

		if err := store.Transact(ctx, func(tx storage.Store) error {
			if err := tx.PutFleetGroup(ctx, group); err != nil {
				return err
			}
			return tx.PutClient(ctx, client)
		}); err != nil {
			t.Fatalf("Transact() commit error = %v", err)
		}

		got, err := store.GetClientByID(ctx, client.ID)
		if err != nil {
			t.Fatalf("GetClientByID() after commit error = %v", err)
		}
		if got.ID != client.ID {
			t.Fatalf("GetClientByID().ID = %q, want %q", got.ID, client.ID)
		}
	})

	t.Run("Transact rolls back on fn error", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "tx-rollback-group",
			Name:      "tx-rollback-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		client := storage.ClientRecord{
			ID:        "tx-rollback-client",
			Name:      "tx-rollback-client",
			SecretCiphertext: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			CreatedAt: group.CreatedAt,
			UpdatedAt: group.CreatedAt,
		}

		sentinel := errors.New("sentinel rollback")
		err := store.Transact(ctx, func(tx storage.Store) error {
			if err := tx.PutFleetGroup(ctx, group); err != nil {
				return err
			}
			if err := tx.PutClient(ctx, client); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("Transact() err = %v, want %v", err, sentinel)
		}

		if _, err := store.GetClientByID(ctx, client.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetClientByID() after rollback err = %v, want ErrNotFound", err)
		}
	})

	t.Run("Transact rolls back on panic and re-raises", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		client := storage.ClientRecord{
			ID:        "tx-panic-client",
			Name:      "tx-panic-client",
			SecretCiphertext: "cccccccccccccccccccccccccccccccc",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}

		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic to propagate out of Transact")
				}
			}()
			_ = store.Transact(ctx, func(tx storage.Store) error {
				if err := tx.PutClient(ctx, client); err != nil {
					t.Fatalf("PutClient inside Transact error = %v", err)
				}
				panic("boom")
			})
		}()

		if _, err := store.GetClientByID(ctx, client.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetClientByID() after panic-rollback err = %v, want ErrNotFound", err)
		}
	})

	t.Run("Transact returns ErrNestedTransact on nested call", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		var inner error
		outer := store.Transact(ctx, func(tx storage.Store) error {
			inner = tx.Transact(ctx, func(storage.Store) error { return nil })
			return nil
		})
		if outer != nil {
			t.Fatalf("outer Transact() err = %v, want nil", outer)
		}
		if !errors.Is(inner, storage.ErrNestedTransact) {
			t.Fatalf("inner Transact() err = %v, want ErrNestedTransact", inner)
		}
	})

	t.Run("Transact aborts when context cancelled before fn", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := store.Transact(ctx, func(tx storage.Store) error {
			t.Fatalf("fn should not run after ctx cancellation")
			return nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Transact() err = %v, want context.Canceled", err)
		}
	})

	t.Run("Transact serializes concurrent writers on same row", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "tx-concurrent-group",
			Name:      "tx-concurrent-group",
			CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup() error = %v", err)
		}

		const clientID = "tx-concurrent-client"
		type result struct {
			err    error
			winner string
		}
		results := make(chan result, 2)
		run := func(name string) {
			err := store.Transact(ctx, func(tx storage.Store) error {
				client := storage.ClientRecord{
					ID:        clientID,
					Name:      name,
					SecretCiphertext: "dddddddddddddddddddddddddddddddd",
					CreatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
					UpdatedAt: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
				}
				if err := tx.PutClient(ctx, client); err != nil {
					return err
				}
				assignment := storage.ClientAssignmentRecord{
					ID:           name + "-assignment",
					ClientID:     clientID,
					FleetGroupID: group.ID,
					CreatedAt:    client.CreatedAt,
				}
				return tx.PutClientAssignment(ctx, assignment)
			})
			results <- result{err: err, winner: name}
		}
		go run("name-a")
		go run("name-b")
		r1 := <-results
		r2 := <-results

		if r1.err != nil && r2.err != nil {
			t.Fatalf("both Transacts failed: r1=%v r2=%v", r1.err, r2.err)
		}

		got, err := store.GetClientByID(ctx, clientID)
		if err != nil {
			t.Fatalf("GetClientByID() error = %v", err)
		}
		if got.Name != "name-a" && got.Name != "name-b" {
			t.Fatalf("GetClientByID().Name = %q, want name-a or name-b", got.Name)
		}

		assignments, err := store.ListClientAssignments(ctx, clientID)
		if err != nil {
			t.Fatalf("ListClientAssignments() error = %v", err)
		}
		if len(assignments) == 0 {
			t.Fatalf("expected at least one assignment from the winning Transact")
		}
	})

	t.Run("Transact rejects nil fn", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		if err := store.Transact(context.Background(), nil); err == nil {
			t.Fatalf("Transact(nil) err = nil, want non-nil")
		}
	})

	// -------------------------------------------------------------------
	// P3-PERF-01a: bulk insert contract tests.
	// -------------------------------------------------------------------

	t.Run("PutAgentsBulk empty slice is a no-op", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		if err := store.PutAgentsBulk(context.Background(), nil); err != nil {
			t.Fatalf("PutAgentsBulk(nil) err = %v, want nil", err)
		}
		if err := store.PutAgentsBulk(context.Background(), []storage.AgentRecord{}); err != nil {
			t.Fatalf("PutAgentsBulk([]) err = %v, want nil", err)
		}
	})

	t.Run("PutAgentsBulk UPSERT semantics - last write wins on duplicate id", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{
			ID:        "bulk-grp",
			Name:      "Bulk Group",
			CreatedAt: time.Date(2026, time.April, 1, 10, 0, 0, 0, time.UTC),
		}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}

		ts := time.Date(2026, time.April, 1, 10, 5, 0, 0, time.UTC)
		// Two entries with the same ID in one batch — the second must win.
		batch := []storage.AgentRecord{
			{ID: "a-dup", NodeName: "first", FleetGroupID: group.ID, Version: "v1", LastSeenAt: ts},
			{ID: "a-dup", NodeName: "second", FleetGroupID: group.ID, Version: "v2", LastSeenAt: ts},
			{ID: "a-unique", NodeName: "solo", FleetGroupID: group.ID, Version: "v1", LastSeenAt: ts},
		}
		if err := store.PutAgentsBulk(ctx, batch); err != nil {
			t.Fatalf("PutAgentsBulk: %v", err)
		}

		agents, err := store.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents: %v", err)
		}
		if len(agents) != 2 {
			t.Fatalf("len(agents) = %d, want 2 (dedup)", len(agents))
		}
		var dup storage.AgentRecord
		for _, a := range agents {
			if a.ID == "a-dup" {
				dup = a
			}
		}
		if dup.NodeName != "second" || dup.Version != "v2" {
			t.Fatalf("dup node_name=%q version=%q, want second/v2 (last-write-wins)", dup.NodeName, dup.Version)
		}

		// Calling PutAgentsBulk again with an updated row for the same id
		// updates in place (UPSERT semantics across calls).
		if err := store.PutAgentsBulk(ctx, []storage.AgentRecord{{
			ID: "a-dup", NodeName: "third", FleetGroupID: group.ID, Version: "v3", LastSeenAt: ts,
		}}); err != nil {
			t.Fatalf("PutAgentsBulk (second call): %v", err)
		}
		agents, err = store.ListAgents(ctx)
		if err != nil {
			t.Fatalf("ListAgents after second call: %v", err)
		}
		for _, a := range agents {
			if a.ID == "a-dup" && a.NodeName != "third" {
				t.Fatalf("after second PutAgentsBulk, node_name = %q, want 'third'", a.NodeName)
			}
		}
	})

	t.Run("PutInstancesBulk upserts a batch", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "inst-grp", Name: "Inst", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "inst-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
		}
		ts := time.Date(2026, time.April, 1, 12, 0, 0, 0, time.UTC)
		batch := []storage.InstanceRecord{
			{ID: "i1", AgentID: agent.ID, Name: "t1", Version: "v1", ConfigFingerprint: "c1", ConnectedUsers: 1, UpdatedAt: ts},
			{ID: "i2", AgentID: agent.ID, Name: "t2", Version: "v1", ConfigFingerprint: "c2", ConnectedUsers: 2, UpdatedAt: ts},
		}
		if err := store.PutInstancesBulk(ctx, batch); err != nil {
			t.Fatalf("PutInstancesBulk: %v", err)
		}
		if err := store.PutInstancesBulk(ctx, nil); err != nil {
			t.Fatalf("PutInstancesBulk(nil): %v", err)
		}
		got, err := store.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(instances) = %d, want 2", len(got))
		}
	})

	t.Run("AppendMetricSnapshotsBulk inserts a batch", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "m-grp", Name: "M", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "m-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
		}
		ts := time.Date(2026, time.April, 2, 10, 0, 0, 0, time.UTC)
		batch := []storage.MetricSnapshotRecord{
			{ID: "s1", AgentID: agent.ID, InstanceID: "", CapturedAt: ts, Values: map[string]uint64{"cpu": 1}},
			{ID: "s2", AgentID: agent.ID, InstanceID: "", CapturedAt: ts.Add(time.Second), Values: map[string]uint64{"cpu": 2}},
		}
		if err := store.AppendMetricSnapshotsBulk(ctx, batch); err != nil {
			t.Fatalf("AppendMetricSnapshotsBulk: %v", err)
		}
		if err := store.AppendMetricSnapshotsBulk(ctx, nil); err != nil {
			t.Fatalf("AppendMetricSnapshotsBulk(nil): %v", err)
		}
		got, err := store.ListMetricSnapshots(ctx)
		if err != nil {
			t.Fatalf("ListMetricSnapshots: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(snapshots) = %d, want 2", len(got))
		}
	})

	t.Run("AppendServerLoadPointsBulk inserts and de-dupes on (agent,captured_at)", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "sl-grp", Name: "SL", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "sl-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
		}

		// Probe: if the backend does not actually persist timeseries data
		// (the in-memory contract stub uses no-op stubs), skip the list-based
		// assertions. This keeps the same contract runnable against both the
		// production backends and the lightweight memoryStore fixture.
		probe := storage.ServerLoadPointRecord{AgentID: agent.ID, CapturedAt: time.Now().UTC(), SampleCount: 1}
		if err := store.AppendServerLoadPoint(ctx, probe); err != nil {
			t.Fatalf("AppendServerLoadPoint(probe): %v", err)
		}
		seen, err := store.ListServerLoadPoints(ctx, agent.ID, probe.CapturedAt.Add(-time.Hour), probe.CapturedAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListServerLoadPoints(probe): %v", err)
		}
		persistent := len(seen) > 0

		ts := time.Date(2026, time.April, 2, 11, 0, 0, 0, time.UTC)
		batch := []storage.ServerLoadPointRecord{
			{AgentID: agent.ID, CapturedAt: ts, SampleCount: 1},
			{AgentID: agent.ID, CapturedAt: ts.Add(time.Minute), SampleCount: 1},
			// Duplicate key: same agent + captured_at as first row. Must be
			// ignored by the ON CONFLICT DO NOTHING semantics.
			{AgentID: agent.ID, CapturedAt: ts, SampleCount: 99},
		}
		if err := store.AppendServerLoadPointsBulk(ctx, batch); err != nil {
			t.Fatalf("AppendServerLoadPointsBulk: %v", err)
		}
		if err := store.AppendServerLoadPointsBulk(ctx, nil); err != nil {
			t.Fatalf("AppendServerLoadPointsBulk(nil): %v", err)
		}
		if !persistent {
			return
		}
		got, err := store.ListServerLoadPoints(ctx, agent.ID, ts.Add(-time.Hour), ts.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListServerLoadPoints: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(server_load) = %d, want 2 (conflict ignored)", len(got))
		}
	})

	t.Run("AppendDCHealthPointsBulk inserts a batch", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "dc-grp", Name: "DC", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "dc-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
		}

		// Persistence probe — see the server_load bulk test for the rationale.
		probe := storage.DCHealthPointRecord{AgentID: agent.ID, CapturedAt: time.Now().UTC(), DC: 99, SampleCount: 1}
		if err := store.AppendDCHealthPoint(ctx, probe); err != nil {
			t.Fatalf("AppendDCHealthPoint(probe): %v", err)
		}
		seen, err := store.ListDCHealthPoints(ctx, agent.ID, probe.CapturedAt.Add(-time.Hour), probe.CapturedAt.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListDCHealthPoints(probe): %v", err)
		}
		persistent := len(seen) > 0

		ts := time.Date(2026, time.April, 2, 12, 0, 0, 0, time.UTC)
		batch := []storage.DCHealthPointRecord{
			{AgentID: agent.ID, CapturedAt: ts, DC: 2, SampleCount: 1},
			{AgentID: agent.ID, CapturedAt: ts, DC: 3, SampleCount: 1},
		}
		if err := store.AppendDCHealthPointsBulk(ctx, batch); err != nil {
			t.Fatalf("AppendDCHealthPointsBulk: %v", err)
		}
		if err := store.AppendDCHealthPointsBulk(ctx, nil); err != nil {
			t.Fatalf("AppendDCHealthPointsBulk(nil): %v", err)
		}
		if !persistent {
			return
		}
		got, err := store.ListDCHealthPoints(ctx, agent.ID, ts.Add(-time.Hour), ts.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListDCHealthPoints: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("len(dc_health) = %d, want 2", len(got))
		}
	})

	t.Run("UpsertClientIPHistoryBulk upserts a batch and updates last_seen on conflict", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		group := storage.FleetGroupRecord{ID: "ip-grp", Name: "IP", CreatedAt: time.Now().UTC()}
		if err := store.PutFleetGroup(ctx, group); err != nil {
			t.Fatalf("PutFleetGroup: %v", err)
		}
		agent := storage.AgentRecord{ID: "ip-agent", NodeName: "n", FleetGroupID: group.ID, LastSeenAt: time.Now().UTC()}
		if err := store.PutAgent(ctx, agent); err != nil {
			t.Fatalf("PutAgent: %v", err)
		}
		client := storage.ClientRecord{
			ID: "ip-client", Name: "alice", SecretCiphertext: "s", UserADTag: "0123456789abcdef0123456789abcdef",
			Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := store.PutClient(ctx, client); err != nil {
			t.Fatalf("PutClient: %v", err)
		}

		first := time.Date(2026, time.April, 2, 13, 0, 0, 0, time.UTC)
		later := first.Add(5 * time.Minute)

		// Persistence probe — uses an IP outside the batch set and a timestamp
		// inside the subsequent list window so we can detect whether the
		// backend actually persists rows.
		probeTime := first.Add(-30 * time.Minute)
		probe := storage.ClientIPHistoryRecord{AgentID: agent.ID, ClientID: client.ID, IPAddress: "127.0.0.254", FirstSeen: probeTime, LastSeen: probeTime}
		if err := store.UpsertClientIPHistory(ctx, probe); err != nil {
			t.Fatalf("UpsertClientIPHistory(probe): %v", err)
		}
		seen, err := store.ListClientIPHistory(ctx, client.ID, first.Add(-time.Hour), later.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListClientIPHistory(probe): %v", err)
		}
		persistent := len(seen) > 0
		batch := []storage.ClientIPHistoryRecord{
			{AgentID: agent.ID, ClientID: client.ID, IPAddress: "10.0.0.1", FirstSeen: first, LastSeen: first},
			{AgentID: agent.ID, ClientID: client.ID, IPAddress: "10.0.0.2", FirstSeen: first, LastSeen: first},
			// Duplicate key (same agent+client+ip as first row). last_seen
			// must advance via the ON CONFLICT DO UPDATE clause.
			{AgentID: agent.ID, ClientID: client.ID, IPAddress: "10.0.0.1", FirstSeen: first, LastSeen: later},
		}
		if err := store.UpsertClientIPHistoryBulk(ctx, batch); err != nil {
			t.Fatalf("UpsertClientIPHistoryBulk: %v", err)
		}
		if err := store.UpsertClientIPHistoryBulk(ctx, nil); err != nil {
			t.Fatalf("UpsertClientIPHistoryBulk(nil): %v", err)
		}
		if !persistent {
			return
		}
		got, err := store.ListClientIPHistory(ctx, client.ID, first.Add(-time.Hour), later.Add(time.Hour))
		if err != nil {
			t.Fatalf("ListClientIPHistory: %v", err)
		}
		// 3 distinct (agent,client,ip) combos: probe 127.0.0.254, 10.0.0.1, 10.0.0.2.
		if len(got) != 3 {
			t.Fatalf("len(ip_history) = %d, want 3 (probe + 2 from batch, conflict collapses)", len(got))
		}
		var first10 storage.ClientIPHistoryRecord
		for _, r := range got {
			if r.IPAddress == "10.0.0.1" {
				first10 = r
			}
		}
		if !first10.LastSeen.Equal(later) {
			t.Fatalf("10.0.0.1 last_seen = %v, want %v (updated on conflict)", first10.LastSeen, later)
		}
	})
}
