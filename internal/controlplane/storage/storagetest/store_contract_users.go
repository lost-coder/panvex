package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runUsersContract extracts the users + user-appearance contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runUsersContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("user create and load round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()
		user := storage.UserRecord{ //nolint:gosec // synthetic test fixture, not a real credential
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
		user := storage.UserRecord{ //nolint:gosec // synthetic test fixture, not a real credential
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

		if _, err := store.GetUserByID(ctx, user.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("GetUserByID() after DeleteUser error = %v, want %v", err, storage.ErrNotFound)
		}
	})

	t.Run("user appearance defaults and round trip", func(t *testing.T) {
		store := open(t)
		defer store.Close()

		ctx := context.Background()

		defaultAppearance, err := store.GetUserAppearance(ctx, fixtureUserAppearanceKey)
		if err != nil {
			t.Fatalf("GetUserAppearance(default) error = %v", err)
		}
		if defaultAppearance.UserID != fixtureUserAppearanceKey {
			t.Fatalf("GetUserAppearance(default) UserID = %q, want %q", defaultAppearance.UserID, fixtureUserAppearanceKey)
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

}
