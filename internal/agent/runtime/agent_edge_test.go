package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// errTelemt is a stub telemtClient that returns a configured error from
// every fetch path so callers can drive error-handling assertions
// without polluting the broader fakeTelemtClient (S27 T3).
type errTelemt struct {
	fetchRuntimeStateErr error
	fetchUsageErr        error
	fetchActiveIPsErr    error
	fetchSystemInfoErr   error
	fetchDiscoveredErr   error
	executeReloadErr     error
	createErr            error
	updateErr            error
	deleteErr            error
	createCalls          atomic.Int64
	updateCalls          atomic.Int64
	deleteCalls          atomic.Int64
}

func (e *errTelemt) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	return telemt.RuntimeState{}, e.fetchRuntimeStateErr
}
func (e *errTelemt) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	return telemt.ClientUsageMetricsSnapshot{}, e.fetchUsageErr
}
func (e *errTelemt) FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error) {
	return nil, e.fetchActiveIPsErr
}
func (e *errTelemt) FetchSystemInfo(context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, e.fetchSystemInfoErr
}
func (e *errTelemt) FetchDiscoveredUsers(context.Context, string) ([]telemt.DiscoveredUser, error) {
	return nil, e.fetchDiscoveredErr
}
func (e *errTelemt) ExecuteRuntimeReload(context.Context) error { return e.executeReloadErr }
func (e *errTelemt) CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	e.createCalls.Add(1)
	return telemt.ClientApplyResult{}, e.createErr
}
func (e *errTelemt) UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	e.updateCalls.Add(1)
	return telemt.ClientApplyResult{}, e.updateErr
}
func (e *errTelemt) DeleteClient(context.Context, string) error {
	e.deleteCalls.Add(1)
	return e.deleteErr
}
func (e *errTelemt) InvalidateSlowDataCache() {}
func (e *errTelemt) ResetUserQuota(context.Context, string) (telemt.ResetUserQuotaResult, error) {
	return telemt.ResetUserQuotaResult{}, nil
}
func (e *errTelemt) PatchConfig(context.Context, map[string]any, string) (telemt.PatchConfigResult, error) {
	return telemt.PatchConfigResult{}, nil
}
func (e *errTelemt) GetManagedConfig(context.Context) (map[string]any, string, error) {
	return nil, "", nil
}
func (e *errTelemt) HealthReady(context.Context) (bool, string, error) {
	return true, "", nil
}

// TestBuildUsageSnapshotPropagatesTelemtError verifies that when Telemt
// is unreachable the usage path returns the error WITHOUT advancing the
// cumulative totals (P4). A regression that folded a partial/error read
// into usageTotals would corrupt the panel's watermark accounting.
func TestBuildUsageSnapshotPropagatesTelemtError(t *testing.T) {
	stub := &errTelemt{fetchUsageErr: errors.New("telemt unreachable")}

	agent := New(Config{
		AgentID:  "agent-1",
		NodeName: "node-a",
		Version:  "1.0.0",
	}, stub)

	totalBefore := agent.UsageTotalForTest("client-1")
	snap, err := agent.BuildUsageSnapshot(context.Background(), time.Now())
	if err == nil {
		t.Fatal("BuildUsageSnapshot err = nil, want telemt error")
	}
	if snap != nil {
		t.Fatalf("BuildUsageSnapshot snap = %v, want nil on error", snap)
	}
	if got := agent.UsageTotalForTest("client-1"); got != totalBefore {
		t.Fatalf("usageTotals = %d, want unchanged %d (must NOT advance on telemt error)", got, totalBefore)
	}
}

// TestPollActiveIPsPropagatesTelemtError verifies the IP collector poll
// path returns the underlying telemt error without panicking (S27 T3).
func TestPollActiveIPsPropagatesTelemtError(t *testing.T) {
	want := errors.New("ipfetch boom")
	stub := &errTelemt{fetchActiveIPsErr: want}
	agent := New(Config{AgentID: "agent-1"}, stub)

	if err := agent.PollActiveIPs(context.Background()); !errors.Is(err, want) {
		t.Fatalf("PollActiveIPs err = %v, want %v", err, want)
	}

	// A subsequent BuildIPSnapshot must still produce a valid (empty)
	// snapshot — the agent must not be wedged by the prior error.
	snap := agent.BuildIPSnapshot(time.Now())
	if snap == nil {
		t.Fatal("BuildIPSnapshot = nil after PollActiveIPs error")
	}
	if !snap.HasClientIps {
		t.Fatal("HasClientIps = false, want true (empty-but-present)")
	}
	if len(snap.ClientIps) != 0 {
		t.Fatalf("len(ClientIps) = %d, want 0", len(snap.ClientIps))
	}
}

// TestHandleJobUnsupportedAction verifies the default branch in HandleJob:
// an unknown action must return Success=false with a descriptive message
// rather than crash or silently succeed (S27 T3).
func TestHandleJobUnsupportedAction(t *testing.T) {
	stub := &errTelemt{}
	agent := New(Config{AgentID: "agent-1"}, stub)

	res := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:     "job-unknown",
		Action: "nonsense.action",
	}, time.Now())

	if res.Success {
		t.Fatalf("HandleJob Success = true, want false (unsupported action)")
	}
	if res.Message == "" {
		t.Fatal("HandleJob Message empty, want descriptive error")
	}
}

