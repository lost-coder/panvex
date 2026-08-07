package telemtupdate

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// fakeLookPath returns a lookPath func backed by a map of name -> path.
// Names absent from the map behave as "not found" (exec.ErrNotFound-shaped).
func fakeLookPath(found map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if path, ok := found[name]; ok {
			return path, nil
		}
		return "", errors.New("executable file not found in $PATH")
	}
}

// fakeStat returns a stat func backed by a set of paths that "exist".
// Paths absent from the set behave as os.ErrNotExist.
func fakeStat(exists map[string]bool) func(string) (os.FileInfo, error) {
	return func(path string) (os.FileInfo, error) {
		if exists[path] {
			return nil, nil
		}
		return nil, os.ErrNotExist
	}
}

// fakeOutput returns an output func backed by a map keyed on the joined
// command line ("name arg1 arg2 ..."). Missing keys behave as a non-zero
// exit (error).
type fakeCmdResult struct {
	out []byte
	err error
}

func fakeOutput(results map[string]fakeCmdResult) func(string, ...string) ([]byte, error) {
	return func(name string, args ...string) ([]byte, error) {
		key := strings.Join(append([]string{name}, args...), " ")
		if r, ok := results[key]; ok {
			return r.out, r.err
		}
		return nil, errors.New("exit status 1")
	}
}

func TestRunProbe_SystemdBranch(t *testing.T) {
	lookPath := fakeLookPath(map[string]string{"systemctl": "/usr/bin/systemctl"})
	stat := fakeStat(nil)
	output := fakeOutput(map[string]fakeCmdResult{
		"systemctl cat telemt": {out: []byte("[Service]\nExecStart=/usr/local/bin/telemt --config /etc/telemt/config.toml\n")},
	})

	got := RunProbe(lookPath, stat, output)

	want := ProbeResult{
		Mode:                 "binary",
		SuggestedRestartSpec: "systemd:telemt",
		BinaryPath:           "/usr/local/bin/telemt",
		Available:            true,
		Reason:               "",
	}
	if got != want {
		t.Fatalf("RunProbe() = %+v, want %+v", got, want)
	}
}

func TestRunProbe_SystemdBranch_FallsBackToLookPathWhenExecStartUnparsable(t *testing.T) {
	lookPath := fakeLookPath(map[string]string{
		"systemctl": "/usr/bin/systemctl",
		"telemt":    "/opt/telemt/bin/telemt",
	})
	stat := fakeStat(nil)
	output := fakeOutput(map[string]fakeCmdResult{
		"systemctl cat telemt": {out: []byte("[Service]\nType=simple\n")},
	})

	got := RunProbe(lookPath, stat, output)

	if got.Mode != "binary" || got.SuggestedRestartSpec != "systemd:telemt" {
		t.Fatalf("RunProbe() = %+v, want binary/systemd:telemt", got)
	}
	if got.BinaryPath != "/opt/telemt/bin/telemt" {
		t.Fatalf("BinaryPath = %q, want lookPath(\"telemt\") fallback", got.BinaryPath)
	}
}

func TestRunProbe_SystemctlPresentButCatFails_FallsThroughToNextBranch(t *testing.T) {
	// systemctl is on PATH but `systemctl cat telemt` fails (unit not
	// installed) -- branch 1 must NOT match, and detection must continue
	// to branch 2.
	lookPath := fakeLookPath(map[string]string{
		"systemctl":  "/usr/bin/systemctl",
		"rc-service": "/usr/sbin/rc-service",
		"telemt":     "/usr/local/bin/telemt",
	})
	stat := fakeStat(map[string]bool{"/etc/init.d/telemt": true})
	output := fakeOutput(nil) // systemctl cat telemt -> exit 1 (not in map)

	got := RunProbe(lookPath, stat, output)

	want := ProbeResult{
		Mode:                 "binary",
		SuggestedRestartSpec: "openrc:telemt",
		BinaryPath:           "/usr/local/bin/telemt",
		Available:            true,
	}
	if got != want {
		t.Fatalf("RunProbe() = %+v, want %+v", got, want)
	}
}

func TestRunProbe_OpenRCBranch(t *testing.T) {
	lookPath := fakeLookPath(map[string]string{
		"rc-service": "/usr/sbin/rc-service",
		"telemt":     "/usr/local/bin/telemt",
	})
	stat := fakeStat(map[string]bool{"/etc/init.d/telemt": true})
	output := fakeOutput(nil)

	got := RunProbe(lookPath, stat, output)

	want := ProbeResult{
		Mode:                 "binary",
		SuggestedRestartSpec: "openrc:telemt",
		BinaryPath:           "/usr/local/bin/telemt",
		Available:            true,
	}
	if got != want {
		t.Fatalf("RunProbe() = %+v, want %+v", got, want)
	}
}

func TestRunProbe_ProcdBranch_WhenRCServiceAbsent(t *testing.T) {
	lookPath := fakeLookPath(map[string]string{"telemt": "/usr/local/bin/telemt"})
	stat := fakeStat(map[string]bool{"/etc/init.d/telemt": true})
	output := fakeOutput(nil)

	got := RunProbe(lookPath, stat, output)

	want := ProbeResult{
		Mode:                 "binary",
		SuggestedRestartSpec: "procd:telemt",
		BinaryPath:           "/usr/local/bin/telemt",
		Available:            true,
	}
	if got != want {
		t.Fatalf("RunProbe() = %+v, want %+v", got, want)
	}
}

