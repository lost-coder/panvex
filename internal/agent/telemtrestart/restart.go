// Package telemtrestart restarts the local Telemt process for the agent.
//
// The agent runs as a host process (systemd/procd/openrc/runit service), so it
// restarts Telemt with a deterministic host command — e.g. `systemctl restart
// <unit>` — which is stop+start and does not depend on the supervisor's own
// restart policy. A raw `command:` escape hatch covers other supervisors. We
// never rely on Telemt self-exiting and a policy bringing it back.
//
// Spec parsing (validating the strategy string and turning it into an argv)
// lives in internal/restartspec, which the control-plane also imports to
// validate an operator-supplied strategy without pulling this package's
// os/exec dependency into the panel binary. This package only adds execution.
package telemtrestart

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/lost-coder/panvex/internal/restartspec"
)

// CommandRunner runs an external command. Injectable for tests.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// ExecRunner runs commands via os/exec.
type ExecRunner struct{}

// Run executes name+args, attaching combined output to any error.
func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // G204: operator-configured restart strategy (systemd/procd/openrc/runit/command), not untrusted input
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Restarter restarts Telemt via a pre-parsed restart command.
type Restarter struct {
	cmd    []string
	runner CommandRunner
}

// New parses spec via restartspec.Parse and pairs the resulting command with
// runner. A blank spec surfaces as a wrapped restartspec.ErrEmpty so callers
// can distinguish "no strategy configured" from other parse failures with
// errors.Is.
func New(spec string, runner CommandRunner) (*Restarter, error) {
	cmd, err := restartspec.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("telemtrestart: %w", err)
	}
	return &Restarter{cmd: cmd, runner: runner}, nil
}

// Restart stop+starts Telemt via the configured command. Runner errors are
// wrapped with the command text so logs/alerts show what was attempted even
// if the CommandRunner implementation doesn't include it itself.
func (r *Restarter) Restart(ctx context.Context) error {
	if err := r.runner.Run(ctx, r.cmd[0], r.cmd[1:]...); err != nil {
		return fmt.Errorf("telemtrestart: restart via %q: %w", strings.Join(r.cmd, " "), err)
	}
	return nil
}
