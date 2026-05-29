package telemt

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

// systemLoadProbes is the set of host probes used to sample local system load.
// It is injected so failures can be exercised in tests; production wiring uses
// defaultSystemLoadProbes.
type systemLoadProbes struct {
	cpuPercent    func(ctx context.Context) ([]float64, error)
	virtualMemory func(ctx context.Context) (*mem.VirtualMemoryStat, error)
	diskUsage     func(ctx context.Context, path string) (*disk.UsageStat, error)
	loadAvg       func(ctx context.Context) (*load.AvgStat, error)
	netIOCounters func(ctx context.Context) ([]net.IOCountersStat, error)
}

func defaultSystemLoadProbes() systemLoadProbes {
	return systemLoadProbes{
		cpuPercent: func(ctx context.Context) ([]float64, error) {
			return cpu.PercentWithContext(ctx, 0, false)
		},
		virtualMemory: mem.VirtualMemoryWithContext,
		diskUsage:     disk.UsageWithContext,
		loadAvg:       load.AvgWithContext,
		netIOCounters: func(ctx context.Context) ([]net.IOCountersStat, error) {
			return net.IOCountersWithContext(ctx, false)
		},
	}
}

func collectLocalSystemLoad(ctx context.Context) (RuntimeSystemLoad, error) {
	return collectSystemLoad(ctx, defaultSystemLoadProbes())
}

// collectSystemLoad samples each probe independently. A probe failure no longer
// masquerades as a zero reading: the failed field is left at zero, the snapshot
// is flagged Partial, and the joined probe errors are returned so callers can
// log/alert and mark the parent snapshot incomplete. See IN-L3.
func collectSystemLoad(ctx context.Context, probes systemLoadProbes) (RuntimeSystemLoad, error) {
	systemLoad := RuntimeSystemLoad{}
	var errs []error

	if cpuUsage, err := probes.cpuPercent(ctx); err != nil {
		errs = append(errs, fmt.Errorf("cpu: %w", err))
	} else if len(cpuUsage) > 0 {
		systemLoad.CPUUsagePct = cpuUsage[0]
	}

	if memory, err := probes.virtualMemory(ctx); err != nil {
		errs = append(errs, fmt.Errorf("memory: %w", err))
	} else if memory != nil {
		systemLoad.MemoryUsedBytes = memory.Used
		systemLoad.MemoryTotalBytes = memory.Total
		systemLoad.MemoryUsagePct = memory.UsedPercent
	}

	diskPath := "/"
	if runtime.GOOS == "windows" {
		diskPath = `C:\`
	}
	if usage, err := probes.diskUsage(ctx, diskPath); err != nil {
		errs = append(errs, fmt.Errorf("disk: %w", err))
	} else if usage != nil {
		systemLoad.DiskUsedBytes = usage.Used
		systemLoad.DiskTotalBytes = usage.Total
		systemLoad.DiskUsagePct = usage.UsedPercent
	}

	if averages, err := probes.loadAvg(ctx); err != nil {
		errs = append(errs, fmt.Errorf("load: %w", err))
	} else if averages != nil {
		systemLoad.Load1M = averages.Load1
		systemLoad.Load5M = averages.Load5
		systemLoad.Load15M = averages.Load15
	}

	if counters, err := probes.netIOCounters(ctx); err != nil {
		errs = append(errs, fmt.Errorf("net: %w", err))
	} else if len(counters) > 0 {
		systemLoad.NetBytesSent = counters[0].BytesSent
		systemLoad.NetBytesRecv = counters[0].BytesRecv
	}

	if len(errs) > 0 {
		systemLoad.Partial = true
		return systemLoad, errors.Join(errs...)
	}
	return systemLoad, nil
}
