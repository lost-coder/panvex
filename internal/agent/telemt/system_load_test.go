package telemt

import (
	"context"
	"errors"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

func okProbes() systemLoadProbes {
	return systemLoadProbes{
		cpuPercent: func(context.Context) ([]float64, error) { return []float64{12.5}, nil },
		virtualMemory: func(context.Context) (*mem.VirtualMemoryStat, error) {
			return &mem.VirtualMemoryStat{Used: 100, Total: 200, UsedPercent: 50}, nil
		},
		diskUsage: func(context.Context, string) (*disk.UsageStat, error) {
			return &disk.UsageStat{Used: 30, Total: 60, UsedPercent: 50}, nil
		},
		loadAvg: func(context.Context) (*load.AvgStat, error) {
			return &load.AvgStat{Load1: 1, Load5: 2, Load15: 3}, nil
		},
		netIOCounters: func(context.Context) ([]net.IOCountersStat, error) {
			return []net.IOCountersStat{{BytesSent: 1000, BytesRecv: 2000}}, nil
		},
	}
}

func TestCollectSystemLoadAllProbesSucceed(t *testing.T) {
	got, err := collectSystemLoad(context.Background(), okProbes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Partial {
		t.Errorf("Partial = true, want false when all probes succeed")
	}
	if got.CPUUsagePct != 12.5 || got.MemoryUsedBytes != 100 || got.DiskUsedBytes != 30 ||
		got.Load1M != 1 || got.NetBytesSent != 1000 {
		t.Errorf("unexpected values: %+v", got)
	}
}

func TestCollectSystemLoadReportsProbeFailures(t *testing.T) {
	probeErr := errors.New("cpu probe boom")
	probes := okProbes()
	probes.cpuPercent = func(context.Context) ([]float64, error) { return nil, probeErr }

	got, err := collectSystemLoad(context.Background(), probes)
	if err == nil {
		t.Fatal("expected error when a probe fails, got nil")
	}
	if !errors.Is(err, probeErr) {
		t.Errorf("error %v does not wrap probe error %v", err, probeErr)
	}
	if !got.Partial {
		t.Errorf("Partial = false, want true when a probe fails")
	}
	// Failed probe leaves its field at zero (do not fabricate a value)...
	if got.CPUUsagePct != 0 {
		t.Errorf("CPUUsagePct = %v, want 0 for failed probe", got.CPUUsagePct)
	}
	// ...while successful probes are still retained.
	if got.MemoryUsedBytes != 100 || got.DiskUsedBytes != 30 ||
		got.Load1M != 1 || got.NetBytesSent != 1000 {
		t.Errorf("successful probe values not retained: %+v", got)
	}
}
