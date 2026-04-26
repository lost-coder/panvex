package storagetest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runSettingsContract extracts the panel/retention/update settings contract blocks from
// the historic store_contract.go monolith (R-Q-18). RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runSettingsContract(t *testing.T, open OpenStore) {
	t.Helper()

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


}
