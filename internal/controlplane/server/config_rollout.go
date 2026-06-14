package server

import "context"

// RollingResult summarizes a rolling apply.
type RollingResult struct {
	Applied int    // agents that applied successfully
	Failed  string // agent id that failed (empty if none)
	Err     error  // the failure (nil if all applied)
}

// rollingApply applies to agents one at a time, stopping at the first failure so
// a bad config never lands on more than one node. `apply` performs the per-agent
// apply and returns an error on failure. Respects ctx cancellation between steps.
func rollingApply(ctx context.Context, agentIDs []string, apply func(context.Context, string) error) RollingResult {
	var res RollingResult
	for _, id := range agentIDs {
		if err := ctx.Err(); err != nil {
			res.Err = err
			res.Failed = id
			return res
		}
		if err := apply(ctx, id); err != nil {
			res.Failed = id
			res.Err = err
			return res
		}
		res.Applied++
	}
	return res
}
