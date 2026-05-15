package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/runtime"
	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// failingTelemt drives runtime.Agent so every FetchRuntimeState returns err.
type failingTelemt struct{}

func (failingTelemt) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return telemt.RuntimeState{}, errors.New("telemt unreachable")
}
func (failingTelemt) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	return telemt.ClientUsageMetricsSnapshot{}, nil
}
func (failingTelemt) FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error) {
	return nil, nil
}
func (failingTelemt) FetchSystemInfo(context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, nil
}
func (failingTelemt) FetchDiscoveredUsers(context.Context, string) ([]telemt.DiscoveredUser, error) {
	return nil, nil
}
func (failingTelemt) ExecuteRuntimeReload(context.Context) error { return nil }
func (failingTelemt) CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}
func (failingTelemt) UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}
func (failingTelemt) DeleteClient(context.Context, string) error { return nil }
func (failingTelemt) InvalidateSlowDataCache()                   {}
func (failingTelemt) ResetUserQuota(context.Context, string) (telemt.ResetUserQuotaResult, error) {
	return telemt.ResetUserQuotaResult{}, nil
}

func TestRuntimePollEmitsUnreachableAfterThreshold(t *testing.T) {
	agent := runtime.New(runtime.Config{AgentID: "agent-1", NodeName: "n"}, failingTelemt{})
	buffer := runtime.NewRuntimeRingBuffer(8)
	out := make(chan *gatewayrpc.ConnectClientMessage, 8)

	tracker := &telemtReachabilityTracker{}
	consecutiveFailures := 0
	start := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)

	// Poll #1 at t=0 — first failure, no emit.
	performRuntimePoll(context.Background(), agent, buffer, out, start, &consecutiveFailures, tracker)
	if got := len(out); got != 0 {
		t.Fatalf("emit at t=0 = %d snapshots, want 0 (within grace)", got)
	}
	if !tracker.firstFailureAt.Equal(start) {
		t.Fatalf("firstFailureAt = %v, want %v", tracker.firstFailureAt, start)
	}

	// Poll #2 at t=29s — still inside grace.
	performRuntimePoll(context.Background(), agent, buffer, out, start.Add(29*time.Second),
		&consecutiveFailures, tracker)
	if got := len(out); got != 0 {
		t.Fatalf("emit at t=29s = %d snapshots, want 0 (within grace)", got)
	}

	// Poll #3 at t=30s — threshold reached, emit unreachable snapshot.
	performRuntimePoll(context.Background(), agent, buffer, out, start.Add(30*time.Second),
		&consecutiveFailures, tracker)
	if got := len(out); got != 1 {
		t.Fatalf("emit at t=30s = %d snapshots, want 1", got)
	}
	msg := <-out
	snap := msg.GetSnapshot()
	if snap == nil || snap.Runtime == nil {
		t.Fatal("emitted message missing runtime snapshot")
	}
	if !snap.Runtime.TelemtUnreachable {
		t.Fatal("emitted runtime.TelemtUnreachable = false, want true")
	}
	if snap.Runtime.TelemtUnreachableSinceUnix != start.Unix() {
		t.Fatalf("emitted since = %d, want %d", snap.Runtime.TelemtUnreachableSinceUnix, start.Unix())
	}

	// Poll #4 at t=60s — every subsequent failure should keep emitting.
	performRuntimePoll(context.Background(), agent, buffer, out, start.Add(60*time.Second),
		&consecutiveFailures, tracker)
	if got := len(out); got != 1 {
		t.Fatalf("re-emit at t=60s = %d snapshots in channel, want 1 (drained between calls)", got)
	}
}

func TestRuntimePollClearsTrackerOnRecovery(t *testing.T) {
	calls := 0
	stub := &recoveringTelemt{onCall: func() (telemt.RuntimeState, error) {
		calls++
		if calls <= 2 {
			return telemt.RuntimeState{}, errors.New("down")
		}
		return telemt.RuntimeState{
			Gates: telemt.RuntimeGates{
				UseMiddleProxy:          true,
				MERuntimeReady:          true,
				AcceptingNewConnections: true,
			},
		}, nil
	}}
	agent := runtime.New(runtime.Config{AgentID: "agent-1"}, stub)
	buffer := runtime.NewRuntimeRingBuffer(8)
	out := make(chan *gatewayrpc.ConnectClientMessage, 8)
	tracker := &telemtReachabilityTracker{}
	consecutiveFailures := 0
	start := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)

	// Two failures inside grace.
	performRuntimePoll(context.Background(), agent, buffer, out, start, &consecutiveFailures, tracker)
	performRuntimePoll(context.Background(), agent, buffer, out, start.Add(10*time.Second), &consecutiveFailures, tracker)
	if tracker.firstFailureAt.IsZero() {
		t.Fatal("firstFailureAt should be set after two failures")
	}

	// Successful poll clears tracker.
	performRuntimePoll(context.Background(), agent, buffer, out, start.Add(20*time.Second), &consecutiveFailures, tracker)
	if !tracker.firstFailureAt.IsZero() {
		t.Fatalf("firstFailureAt = %v, want zero after successful poll", tracker.firstFailureAt)
	}
	if consecutiveFailures != 0 {
		t.Fatalf("consecutiveFailures = %d, want 0 after success", consecutiveFailures)
	}
}

// recoveringTelemt lets the test inject FetchRuntimeState behaviour.
type recoveringTelemt struct {
	onCall func() (telemt.RuntimeState, error)
}

func (r *recoveringTelemt) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return r.onCall()
}
func (r *recoveringTelemt) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	return telemt.ClientUsageMetricsSnapshot{}, nil
}
func (r *recoveringTelemt) FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error) {
	return nil, nil
}
func (r *recoveringTelemt) FetchSystemInfo(context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, nil
}
func (r *recoveringTelemt) FetchDiscoveredUsers(context.Context, string) ([]telemt.DiscoveredUser, error) {
	return nil, nil
}
func (r *recoveringTelemt) ExecuteRuntimeReload(context.Context) error { return nil }
func (r *recoveringTelemt) CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}
func (r *recoveringTelemt) UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}
func (r *recoveringTelemt) DeleteClient(context.Context, string) error { return nil }
func (r *recoveringTelemt) InvalidateSlowDataCache()                   {}
func (r *recoveringTelemt) ResetUserQuota(context.Context, string) (telemt.ResetUserQuotaResult, error) {
	return telemt.ResetUserQuotaResult{}, nil
}
