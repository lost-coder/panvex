package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lost-coder/panvex/internal/agent/telemtrestart"
	"github.com/lost-coder/panvex/internal/agent/telemtupdate"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// telemtUpdateExecute is swapped in tests so handler-level behaviour can be
// exercised without touching the network or the filesystem (mirrors
// selfUpdateExecute's pattern for agent.self-update).
var telemtUpdateExecute = telemtupdate.Execute

// handleTelemtUpdateJob runs the telemt.update action: it resolves the
// currently-running Telemt version, delegates the download/verify/swap/
// health-gate flow to telemtupdate.Execute, and turns the Outcome into a
// JobResult. On success, Outcome.Message (which folds in KeepBackup /
// rollback detail on the failure paths that populate it) is used verbatim;
// see the error branch below for why failures need an err.Error() fallback.
func (a *Agent) handleTelemtUpdateJob(ctx context.Context, job *gatewayrpc.JobCommand, result *gatewayrpc.JobResult) *gatewayrpc.JobResult {
	var payload telemtupdate.Payload
	if err := json.Unmarshal([]byte(job.GetPayloadJson()), &payload); err != nil {
		result.Message = fmt.Sprintf("telemt.update: invalid payload: %v", err)
		return result
	}

	info, err := a.telemt.FetchSystemInfo(ctx)
	if err != nil {
		result.Message = fmt.Sprintf("cannot read current telemt version: %v", err)
		return result
	}

	outcome, err := telemtUpdateExecute(ctx, payload, info.Version, a.telemt, telemtrestart.ExecRunner{}, a.logger)
	if err != nil {
		// Not every error branch inside Execute populates Outcome.Message
		// (e.g. the downgrade gate, asset resolution, download, checksum,
		// and swap failures all return a zero Outcome alongside err) —
		// only the later restart/health-gate branches do, because those
		// need to communicate KeepBackup detail. Fall back to err.Error()
		// so a failure never surfaces as a blank JobResult message.
		result.Message = outcome.Message
		if result.Message == "" {
			result.Message = err.Error()
		}
		return result
	}

	result.Success = true
	result.Message = outcome.Message
	// After a successful in-place update, the panel must show the new Telemt
	// version promptly — otherwise the "update available" badge and the
	// displayed version keep showing the old release even though the node is
	// already upgraded, which reads as "the update did nothing".
	//
	// Two things gate that version reaching the panel:
	//   1. The agent serves Telemt's reported version out of a slow-data cache
	//      until its TTL expires — invalidate it so the next runtime snapshot
	//      refetches /v1/system/info and sees the new version.
	//   2. The diagnostics body (which carries system_info.version) is
	//      delta-gated by content hash. The restart during the update drops
	//      the agent<->Telemt connection and emits unreachable snapshots, and
	//      the panel blanks its stored diagnostics row on unreachable; the
	//      agent-side gate, however, still remembers the last-sent hash, so a
	//      plain refetch may not re-send the body. Reset the delta gates so the
	//      next snapshot re-sends the full diagnostics body unconditionally.
	// Instance.version (the fast, non-gated path) already updates on the very
	// next snapshot; this closes the slow diagnostics path the UI badge reads.
	a.telemt.InvalidateSlowDataCache()
	a.ResetDeltaGates()
	return result
}
