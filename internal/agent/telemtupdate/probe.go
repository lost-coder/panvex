package telemtupdate

import (
	"os"
	"strings"
)

// ProbeResult describes how (and whether) the agent can update the local
// Telemt install: which supervisor (if any) fronts the process, and — when
// no in-place update path exists — why.
type ProbeResult struct {
	// Mode is "binary" (a supported process supervisor owns telemt, so a
	// binary swap + supervised restart is safe), "docker" (telemt runs in
	// a container — updates must go through the image, not a local binary
	// swap), or "none" (no supported supervisor was detected).
	Mode string
	// SuggestedRestartSpec is the telemtrestart spec ("systemd:telemt",
	// "openrc:telemt", "procd:telemt", "runit:telemt") to use for the
	// supervised restart after a binary swap. Empty when Mode != "binary".
	SuggestedRestartSpec string
	// BinaryPath is the best-effort resolved path to the telemt executable,
	// used as the swap target. May be empty if it could not be resolved.
	BinaryPath string
	// Available reports whether an in-place update is offered at all.
	Available bool
	// Reason is a localizable CODE (not a human-readable phrase) explaining
	// why Available is false, e.g. "docker_only", "no_service_manager_detected".
	// Empty when Available is true.
	Reason string
}

// RunProbe detects which process supervisor (if any) fronts the local
// telemt process on this node. lookPath, stat and output are injected so
// callers can point them at exec.LookPath / os.Stat / a helper wrapping
// exec.Command(name, args...).Output() in production, and at deterministic
// fakes in tests.
//
// Detection order (first match wins):
//  1. systemd: `systemctl cat telemt` exits 0.
//  2. OpenRC/procd: /etc/init.d/telemt exists.
//  3. runit: `sv` is on PATH and /etc/service/telemt exists.
//  4. docker: `docker` is on PATH and a container named telemt is running.
//  5. none: nothing above matched.
func RunProbe(
	lookPath func(string) (string, error),
	stat func(string) (os.FileInfo, error),
	output func(name string, args ...string) ([]byte, error),
) ProbeResult {
	if _, err := lookPath("systemctl"); err == nil {
		if out, err := output("systemctl", "cat", "telemt"); err == nil {
			return ProbeResult{
				Mode:                 "binary",
				SuggestedRestartSpec: "systemd:telemt",
				BinaryPath:           resolveBinaryPath(parseExecStart(out), lookPath, ""),
				Available:            true,
			}
		}
	}

	if _, err := stat("/etc/init.d/telemt"); err == nil {
		spec := "procd:telemt"
		if _, err := lookPath("rc-service"); err == nil {
			spec = "openrc:telemt"
		}
		return ProbeResult{
			Mode:                 "binary",
			SuggestedRestartSpec: spec,
			BinaryPath:           resolveBinaryPath("", lookPath, "/usr/local/bin/telemt"),
			Available:            true,
		}
	}

	if _, err := lookPath("sv"); err == nil {
		if _, err := stat("/etc/service/telemt"); err == nil {
			return ProbeResult{
				Mode:                 "binary",
				SuggestedRestartSpec: "runit:telemt",
				BinaryPath:           resolveBinaryPath("", lookPath, ""),
				Available:            true,
			}
		}
	}

	if _, err := lookPath("docker"); err == nil {
		if out, err := output("docker", "ps", "--filter", "name=telemt", "--format", "{{.Names}}"); err == nil && strings.TrimSpace(string(out)) != "" {
			return ProbeResult{
				Mode:      "docker",
				Available: false,
				Reason:    "docker_only",
			}
		}
	}

	return ProbeResult{
		Mode:      "none",
		Available: false,
		Reason:    "no_service_manager_detected",
	}
}

// resolveBinaryPath prefers an already-parsed path (e.g. from ExecStart=),
// then falls back to a PATH lookup of "telemt", then to the given default
// (which may itself be empty when no sensible default exists).
func resolveBinaryPath(parsed string, lookPath func(string) (string, error), fallback string) string {
	if parsed != "" {
		return parsed
	}
	if path, err := lookPath("telemt"); err == nil && path != "" {
		return path
	}
	return fallback
}

// parseExecStart extracts the executable from a `systemctl cat` unit dump:
// the first whitespace-separated token following an "ExecStart=" line's "=".
// Returns "" if no such line is present.
func parseExecStart(catOutput []byte) string {
	const prefix = "ExecStart="
	for _, line := range strings.Split(string(catOutput), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, prefix))
		if len(fields) == 0 {
			continue
		}
		return fields[0]
	}
	return ""
}
