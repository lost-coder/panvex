package server

import (
	"context"

	"github.com/lost-coder/panvex/internal/controlplane/storage"
)

// applyGroupToAgent pushes a fleet group's config sections to a single agent
// as an agent-scoped batch-of-one, bypassing applyDiff (enqueueConfigApplyJob's
// usual patch source). A node that just joined a group has not yet stored the
// group's values in its own desired snapshot, so applyDiff(agent's desired,
// observed) would not surface them — the group sections must be pushed
// directly. Used by the group-membership-change hook (P3) to catch a node up
// on join.
//
// groupID == "" (agent not in a group, e.g. after leaving one) and an empty
// group config both yield a no-op ("", nil), mirroring the empty-diff no-op in
// enqueueConfigApplyJob.
func (s *Server) applyGroupToAgent(ctx context.Context, actorID, agentID, groupID string) (string, error) {
	if groupID == "" {
		return "", nil
	}
	sections, err := s.configTargets.Sections(ctx, storage.ConfigScopeGroup, groupID)
	if err != nil {
		return "", err
	}
	if len(sections) == 0 {
		return "", nil
	}
	return s.createConfigApplyBatchWithPatch(ctx, actorID, "", []string{agentID},
		reloadPolicy{Mode: "drain", TimeoutSecs: defaultReloadDrainSecs}, sections)
}
