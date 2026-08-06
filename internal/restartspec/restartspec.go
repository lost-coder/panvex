// Package restartspec parses a Telemt restart-strategy spec string into an
// argv slice, without doing anything about actually running it. Keeping this
// pure (no os/exec, no side effects) lets the control-plane validate an
// operator-supplied strategy string with the exact same rules the agent will
// apply, without pulling os/exec-carrying agent packages into the panel
// binary. Execution lives in internal/agent/telemtrestart, which imports this
// package.
package restartspec

import (
	"errors"
	"fmt"
	"strings"
)

// ErrEmpty reports a blank (or whitespace-only) spec. Callers treat a node
// with no restart strategy configured as unable to apply restart-required
// changes.
var ErrEmpty = errors.New("restartspec: empty spec")

// Parse builds a restart argv from a strategy spec of the form
// "<kind>:<name>":
//   - "systemd:<unit>"  -> systemctl restart <unit>
//   - "procd:<service>" -> /etc/init.d/<service> restart   (OpenWrt)
//   - "openrc:<service>"-> rc-service <service> restart
//   - "runit:<service>" -> sv restart <service>
//   - "command:<argv>"  -> the given command, space-split
//
// For every preset except "command", name must be non-empty, contain no
// whitespace, and contain neither "/" nor ".." — it is substituted verbatim
// into a fixed command line (and, for procd, into a filesystem path), so
// those characters would otherwise let an operator-supplied string smuggle
// in extra arguments or escape the init.d directory.
func Parse(spec string) ([]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, ErrEmpty
	}
	kind, arg, ok := strings.Cut(spec, ":")
	if !ok {
		return nil, fmt.Errorf("restartspec: invalid spec %q: missing ':'", spec)
	}
	arg = strings.TrimSpace(arg)

	if kind == "command" {
		fields := strings.Fields(arg)
		if len(fields) == 0 {
			return nil, fmt.Errorf("restartspec: empty command strategy")
		}
		return fields, nil
	}

	if err := validateName(arg); err != nil {
		return nil, fmt.Errorf("restartspec: invalid spec %q: %w", spec, err)
	}

	switch kind {
	case "systemd":
		return []string{"systemctl", "restart", arg}, nil
	case "procd":
		return []string{"/etc/init.d/" + arg, "restart"}, nil
	case "openrc":
		return []string{"rc-service", arg, "restart"}, nil
	case "runit":
		return []string{"sv", "restart", arg}, nil
	default:
		return nil, fmt.Errorf("restartspec: unknown strategy kind %q", kind)
	}
}

// validateName rejects unit/service names that are empty, contain
// whitespace, or contain "/" or "..". These names are substituted into a
// fixed argv (and, for procd, a filesystem path), so the restriction blocks
// both argument injection and path traversal.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("empty name")
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("name %q contains whitespace", name)
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("name %q contains '/'", name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("name %q contains '..'", name)
	}
	return nil
}
