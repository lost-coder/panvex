package telemt

import (
	"context"
	"runtime"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

func collectLocalSystemLoad(ctx context.Context) (RuntimeSystemLoad, error) {
	systemLoad := RuntimeSystemLoad{}

	if cpuUsage, err := cpu.PercentWithContext(ctx, 0, false); err == nil && len(cpuUsage) > 0 {
		systemLoad.CPUUsagePct = cpuUsage[0]
	}

	if memory, err := mem.VirtualMemoryWithContext(ctx); err == nil && memory != nil {
		systemLoad.MemoryUsedBytes = memory.Used
		systemLoad.MemoryTotalBytes = memory.Total
		systemLoad.MemoryUsagePct = memory.UsedPercent
	}

	diskPath := "/"
	if runtime.GOOS == "windows" {
		diskPath = `C:\`
	}
	if usage, err := disk.UsageWithContext(ctx, diskPath); err == nil && usage != nil {
		systemLoad.DiskUsedBytes = usage.Used
		systemLoad.DiskTotalBytes = usage.Total
		systemLoad.DiskUsagePct = usage.UsedPercent
	}

	if averages, err := load.AvgWithContext(ctx); err == nil && averages != nil {
		systemLoad.Load1M = averages.Load1
		systemLoad.Load5M = averages.Load5
		systemLoad.Load15M = averages.Load15
	}

	if counters, err := net.IOCountersWithContext(ctx, false); err == nil && len(counters) > 0 {
		systemLoad.NetBytesSent = counters[0].BytesSent
		systemLoad.NetBytesRecv = counters[0].BytesRecv
	}

	return systemLoad, nil
}
