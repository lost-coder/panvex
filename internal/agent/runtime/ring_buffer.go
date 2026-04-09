package runtime

import (
	"sync"
	"time"

	"github.com/panvex/panvex/internal/gatewayrpc"
)

// RuntimeSample stores one poll result from Telemt + gopsutil.
type RuntimeSample struct {
	ObservedAt time.Time
	Snapshot   *gatewayrpc.Snapshot
}

// RuntimeRingBuffer accumulates poll samples between upload ticks.
type RuntimeRingBuffer struct {
	mu      sync.Mutex
	samples []RuntimeSample
	cap     int
}

func NewRuntimeRingBuffer(capacity int) *RuntimeRingBuffer {
	return &RuntimeRingBuffer{
		samples: make([]RuntimeSample, 0, capacity),
		cap:     capacity,
	}
}

// Push adds a sample. If at capacity, the oldest sample is dropped.
func (b *RuntimeRingBuffer) Push(sample RuntimeSample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.samples) >= b.cap {
		copy(b.samples, b.samples[1:])
		b.samples[len(b.samples)-1] = sample
	} else {
		b.samples = append(b.samples, sample)
	}
}

// DrainAndAggregate returns a single aggregated snapshot from all buffered samples,
// then clears the buffer. Returns nil if no samples exist.
func (b *RuntimeRingBuffer) DrainAndAggregate() *gatewayrpc.Snapshot {
	b.mu.Lock()
	samples := b.samples
	b.samples = make([]RuntimeSample, 0, b.cap)
	b.mu.Unlock()

	if len(samples) == 0 {
		return nil
	}

	// Use the last sample as the base (most recent state).
	last := samples[len(samples)-1]
	result := last.Snapshot

	if result.Runtime == nil {
		return result
	}

	n := len(samples)
	if n == 1 {
		result.Runtime.AggregationSamples = 1
		return result
	}

	// Aggregate system load: avg + max
	var cpuSum, cpuMax, memSum, memMax, diskSum, diskMax float64
	var load1Last, load5Last, load15Last float64
	for _, s := range samples {
		sl := s.Snapshot.GetRuntime().GetSystemLoad()
		if sl == nil {
			continue
		}
		cpuSum += sl.CpuUsagePct
		if sl.CpuUsagePct > cpuMax {
			cpuMax = sl.CpuUsagePct
		}
		memSum += sl.MemoryUsagePct
		if sl.MemoryUsagePct > memMax {
			memMax = sl.MemoryUsagePct
		}
		diskSum += sl.DiskUsagePct
		if sl.DiskUsagePct > diskMax {
			diskMax = sl.DiskUsagePct
		}
		load1Last = sl.Load_1M
		load5Last = sl.Load_5M
		load15Last = sl.Load_15M
	}

	// Net bytes: use last sample (cumulative counters — delta computed by control-plane).
	lastSL := last.Snapshot.GetRuntime().GetSystemLoad()
	var netSent, netRecv uint64
	if lastSL != nil {
		netSent = lastSL.NetBytesSent
		netRecv = lastSL.NetBytesRecv
	}

	result.Runtime.AggregatedSystemLoad = &gatewayrpc.AggregatedSystemLoad{
		CpuPctAvg:    cpuSum / float64(n),
		CpuPctMax:    cpuMax,
		MemPctAvg:    memSum / float64(n),
		MemPctMax:    memMax,
		DiskPctAvg:   diskSum / float64(n),
		DiskPctMax:   diskMax,
		Load_1M:      load1Last,
		Load_5M:      load5Last,
		Load_15M:     load15Last,
		NetBytesSent: netSent,
		NetBytesRecv: netRecv,
	}

	// Aggregate connections: avg + max
	var connSum, connMax int32
	var connMESum, connDirectSum, usersSum, usersMax int32
	for _, s := range samples {
		rt := s.Snapshot.GetRuntime()
		if rt == nil {
			continue
		}
		connSum += rt.CurrentConnections
		if rt.CurrentConnections > connMax {
			connMax = rt.CurrentConnections
		}
		connMESum += rt.CurrentConnectionsMe
		connDirectSum += rt.CurrentConnectionsDirect
		usersSum += rt.ActiveUsers
		if rt.ActiveUsers > usersMax {
			usersMax = rt.ActiveUsers
		}
	}
	result.Runtime.AggregatedConnections = &gatewayrpc.AggregatedConnections{
		ConnectionsAvg:      connSum / int32(n),
		ConnectionsMax:      connMax,
		ConnectionsMeAvg:    connMESum / int32(n),
		ConnectionsDirectAvg: connDirectSum / int32(n),
		ActiveUsersAvg:      usersSum / int32(n),
		ActiveUsersMax:      usersMax,
	}

	// Aggregate DC health: avg + min coverage, avg + max rtt, min writers
	dcMap := make(map[int32]*gatewayrpc.AggregatedDCHealth)
	dcCounts := make(map[int32]int)
	for _, s := range samples {
		for _, dc := range s.Snapshot.GetRuntime().GetDcs() {
			agg, ok := dcMap[dc.Dc]
			if !ok {
				agg = &gatewayrpc.AggregatedDCHealth{
					Dc:              dc.Dc,
					CoveragePctMin:  dc.CoveragePct,
					AliveWritersMin: dc.AliveWriters,
					RequiredWriters: dc.RequiredWriters,
				}
				dcMap[dc.Dc] = agg
			}
			dcCounts[dc.Dc]++
			agg.CoveragePctAvg += dc.CoveragePct
			if dc.CoveragePct < agg.CoveragePctMin {
				agg.CoveragePctMin = dc.CoveragePct
			}
			agg.RttMsAvg += dc.RttMs
			if dc.RttMs > agg.RttMsMax {
				agg.RttMsMax = dc.RttMs
			}
			if dc.AliveWriters < agg.AliveWritersMin {
				agg.AliveWritersMin = dc.AliveWriters
			}
			if dc.Load > agg.LoadMax {
				agg.LoadMax = dc.Load
			}
			agg.RequiredWriters = dc.RequiredWriters
		}
	}
	aggregatedDCs := make([]*gatewayrpc.AggregatedDCHealth, 0, len(dcMap))
	for dc, agg := range dcMap {
		cnt := dcCounts[dc]
		agg.CoveragePctAvg /= float64(cnt)
		agg.RttMsAvg /= float64(cnt)
		aggregatedDCs = append(aggregatedDCs, agg)
	}
	result.Runtime.AggregatedDcs = aggregatedDCs
	result.Runtime.AggregationSamples = int32(n)

	return result
}
