package jobs

import (
	"reflect"
	"testing"
)

func TestDeriveJobStatusTable(t *testing.T) {
	cases := []struct {
		name    string
		targets []TargetStatus
		want    Status
	}{
		{
			name:    "empty targets -> queued",
			targets: nil,
			want:    StatusQueued,
		},
		{
			name:    "all queued -> queued",
			targets: []TargetStatus{TargetStatusQueued, TargetStatusQueued},
			want:    StatusQueued,
		},
		{
			name:    "all succeeded -> succeeded",
			targets: []TargetStatus{TargetStatusSucceeded, TargetStatusSucceeded},
			want:    StatusSucceeded,
		},
		{
			name:    "all failed -> failed",
			targets: []TargetStatus{TargetStatusFailed, TargetStatusFailed},
			want:    StatusFailed,
		},
		{
			name:    "all expired -> expired",
			targets: []TargetStatus{TargetStatusExpired, TargetStatusExpired},
			want:    StatusExpired,
		},
		{
			// F2: a single failure no longer masks sibling successes.
			name:    "succeeded and failed -> partial",
			targets: []TargetStatus{TargetStatusSucceeded, TargetStatusFailed},
			want:    StatusPartial,
		},
		{
			// F2: this used to resolve to failed, hiding the success.
			name:    "succeeded, failed, succeeded -> partial",
			targets: []TargetStatus{TargetStatusSucceeded, TargetStatusFailed, TargetStatusSucceeded},
			want:    StatusPartial,
		},
		{
			// F2: this used to resolve to expired, hiding the success.
			name:    "succeeded and expired -> partial",
			targets: []TargetStatus{TargetStatusSucceeded, TargetStatusExpired},
			want:    StatusPartial,
		},
		{
			// F2: mix of success with both terminal-unsuccessful kinds.
			name:    "succeeded, failed and expired -> partial",
			targets: []TargetStatus{TargetStatusSucceeded, TargetStatusFailed, TargetStatusExpired},
			want:    StatusPartial,
		},
		{
			name:    "failed and expired (no success) -> failed",
			targets: []TargetStatus{TargetStatusFailed, TargetStatusExpired},
			want:    StatusFailed,
		},
		{
			// Progress in flight outranks a mixed terminal outcome: the job is
			// not terminal yet, so it is still running rather than partial.
			name:    "succeeded, failed and sent -> running",
			targets: []TargetStatus{TargetStatusSucceeded, TargetStatusFailed, TargetStatusSent},
			want:    StatusRunning,
		},
		{
			name:    "succeeded and sent -> running",
			targets: []TargetStatus{TargetStatusSucceeded, TargetStatusSent},
			want:    StatusRunning,
		},
		{
			name:    "expired and sent -> running",
			targets: []TargetStatus{TargetStatusExpired, TargetStatusSent},
			want:    StatusRunning,
		},
		{
			name:    "expired and acknowledged -> running",
			targets: []TargetStatus{TargetStatusExpired, TargetStatusAcknowledged},
			want:    StatusRunning,
		},
		{
			// Some targets finished while others are still queued: not terminal.
			name:    "succeeded and queued -> running",
			targets: []TargetStatus{TargetStatusSucceeded, TargetStatusQueued},
			want:    StatusRunning,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			targets := make([]JobTarget, 0, len(tc.targets))
			for _, st := range tc.targets {
				targets = append(targets, JobTarget{Status: st})
			}
			got := deriveJobStatus(targets)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("deriveJobStatus(%v) = %v; want %v", tc.targets, got, tc.want)
			}
		})
	}
}
