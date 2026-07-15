package jobs

import (
	"context"
	"errors"

	"github.com/lost-coder/panvex/internal/agent/telemt"
)

// failingTelemt drives runtime.Agent so every FetchRuntimeState returns err.
// Copy of the fake that lives beside the polling workers (internal/agent/conn):
// the job runner needs the same "Telemt is down" driver, and a per-package test
// fixture is cheaper than exporting one from production code.
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
func (failingTelemt) PatchConfig(context.Context, map[string]any, string) (telemt.PatchConfigResult, error) {
	return telemt.PatchConfigResult{}, nil
}
func (failingTelemt) GetManagedConfig(context.Context) (map[string]any, string, error) {
	return nil, "", nil
}
func (failingTelemt) HealthReady(context.Context) (bool, string, error) {
	return true, "", nil
}

type fakeDiagnosticsRefreshTelemtClient struct {
	state                     telemt.RuntimeState
	invalidateSlowDataCalls   int
	fetchErrAfterInvalidation bool
}

func (c *fakeDiagnosticsRefreshTelemtClient) FetchRuntimeState(context.Context) (telemt.RuntimeState, error) {
	if c.fetchErrAfterInvalidation && c.invalidateSlowDataCalls > 0 {
		return telemt.RuntimeState{}, context.DeadlineExceeded
	}
	return c.state, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) FetchClientUsageFromMetrics(context.Context) (telemt.ClientUsageMetricsSnapshot, error) {
	return telemt.ClientUsageMetricsSnapshot{}, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) FetchActiveIPs(context.Context) ([]telemt.UserActiveIPs, error) {
	return nil, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) CreateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) UpdateClient(context.Context, telemt.ManagedClient) (telemt.ClientApplyResult, error) {
	return telemt.ClientApplyResult{}, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) DeleteClient(context.Context, string) error {
	return nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) FetchSystemInfo(context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) FetchDiscoveredUsers(context.Context, string) ([]telemt.DiscoveredUser, error) {
	return nil, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) InvalidateSlowDataCache() {
	c.invalidateSlowDataCalls++
}

func (c *fakeDiagnosticsRefreshTelemtClient) ResetUserQuota(context.Context, string) (telemt.ResetUserQuotaResult, error) {
	return telemt.ResetUserQuotaResult{}, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) PatchConfig(context.Context, map[string]any, string) (telemt.PatchConfigResult, error) {
	return telemt.PatchConfigResult{}, nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) GetManagedConfig(context.Context) (map[string]any, string, error) {
	return nil, "", nil
}

func (c *fakeDiagnosticsRefreshTelemtClient) HealthReady(context.Context) (bool, string, error) {
	return true, "", nil
}
