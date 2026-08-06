package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

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

	outcome, err := telemtUpdateExecute(ctx, payload, info.Version, a.telemt, telemtrestart.ExecRunner{}, slog.Default())
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
	// Force the next runtime-snapshot cycle to refetch slow diagnostics
	// (which includes Telemt's reported version) instead of serving the
	// pre-update value out of the slow-data cache until its TTL expires.
	a.telemt.InvalidateSlowDataCache()
	return result
}
