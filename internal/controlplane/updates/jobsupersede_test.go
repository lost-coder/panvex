package updates

import (
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/jobs"
)

func TestJobSupersedeKey(t *testing.T) {
	tests := []struct {
		name        string
		action      jobs.Action
		payloadJSON string
		want        string
	}{
		{
			name:        "telemt.update",
			action:      jobs.ActionTelemtUpdate,
			payloadJSON: `{"version":"3.4.25","release_base_url":"https://x","restart_spec":"systemd:telemt","binary_path":"/usr/local/bin/telemt"}`,
			want:        "telemt.update",
		},
		{
			name:        "telemt.update key ignores payload content",
			action:      jobs.ActionTelemtUpdate,
			payloadJSON: `{"version":"1.0.0"}`,
			want:        "telemt.update",
		},
		{
			name:        "unrelated action",
			action:      jobs.ActionAgentSelfUpdate,
			payloadJSON: `{"version":"1.4.0"}`,
			want:        "",
		},
		{
			name:        "empty payload",
			action:      jobs.ActionTelemtUpdate,
			payloadJSON: "",
			want:        "telemt.update",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := JobSupersedeKey(tc.action, tc.payloadJSON); got != tc.want {
				t.Fatalf("JobSupersedeKey(%s, %q) = %q, want %q", tc.action, tc.payloadJSON, got, tc.want)
			}
		})
	}
}
