package updater

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
				Version:        "1.4.6",
				ReleaseBaseURL: "https://example.com/agent/v1.4.6",
			},
			wantErrSubstr: "refusing downgrade",
		},
		{
			name:           "older minor refused",
			currentVersion: "2.1.0",
			payload: Payload{
				Version:        "2.0.99",
				ReleaseBaseURL: "https://example.com/agent/v2.0.99",
			},
			wantErrSubstr: "refusing downgrade",
		},
		{
			name:           "older major refused",
			currentVersion: "3.0.0",
			payload: Payload{
				Version:        "2.99.99",
				ReleaseBaseURL: "https://example.com/agent/v2.99.99",
			},
			wantErrSubstr: "refusing downgrade",
		},
		{
			name:           "with allow_downgrade falls through to download",
			currentVersion: "1.4.7",
			payload: Payload{
				Version:        "1.4.6",
				ReleaseBaseURL: "https://allowed.invalid/agent",
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
				Version:        "1.0.0",
				ReleaseBaseURL: "https://allowed.invalid/agent",
			},
			wantErrSubstr: "running version",
		},
		{
			name:           "dev build allows update with explicit AllowDowngrade",
			currentVersion: "dev",
			payload: Payload{
				Version:        "1.0.0",
				ReleaseBaseURL: "https://allowed.invalid/agent",
				AllowDowngrade: true,
			},
			wantErrSubstr: "download",
		},
		{
			name:           "missing payload version refused",
			currentVersion: "5.0.0",
			payload: Payload{
				Version:        "",
				ReleaseBaseURL: "https://allowed.invalid/agent",
			},
			wantErrSubstr: "payload version",
		},
		{
			name:           "pre-release does not silently equal release",
			currentVersion: "1.4.7",
			payload: Payload{
				Version:        "1.4.7-rc1",
				ReleaseBaseURL: "https://allowed.invalid/agent",
			},
			wantErrSubstr: "refusing downgrade",
		},
		{
			name:           "non-numeric segment refused (could parse-trick the gate)",
			currentVersion: "1.4.7",
			payload: Payload{
				Version:        "1.4.7-malicious",
				ReleaseBaseURL: "https://allowed.invalid/agent",
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

			_, err := executeWith(
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

	_, err := executeWith(
		context.Background(),
		Payload{
			Version:        "1.5.0",
			ReleaseBaseURL: "https://allowed.invalid/agent",
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

// TestExecute_NoopWhenAlreadyAtTargetVersion guards A3: a self-update to the
// version the agent already runs must short-circuit to a successful no-op —
// no download, no binary swap, no restart. Previously equality passed the
// downgrade gate, the agent reinstalled itself and restarted before the
// JobResult was sent, so the panel re-dispatched the job forever.
func TestExecute_NoopWhenAlreadyAtTargetVersion(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
	}))
	t.Cleanup(srv.Close)

	cfg := defaultConfig()
	cfg.HTTPClient = srv.Client()
	cfg.AllowedHosts = []string{hostOf(t, srv.URL)}
	cfg.AllowInsecure = true

	outcome, err := executeWith(
		context.Background(),
		Payload{Version: "1.4.7", ReleaseBaseURL: srv.URL + "/agent/v1.4.7"},
		"v1.4.7", // leading-v form must compare equal to "1.4.7"
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg,
	)
	if err != nil {
		t.Fatalf("expected no-op success, got error: %v", err)
	}
	if outcome != OutcomeNoop {
		t.Fatalf("outcome = %v, want OutcomeNoop", outcome)
	}
	if requests != 0 {
		t.Fatalf("no-op must not touch the release server, got %d requests", requests)
	}
}
