package storagetest

import (
	"context"
	"encoding/json"
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
}
