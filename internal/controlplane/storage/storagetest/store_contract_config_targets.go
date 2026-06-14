package storagetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// runAgentConfigTargetContract exercises the agent_config_targets table:
// upsert → get → list → replace → delete → get-not-found. RunStoreContract
// dispatches into it so each backend exercises the same coverage.
func runAgentConfigTargetContract(t *testing.T, open OpenStore) {
	t.Helper()

	t.Run("agent config target round trip", func(t *testing.T) {
		st := open(t)
		defer st.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		rec := storage.AgentConfigTargetRecord{
			ScopeType: storage.ConfigScopeGroup, ScopeID: "grp-1",
			SectionsJSON: `{"censorship":{"tls_domain":"a.com"}}`,
			CreatedAt:    now, UpdatedAt: now,
		}
		if err := st.UpsertAgentConfigTarget(ctx, rec); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		got, err := st.GetAgentConfigTarget(ctx, storage.ConfigScopeGroup, "grp-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.SectionsJSON != rec.SectionsJSON {
			t.Fatalf("sections = %q, want %q", got.SectionsJSON, rec.SectionsJSON)
		}

		rec.SectionsJSON = `{"general":{"log_level":"debug"}}`
		rec.UpdatedAt = now.Add(time.Minute)
		if err := st.UpsertAgentConfigTarget(ctx, rec); err != nil {
			t.Fatalf("re-upsert: %v", err)
		}
		got, _ = st.GetAgentConfigTarget(ctx, storage.ConfigScopeGroup, "grp-1")
		if got.SectionsJSON != `{"general":{"log_level":"debug"}}` {
			t.Fatalf("replace failed: %q", got.SectionsJSON)
		}

		list, err := st.ListAgentConfigTargets(ctx)
		if err != nil || len(list) != 1 {
			t.Fatalf("list = %d (err %v), want 1", len(list), err)
		}

		n, err := st.DeleteAgentConfigTarget(ctx, storage.ConfigScopeGroup, "grp-1")
		if err != nil || n != 1 {
			t.Fatalf("delete = %d (err %v), want 1", n, err)
		}
		if _, err := st.GetAgentConfigTarget(ctx, storage.ConfigScopeGroup, "grp-1"); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("get after delete: want ErrNotFound, got %v", err)
		}
	})
}