func TestRunProbe_InitDBranch_BinaryPathFallsBackToHardcodedDefault(t *testing.T) {
	lookPath := fakeLookPath(nil) // "telemt" not found anywhere on PATH
	stat := fakeStat(map[string]bool{"/etc/init.d/telemt": true})
	output := fakeOutput(nil)

	got := RunProbe(lookPath, stat, output)

	if got.BinaryPath != "/usr/local/bin/telemt" {
		t.Fatalf("BinaryPath = %q, want hardcoded fallback /usr/local/bin/telemt", got.BinaryPath)
	}
	if got.SuggestedRestartSpec != "procd:telemt" {
		t.Fatalf("SuggestedRestartSpec = %q, want procd:telemt", got.SuggestedRestartSpec)
	}
}

func TestRunProbe_RunitBranch(t *testing.T) {
	lookPath := fakeLookPath(map[string]string{"sv": "/usr/bin/sv"})
	stat := fakeStat(map[string]bool{"/etc/service/telemt": true})
	output := fakeOutput(nil)

	got := RunProbe(lookPath, stat, output)

	if got.Mode != "binary" || got.SuggestedRestartSpec != "runit:telemt" || !got.Available {
		t.Fatalf("RunProbe() = %+v, want binary/runit:telemt/available", got)
	}
}

func TestRunProbe_RunitBranch_RequiresBothSvAndServiceDir(t *testing.T) {
	// sv present but no /etc/service/telemt dir: must NOT match branch 3.
	lookPath := fakeLookPath(map[string]string{"sv": "/usr/bin/sv"})
	stat := fakeStat(nil)
	output := fakeOutput(nil)

	got := RunProbe(lookPath, stat, output)

	if got.Mode == "binary" {
		t.Fatalf("RunProbe() = %+v, must not match runit branch without /etc/service/telemt", got)
	}
}

func TestRunProbe_DockerBranch(t *testing.T) {
	lookPath := fakeLookPath(map[string]string{"docker": "/usr/bin/docker"})
	stat := fakeStat(nil)
	output := fakeOutput(map[string]fakeCmdResult{
		"docker ps --filter name=telemt --format {{.Names}}": {out: []byte("telemt\n")},
	})

	got := RunProbe(lookPath, stat, output)

	want := ProbeResult{
		Mode:      "docker",
		Available: false,
		Reason:    "docker_only",
	}
	if got != want {
		t.Fatalf("RunProbe() = %+v, want %+v", got, want)
	}
}

func TestRunProbe_DockerBranch_EmptyContainerListFallsThroughToNone(t *testing.T) {
	lookPath := fakeLookPath(map[string]string{"docker": "/usr/bin/docker"})
	stat := fakeStat(nil)
	output := fakeOutput(map[string]fakeCmdResult{
		"docker ps --filter name=telemt --format {{.Names}}": {out: []byte("\n")},
	})

	got := RunProbe(lookPath, stat, output)

	want := ProbeResult{
		Mode:      "none",
		Available: false,
		Reason:    "no_service_manager_detected",
	}
	if got != want {
		t.Fatalf("RunProbe() = %+v, want %+v", got, want)
	}
}

func TestRunProbe_NoneBranch(t *testing.T) {
	lookPath := fakeLookPath(nil)
	stat := fakeStat(nil)
	output := fakeOutput(nil)

	got := RunProbe(lookPath, stat, output)

	want := ProbeResult{
		Mode:      "none",
		Available: false,
		Reason:    "no_service_manager_detected",
	}
	if got != want {
		t.Fatalf("RunProbe() = %+v, want %+v", got, want)
	}
}

func TestParseExecStart(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "brief example",
			in:   "ExecStart=/usr/local/bin/telemt --config /etc/telemt/config.toml",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "multi-line systemctl cat dump",
			in:   "[Unit]\nDescription=telemt\n[Service]\nExecStart=/opt/telemt/telemt --config /etc/telemt/config.toml\nRestart=on-failure\n",
			want: "/opt/telemt/telemt",
		},
		{
			name: "no ExecStart line",
			in:   "[Service]\nType=simple\n",
			want: "",
		},
		{
			name: "does not match ExecStartPre",
			in:   "ExecStartPre=/bin/true\n",
			want: "",
		},
		{
			name: "no arguments after path",
			in:   "ExecStart=/usr/local/bin/telemt\n",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "dash prefix (ignore exit status)",
			in:   "ExecStart=-/usr/local/bin/telemt --config /etc/telemt/config.toml\n",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "at prefix (argv0 override)",
			in:   "ExecStart=@/usr/local/bin/telemt\n",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "colon prefix (no variable substitution)",
			in:   "ExecStart=:/usr/local/bin/telemt\n",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "plus prefix (full privileges)",
			in:   "ExecStart=+/usr/local/bin/telemt\n",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "bang prefix (no NNP/private/etc)",
			in:   "ExecStart=!/usr/local/bin/telemt\n",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "double bang prefix (fully unprivileged)",
			in:   "ExecStart=!!/usr/local/bin/telemt\n",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "combined prefixes",
			in:   "ExecStart=-@:/usr/local/bin/telemt --config /etc/telemt/config.toml\n",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "quoted path, no prefix",
			in:   `ExecStart="/usr/local/bin/telemt" --config /etc/telemt/config.toml` + "\n",
			want: "/usr/local/bin/telemt",
		},
		{
			name: "prefix then quoted path",
			in:   `ExecStart=-"/opt/telemt/telemt"` + "\n",
			want: "/opt/telemt/telemt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseExecStart([]byte(tc.in))
			if got != tc.want {
				t.Fatalf("parseExecStart(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
