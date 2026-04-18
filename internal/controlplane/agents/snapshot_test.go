package agents

import (
	"testing"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func TestRuntimeLifecycleState(t *testing.T) {
	cases := []struct {
		name string
		in   *gatewayrpc.RuntimeSnapshot
		want string
	}{
		{"nil snapshot reports unknown", nil, "unknown"},
		{
			"degraded wins over everything else",
			&gatewayrpc.RuntimeSnapshot{Degraded: true, AcceptingNewConnections: true, MeRuntimeReady: true, StartupStatus: "ready", InitializationStatus: "ready"},
			"degraded",
		},
		{
			"initialization status surfaces when not ready",
			&gatewayrpc.RuntimeSnapshot{InitializationStatus: "initializing", StartupStatus: "ready"},
			"initializing",
		},
		{
			"startup status surfaces when init is ready but startup is not",
			&gatewayrpc.RuntimeSnapshot{InitializationStatus: "ready", StartupStatus: "booting"},
			"booting",
		},
		{
			"starting when not accepting connections yet",
			&gatewayrpc.RuntimeSnapshot{InitializationStatus: "ready", StartupStatus: "ready", AcceptingNewConnections: false, MeRuntimeReady: true},
			"starting",
		},
		{
			"starting when me runtime not ready",
			&gatewayrpc.RuntimeSnapshot{InitializationStatus: "ready", StartupStatus: "ready", AcceptingNewConnections: true, MeRuntimeReady: false},
			"starting",
		},
		{
			"ready when all checks pass",
			&gatewayrpc.RuntimeSnapshot{InitializationStatus: "ready", StartupStatus: "ready", AcceptingNewConnections: true, MeRuntimeReady: true},
			"ready",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RuntimeLifecycleState(c.in); got != c.want {
				t.Fatalf("RuntimeLifecycleState() = %q, want %q", got, c.want)
			}
		})
	}
}
