package agents

import (
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// RuntimeLifecycleState derives the coarse-grained lifecycle label used
// by the UI from the raw gateway runtime snapshot. Pure helper.
func RuntimeLifecycleState(snapshot *gatewayrpc.RuntimeSnapshot) string {
	switch {
	case snapshot == nil:
		return "unknown"
	case snapshot.Degraded:
		return "degraded"
	case snapshot.InitializationStatus != "" && snapshot.InitializationStatus != "ready":
		return snapshot.InitializationStatus
	case snapshot.StartupStatus != "" && snapshot.StartupStatus != "ready":
		return snapshot.StartupStatus
	case !snapshot.AcceptingNewConnections || !snapshot.MeRuntimeReady:
		return "starting"
	default:
		return "ready"
	}
}