// TestHandleClientJobInvalidPayload covers malformed JSON in client.create:
// the unmarshal failure must be reported, and Telemt's Create/Update/Delete
// must NOT be invoked (S27 T3).
func TestHandleClientJobInvalidPayload(t *testing.T) {
	stub := &errTelemt{}
	agent := New(Config{AgentID: "agent-1"}, stub)

	res := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:          "job-bad-payload",
		Action:      "client.create",
		PayloadJson: "{bogus}",
	}, time.Now())

	if res.Success {
		t.Fatalf("HandleJob Success = true, want false on malformed payload")
	}
	if stub.createCalls.Load()+stub.updateCalls.Load()+stub.deleteCalls.Load() != 0 {
		t.Fatalf("telemt calls = %d/%d/%d, want zero (payload must reject before telemt call)",
			stub.createCalls.Load(), stub.updateCalls.Load(), stub.deleteCalls.Load())
	}
}

// TestHandleSwitchTransportModeRejectsBadInput is table-driven over the
// two failure modes inside handleSwitchTransportModeJob: malformed JSON
// and an unknown mode string. Both must yield Success=false WITHOUT
// invoking the UpdateTransport callback (S27 T3).
func TestHandleSwitchTransportModeRejectsBadInput(t *testing.T) {
	cases := []struct {
		name    string
		payload string
	}{
		{"malformed-json", "{not-json"},
		{"unknown-mode", `{"mode":"sideways"}`},
		{"empty-mode", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			updateCalls := 0
			stub := &errTelemt{}
			agent := New(Config{
				AgentID: "agent-1",
				UpdateTransport: func(string, string, string) error {
					updateCalls++
					return nil
				},
			}, stub)
			res := agent.HandleJob(context.Background(), &gatewayrpc.JobCommand{
				Id:          "job-switch-bad",
				Action:      "switch_transport_mode",
				PayloadJson: tc.payload,
			}, time.Now())
			if res.Success {
				t.Fatalf("HandleJob Success = true, want false (%s)", tc.name)
			}
			if updateCalls != 0 {
				t.Fatalf("UpdateTransport calls = %d, want 0 (bad input must not reach callback)", updateCalls)
			}
		})
	}
}

// TestConcurrentBuildUsageSnapshotTotalsMonotonic fires N goroutines into
// BuildUsageSnapshot concurrently. The cumulative total observed via the
// test export must never decrease across calls (under -race) — this
// catches any unsynchronised access to a.usageTotals (P4).
func TestConcurrentBuildUsageSnapshotTotalsMonotonic(t *testing.T) {
	clientStub := &fakeTelemtClient{
		metricsUsage: []telemt.ClientUsage{
			{ClientID: "client-1", TrafficUsedBytes: 1, ActiveTCPConns: 1},
		},
	}
	agent := New(Config{AgentID: "agent-1"}, clientStub)

	const workers = 16
	var wg sync.WaitGroup
	totals := make(chan uint64, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			snap, err := agent.BuildUsageSnapshot(context.Background(), time.Now())
			if err != nil {
				t.Errorf("BuildUsageSnapshot[%d]: %v", i, err)
				return
			}
			if snap == nil {
				t.Errorf("BuildUsageSnapshot[%d]: nil snapshot", i)
				return
			}
			totals <- agent.UsageTotalForTest("client-1")
		}(i)
	}
	wg.Wait()
	close(totals)

	// The counter never advances past the baseline in this stub (traffic
	// constant), so every observed total must be exactly 0 — the real
	// assertion is the -race pass over concurrent map access.
	for total := range totals {
		if total != 0 {
			t.Fatalf("cumulative total = %d, want 0 (constant telemt counter)", total)
		}
	}
}

// TestBuildIPSnapshotEmptyWhenNothingPolled verifies that calling
// BuildIPSnapshot before any successful PollActiveIPs returns a
// non-nil snapshot with an empty ClientIps slice — guards against a
// nil-panic regression in baseSnapshot / Flush (S27 T3).
func TestBuildIPSnapshotEmptyWhenNothingPolled(t *testing.T) {
	agent := New(Config{
		AgentID:      "agent-1",
		NodeName:     "node-a",
		FleetGroupID: "fg-1",
		Version:      "1.0.0",
	}, &errTelemt{})

	snap := agent.BuildIPSnapshot(time.Date(2026, time.April, 18, 10, 0, 0, 0, time.UTC))
	if snap == nil {
		t.Fatal("BuildIPSnapshot = nil")
	}
	if !snap.HasClientIps {
		t.Fatal("HasClientIps = false, want true (empty-but-present)")
	}
	if len(snap.ClientIps) != 0 {
		t.Fatalf("len(ClientIps) = %d, want 0 on fresh agent", len(snap.ClientIps))
	}
	if snap.AgentId != "agent-1" || snap.NodeName != "node-a" {
		t.Fatalf("base snapshot identity dropped: %+v", snap)
	}
}

// TestHandleClientDataRequestSwallowsTelemtError verifies the
// HandleClientDataRequest contract: a Telemt failure returns an empty
// envelope tagged with the same RequestId rather than nil — the panel
// uses RequestId to correlate and would otherwise treat a nil response
// as a transport failure (S27 T3).
func TestHandleClientDataRequestSwallowsTelemtError(t *testing.T) {
	stub := &errTelemt{fetchDiscoveredErr: errors.New("telemt down")}
	agent := New(Config{
		AgentID:          "agent-1",
		TelemtConfigPath: "/etc/telemt/config.toml",
	}, stub)

	resp := agent.HandleClientDataRequest(context.Background(), "req-42")
	if resp == nil {
		t.Fatal("HandleClientDataRequest = nil, want envelope")
	}
	if resp.RequestId != "req-42" {
		t.Fatalf("RequestId = %q, want %q", resp.RequestId, "req-42")
	}
	if len(resp.Clients) != 0 {
		t.Fatalf("len(Clients) = %d, want 0 on telemt error", len(resp.Clients))
	}
}
