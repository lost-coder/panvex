package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// Reuses the package's fakeRestarter (config_apply_test.go), which already
// satisfies the unexported configRestarter interface.

func runtimeRestartJob(id string) *gatewayrpc.JobCommand {
	return &gatewayrpc.JobCommand{Id: id, Action: "runtime.restart"}
}

func TestHandleRuntimeRestartJobSuccess(t *testing.T) {
	a := New(Config{AgentID: "a1"}, &fakeTelemtClient{})
	fr := &fakeRestarter{}
	a.restarter = fr

	res := a.HandleJob(context.Background(), runtimeRestartJob("j1"), time.Now())

	if !res.Success {
		t.Fatalf("want success, got message %q", res.Message)
	}
	if fr.restarts != 1 {
		t.Fatalf("Restart invoked %d times, want 1", fr.restarts)
	}
}

func TestHandleRuntimeRestartJobNoStrategy(t *testing.T) {
	// No restart strategy → nil restarter → typed failure, not silent success.
	a := New(Config{AgentID: "a1"}, &fakeTelemtClient{})
	a.restarter = nil

	res := a.HandleJob(context.Background(), runtimeRestartJob("j2"), time.Now())

	if res.Success {
		t.Fatal("want failure when no restart strategy is configured")
	}
	if res.Message == "" {
		t.Fatal("want a non-empty failure message")
	}
}

func TestHandleRuntimeRestartJobRestartError(t *testing.T) {
	a := New(Config{AgentID: "a1"}, &fakeTelemtClient{})
	a.restarter = &fakeRestarter{restartErr: errors.New("systemctl: boom")}

	res := a.HandleJob(context.Background(), runtimeRestartJob("j3"), time.Now())

	if res.Success {
		t.Fatal("want failure when the restart command errors")
	}
}
