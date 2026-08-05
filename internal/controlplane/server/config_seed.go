package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// schemaVersionMarker is the key stamped into a config-target snapshot's
// sections map to record the Telemt schema version the snapshot was seeded
// from. Its presence distinguishes a P2 full-snapshot row from a legacy
// sparse override row.
const schemaVersionMarker = "__schema_version"

// seedDesiredConfig writes the agent's first observed config as its desired
// snapshot, stamping schemaVersionMarker with telemtVersion. It is a no-op
// if a P2 snapshot (one carrying schemaVersionMarker) already exists for the
// agent, so it is safe to call on every observation without clobbering
// operator edits. A legacy sparse row (no marker) is replaced wholesale.
func (s *Server) seedDesiredConfig(ctx context.Context, agentID, telemtVersion string, observed map[string]any) error {
	existing, err := s.configTargets.Sections(ctx, storage.ConfigScopeAgent, agentID)
	if err != nil {
		return err
	}
	if _, seeded := existing[schemaVersionMarker]; seeded {
		return nil // already a P2 snapshot
	}
	snapshot := make(map[string]any, len(observed)+1)
	for k, v := range observed {
		snapshot[k] = v
	}
	snapshot[schemaVersionMarker] = telemtVersion
	return s.configTargets.Upsert(ctx, storage.ConfigScopeAgent, agentID, snapshot)
}

// strippedSnapshot returns a copy of sections with schemaVersionMarker
// removed, for callers that need the pure config sections without the P2
// bookkeeping key.
func strippedSnapshot(sections map[string]any) map[string]any {
	out := make(map[string]any, len(sections))
	for k, v := range sections {
		if k == schemaVersionMarker {
			continue
		}
		out[k] = v
	}
	return out
}
