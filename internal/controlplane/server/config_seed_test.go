package server

import (
	"context"
	"testing"
	"time"
)

func TestSeedDesiredConfigSeedsOnce(t *testing.T) {
	s := testServerWithSQLite(t, time.Now())
	obs := map[string]any{"general": map[string]any{"log_level": "silent"}}

	if err := s.seedDesiredConfig(context.Background(), "agent-1", "3.4.25", obs); err != nil {
		t.Fatal(err)
	}
	got, _ := s.configTargets.Sections(context.Background(), "agent", "agent-1")
	if got["__schema_version"] != "3.4.25" {
		t.Fatalf("marker missing: %#v", got)
	}
	// a second seed with different observed must NOT overwrite
	obs2 := map[string]any{"general": map[string]any{"log_level": "debug"}}
	_ = s.seedDesiredConfig(context.Background(), "agent-1", "3.4.25", obs2)
	got2, _ := s.configTargets.Sections(context.Background(), "agent", "agent-1")
	if got2["general"].(map[string]any)["log_level"] != "silent" {
		t.Fatal("seed overwrote an existing snapshot")
	}
}

func TestSeedReplacesLegacySparseRow(t *testing.T) {
	s := testServerWithSQLite(t, time.Now())
	// legacy sparse row: no __schema_version
	_ = s.configTargets.Upsert(context.Background(), "agent", "agent-1",
		map[string]any{"general": map[string]any{"ad_tag": "legacy"}})
	obs := map[string]any{"general": map[string]any{"log_level": "silent"}}
	_ = s.seedDesiredConfig(context.Background(), "agent-1", "3.4.25", obs)
	got, _ := s.configTargets.Sections(context.Background(), "agent", "agent-1")
	if got["__schema_version"] != "3.4.25" || got["general"].(map[string]any)["ad_tag"] != nil {
		t.Fatalf("legacy row not replaced by full snapshot: %#v", got)
	}
}
