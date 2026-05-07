package updater

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
)

// TestExecute_RefusesDowngrade verifies the agent refuses a self-update
// payload whose version is older than the running binary, unless the
// payload explicitly opts in via AllowDowngrade. This is the
// counter-measure to a compromised panel pinning agents back to a
// vulnerable past release.
func TestExecute_RefusesDowngrade(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		payload        Payload
		wantErrSubstr  string
	}{
		{
			name:           "older patch refused",
			currentVersion: "1.4.7",
			payload: Payload{
				Version:      "1.4.6",
				DownloadURL:  "https://example.com/panvex-agent.tar.gz",
				SignatureURL: "https://example.com/panvex-agent.tar.gz.sig",
			},
			wantErrSubstr: "refusing downgrade",
		},
		{
			name:           "older minor refused",
			currentVersion: "2.1.0",
			payload: Payload{
				Version:      "2.0.99",
				DownloadURL:  "https://example.com/panvex-agent.tar.gz",
				SignatureURL: "https://example.com/panvex-agent.tar.gz.sig",
			},
			wantErrSubstr: "refusing downgrade",
		},
		{
			name:           "older major refused",
			currentVersion: "3.0.0",
			payload: Payload{
				Version:      "2.99.99",
				DownloadURL:  "https://example.com/panvex-agent.tar.gz",
				SignatureURL: "https://example.com/panvex-agent.tar.gz.sig",
			},
			wantErrSubstr: "refusing downgrade",
		},
		{
			name:           "with allow_downgrade falls through to download",
			currentVersion: "1.4.7",
			payload: Payload{
				Version:        "1.4.6",
				DownloadURL:    "https://allowed.invalid/x.tar.gz",
				SignatureURL:   "https://allowed.invalid/x.tar.gz.sig",
				AllowDowngrade: true,
			},
			// Past the gate, the test allowlist will reject the
			// host and produce a download error — proving the
			// downgrade check did not short-circuit.
			wantErrSubstr: "download",
		},
		{
			name:           "dev build refuses any update without explicit override",
			currentVersion: "dev",
			payload: Payload{
				Version:      "1.0.0",
				DownloadURL:  "https://allowed.invalid/x.tar.gz",
				SignatureURL: "https://allowed.invalid/x.tar.gz.sig",
			},
			wantErrSubstr: "running version",
		},
		{
			name:           "dev build allows update with explicit AllowDowngrade",
			currentVersion: "dev",
			payload: Payload{
				Version:        "1.0.0",
				DownloadURL:    "https://allowed.invalid/x.tar.gz",
				SignatureURL:   "https://allowed.invalid/x.tar.gz.sig",
				AllowDowngrade: true,
			},
			wantErrSubstr: "download",
		},
		{
			name:           "missing payload version refused",
			currentVersion: "5.0.0",
			payload: Payload{
				Version:      "",
				DownloadURL:  "https://allowed.invalid/x.tar.gz",
				SignatureURL: "https://allowed.invalid/x.tar.gz.sig",
			},
			wantErrSubstr: "payload version",
		},
		{
			name:           "pre-release does not silently equal release",
			currentVersion: "1.4.7",
			payload: Payload{
				Version:      "1.4.7-rc1",
				DownloadURL:  "https://allowed.invalid/x.tar.gz",
				SignatureURL: "https://allowed.invalid/x.tar.gz.sig",
			},
			wantErrSubstr: "refusing downgrade",
		},
		{
			name:           "non-numeric segment refused (could parse-trick the gate)",
			currentVersion: "1.4.7",
			payload: Payload{
				Version:      "1.4.7-malicious",
				DownloadURL:  "https://allowed.invalid/x.tar.gz",
				SignatureURL: "https://allowed.invalid/x.tar.gz.sig",
			},
			wantErrSubstr: "refusing downgrade",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Lock the download path to a host that doesn't exist
			// so the test never hits the network. The downgrade
			// gate fires before the download — pass-through cases
			// will still error on the unreachable host, which is
			// what we assert in those branches.
			cfg := defaultConfig()
			cfg.AllowedHosts = []string{"never-resolves.invalid"}

			err := executeWith(
				context.Background(),
				tt.payload,
				tt.currentVersion,
				slog.New(slog.NewTextHandler(io.Discard, nil)),
				cfg,
			)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErrSubstr)
			}
			if !strings.Contains(err.Error(), tt.wantErrSubstr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErrSubstr)
			}
		})
	}
}

// TestExecute_AllowsUpgrade exercises the happy comparison path: a newer
// payload version must clear the gate. As above, we don't actually let
// the test reach the network; we just assert it doesn't bail out with
// "refusing downgrade".
func TestExecute_AllowsUpgrade(t *testing.T) {
	cfg := defaultConfig()
	cfg.AllowedHosts = []string{"never-resolves.invalid"}

	err := executeWith(
		context.Background(),
		Payload{
			Version:      "1.5.0",
			DownloadURL:  "https://allowed.invalid/x.tar.gz",
			SignatureURL: "https://allowed.invalid/x.tar.gz.sig",
		},
		"1.4.7",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg,
	)
	if err == nil {
		t.Fatal("expected a download error past the version gate, got nil")
	}
	if strings.Contains(err.Error(), "refusing downgrade") {
		t.Fatalf("upgrade unexpectedly classified as downgrade: %v", err)
	}
}
