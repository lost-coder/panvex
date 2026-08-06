package storagetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	t.Run("full snapshot config target round trip", func(t *testing.T) {
		st := open(t)
		defer st.Close()

		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Second)

		// A realistic P2 full snapshot: the __schema_version marker plus
		// several config sections, as seedDesiredConfig would write for an
		// agent's first observed config (see server/config_seed.go).
		snapshot := map[string]any{
			"__schema_version": "3.4.25",
			"general": map[string]any{
				"log_level": "info",
				"ad_tag":    "abc123",
			},
			"censorship": map[string]any{
				"tls_domain":     "a.com",
				"fake_tls_ports": []any{"443", "8443"},
			},
			"network": map[string]any{
				"listen_addr": "0.0.0.0:443",
				"max_conns":   float64(4096),
			},
			"limits": map[string]any{
				"max_tcp_conns":  float64(1000),
				"max_unique_ips": float64(200),
			},
			"logging": map[string]any{
				"target": "stdout",
				"format": "json",
			},
		}
		body, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatalf("marshal snapshot: %v", err)
		}

		rec := storage.AgentConfigTargetRecord{
			ScopeType: storage.ConfigScopeAgent, ScopeID: "agent-full-1",
			SectionsJSON: string(body),
			CreatedAt:    now, UpdatedAt: now,
		}
		if err := st.UpsertAgentConfigTarget(ctx, rec); err != nil {
			t.Fatalf("upsert: %v", err)
		}

		got, err := st.GetAgentConfigTarget(ctx, storage.ConfigScopeAgent, "agent-full-1")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.SectionsJSON != rec.SectionsJSON {
			t.Fatalf("sections not byte-stable:\n got  %q\n want %q", got.SectionsJSON, rec.SectionsJSON)
		}
		if !got.CreatedAt.Equal(now) {
			t.Fatalf("created_at = %v, want %v", got.CreatedAt, now)
		}

		// Re-upsert with an edited snapshot: CreatedAt must be preserved,
		// UpdatedAt must advance.
		snapshot["general"].(map[string]any)["log_level"] = "debug"
		body2, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatalf("marshal snapshot 2: %v", err)
		}
		rec.SectionsJSON = string(body2)
		rec.UpdatedAt = now.Add(time.Minute)
		if err := st.UpsertAgentConfigTarget(ctx, rec); err != nil {
			t.Fatalf("re-upsert: %v", err)
		}

		got, err = st.GetAgentConfigTarget(ctx, storage.ConfigScopeAgent, "agent-full-1")
		if err != nil {
			t.Fatalf("get after re-upsert: %v", err)
		}
		if got.SectionsJSON != rec.SectionsJSON {
			t.Fatalf("sections not byte-stable after re-upsert:\n got  %q\n want %q", got.SectionsJSON, rec.SectionsJSON)
		}
		if !got.CreatedAt.Equal(now) {
			t.Fatalf("created_at not preserved on re-upsert: got %v, want %v", got.CreatedAt, now)
		}
		if !got.UpdatedAt.Equal(now.Add(time.Minute)) {
			t.Fatalf("updated_at not advanced: got %v, want %v", got.UpdatedAt, now.Add(time.Minute))
		}

		// A large snapshot (~190 leaf keys, 10 sections x 19 keys) must round
		// trip intact — guards against truncation or driver-level JSON
		// reformatting on either backend.
		const sections, keysPerSection = 10, 19
		big := make(map[string]any, sections)
		for s := 0; s < sections; s++ {
			section := make(map[string]any, keysPerSection)
			for k := 0; k < keysPerSection; k++ {
				section[fmt.Sprintf("key_%02d", k)] = fmt.Sprintf("val_%d_%d", s, k)
			}
			big[fmt.Sprintf("section_%02d", s)] = section
		}
		bigBody, err := json.Marshal(big)
		if err != nil {
			t.Fatalf("marshal big snapshot: %v", err)
		}

		bigRec := storage.AgentConfigTargetRecord{
			ScopeType: storage.ConfigScopeAgent, ScopeID: "agent-full-big",
			SectionsJSON: string(bigBody),
			CreatedAt:    now, UpdatedAt: now,
		}
		if err := st.UpsertAgentConfigTarget(ctx, bigRec); err != nil {
			t.Fatalf("upsert big: %v", err)
		}
		gotBig, err := st.GetAgentConfigTarget(ctx, storage.ConfigScopeAgent, "agent-full-big")
		if err != nil {
			t.Fatalf("get big: %v", err)
		}
		if gotBig.SectionsJSON != bigRec.SectionsJSON {
			t.Fatalf("big sections not byte-stable")
		}
		var decoded map[string]map[string]string
		if err := json.Unmarshal([]byte(gotBig.SectionsJSON), &decoded); err != nil {
			t.Fatalf("decode big sections: %v", err)
		}
		leafKeys := 0
		for _, section := range decoded {
			leafKeys += len(section)
		}
		if leafKeys != sections*keysPerSection {
			t.Fatalf("leaf keys = %d, want %d", leafKeys, sections*keysPerSection)
		}
	})
}
