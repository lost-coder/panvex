package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

const defaultReloadDrainSecs = 30

// reloadPolicy is the operator-chosen session policy for a config apply.
type reloadPolicy struct {
	Mode        string // "instant" | "drain"
	TimeoutSecs int    // drain only
}

// parseReloadPolicy validates a mode/timeout pair, mirroring Telemt's
// maestro/reload.rs:48-53. An empty mode defaults to drain with a 30s window
// so a caller that never chose (hot change that turned out to need reload)
// still gets the session-preserving path.
func parseReloadPolicy(mode string, timeoutSecs int) (reloadPolicy, error) {
	switch mode {
	case "":
		return reloadPolicy{Mode: "drain", TimeoutSecs: defaultReloadDrainSecs}, nil
	case "instant":
		if timeoutSecs != 0 {
			return reloadPolicy{}, fmt.Errorf("timeout_secs is only valid with drain")
		}
		return reloadPolicy{Mode: "instant"}, nil
	case "drain":
		if timeoutSecs < 1 || timeoutSecs > 3600 {
			return reloadPolicy{}, fmt.Errorf("drain timeout_secs must be within 1..3600")
		}
		return reloadPolicy{Mode: "drain", TimeoutSecs: timeoutSecs}, nil
	default:
		return reloadPolicy{}, fmt.Errorf("reload mode must be instant or drain")
	}
}

// reloadPolicyRequest is the optional JSON body accepted by the config-apply
// handlers (handleApplyAgentConfig, handleApplyGroupConfig) to let the
// operator choose the session policy for the reload the agent performs.
type reloadPolicyRequest struct {
	ReloadMode        string `json:"reload_mode"`
	ReloadTimeoutSecs int    `json:"reload_timeout_secs"`
}

// decodeReloadPolicyBody decodes the request's optional JSON body into a
// reloadPolicy. An absent or empty body (the existing callers, and any
// caller that never picked a policy) decodes as zero values, which
// parseReloadPolicy resolves to the drain/30s default — so this is safe to
// call unconditionally without breaking callers that send no body at all.
func decodeReloadPolicyBody(r *http.Request) (reloadPolicy, error) {
	var req reloadPolicyRequest
	if err := decodeJSON(r, &req); err != nil {
		if errors.Is(err, io.EOF) {
			return parseReloadPolicy("", 0)
		}
		return reloadPolicy{}, err
	}
	return parseReloadPolicy(req.ReloadMode, req.ReloadTimeoutSecs)
}
