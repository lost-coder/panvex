// Package telemtrestart restarts the local Telemt process for the agent.
//
// The agent runs as a host process (systemd service), so it restarts Telemt with
// deterministic host commands — `systemctl restart <unit>` or `docker restart
// <container>` — which are stop+start and do not depend on the supervisor's
// Restart= / restart: policy. A raw `command:` escape hatch covers other
// supervisors. We never rely on Telemt self-exiting and a policy bringing it back.
package telemtrestart

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoStrategy reports an empty/unconfigured restart strategy. Callers treat a
// node with no restart strategy as unable to apply restart-required changes.
var ErrNoStrategy = errors.New("telemtrestart: no restart strategy configured")

// CommandRunner runs an external command. Injectable for tests.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// ExecRunner runs commands via os/exec.
type ExecRunner struct{}

// Run executes name+args, attaching combined output to any error.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Restarter restarts Telemt and verifies the strategy is usable.
type Restarter struct {
	restartCmd []string
	verifyCmd  []string
	runner     CommandRunner
}

// Parse builds a Restarter from a strategy spec:
//   - "systemd:<unit>"      -> systemctl restart/status <unit>
//   - "docker:<container>"  -> docker restart/inspect <container>
//   - "command:<argv...>"   -> run the given command (space-split); Verify is a no-op
func Parse(spec string, runner CommandRunner) (*Restarter, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, ErrNoStrategy
	}
	kind, arg, ok := strings.Cut(spec, ":")
	arg = strings.TrimSpace(arg)
	if !ok || arg == "" {
		return nil, fmt.Errorf("telemtrestart: invalid strategy %q", spec)
	}
	switch kind {
	case "systemd":
		return &Restarter{
			restartCmd: []string{"systemctl", "restart", arg},
			verifyCmd:  []string{"systemctl", "status", arg},
			runner:     runner,
		}, nil
	case "docker":
		return &Restarter{
			restartCmd: []string{"docker", "restart", arg},
			verifyCmd:  []string{"docker", "inspect", arg},
			runner:     runner,
		}, nil
	case "command":
		fields := strings.Fields(arg)
		if len(fields) == 0 {
			return nil, fmt.Errorf("telemtrestart: empty command strategy")
		}
		return &Restarter{restartCmd: fields, runner: runner}, nil
	default:
		return nil, fmt.Errorf("telemtrestart: unknown strategy kind %q", kind)
	}
}

// Restart stop+starts Telemt via the configured command.
func (r *Restarter) Restart(ctx context.Context) error {
	return r.runner.Run(ctx, r.restartCmd[0], r.restartCmd[1:]...)
}

// Verify probes that the strategy is usable (unit/container exists and the agent
// has permission) without restarting. For the raw "command" strategy there is
// nothing safe to probe, so Verify succeeds (the command itself is the contract).
func (r *Restarter) Verify(ctx context.Context) error {
	if len(r.verifyCmd) == 0 {
		return nil
	}
	return r.runner.Run(ctx, r.verifyCmd[0], r.verifyCmd[1:]...)
}
