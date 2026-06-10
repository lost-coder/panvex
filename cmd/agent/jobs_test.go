package main

import (
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// TestJobExecutionBudget guards A5: the blanket 30s job timeout cannot fit a
// config.apply whose own health probe is 30s plus a Telemt restart — the
// budget must be derived from the action (and its payload).
func TestJobExecutionBudget(t *testing.T) {
	cases := []struct {
		name string
		job  *gatewayrpc.JobCommand
		want time.Duration
	}{
		{
			name: "default action keeps the 30s budget",
			job:  &gatewayrpc.JobCommand{Action: "runtime.reload"},
			want: jobExecutionTimeout,
		},
		{
			name: "config.apply with default health timeout",
			job:  &gatewayrpc.JobCommand{Action: "config.apply", PayloadJson: `{"patch":{"general":{}}}`},
			want: 30*time.Second + configApplyRestartAllowance + configApplyBudgetMargin,
		},
		{
			name: "config.apply with explicit health timeout",
			job:  &gatewayrpc.JobCommand{Action: "config.apply", PayloadJson: `{"health_timeout_s":60,"patch":{}}`},
			want: 60*time.Second + configApplyRestartAllowance + configApplyBudgetMargin,
		},
		{
			name: "config.apply with malformed payload falls back to default health",
			job:  &gatewayrpc.JobCommand{Action: "config.apply", PayloadJson: `not-json`},
			want: 30*time.Second + configApplyRestartAllowance + configApplyBudgetMargin,
		},
		{
			name: "self-update gets the download budget",
			job:  &gatewayrpc.JobCommand{Action: "agent.self-update"},
			want: selfUpdateExecutionTimeout,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := jobExecutionBudget(tc.job); got != tc.want {
				t.Fatalf("jobExecutionBudget = %v, want %v", got, tc.want)
			}
		})
	}
}
