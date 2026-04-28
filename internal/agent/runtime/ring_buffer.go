package runtime

import (
	"sync"
	"time"

	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

// RuntimeSample stores one poll result from Telemt + gopsutil.
type RuntimeSample struct {
	ObservedAt time.Time
	Snapshot   *gatewayrpc.Snapshot
}

// RuntimeRingBuffer accumulates poll samples between upload ticks. Push
// is O(1) — head/count indices wrap on overflow instead of shifting
// every prior sample one slot left, which used to dominate the agent's
// runtime poll cost when AggregationSamples > a few hundred.
type RuntimeRingBuffer struct {
	mu      sync.Mutex
	samples []RuntimeSample // fixed-size storage, len(samples) == cap
	head    int             // index of the next write
	count   int             // number of valid entries (0..cap)
	cap     int
	dropped uint64
}

func NewRuntimeRingBuffer(capacity int) *RuntimeRingBuffer {
	return &RuntimeRingBuffer{
		samples: make([]RuntimeSample, capacity),
		cap:     capacity,
	}
}

// Push adds a sample. If at capacity, the oldest sample is overwritten
// in place and the dropped counter is bumped — O(1) regardless of cap.
func (b *RuntimeRingBuffer) Push(sample RuntimeSample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.samples[b.head] = sample
	b.head = (b.head + 1) % b.cap
	if b.count < b.cap {
		b.count++
	} else {
		b.dropped++
	}
}

// DroppedCount returns the number of samples dropped due to capacity overflow.
func (b *RuntimeRingBuffer) DroppedCount() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// snapshotOrdered returns the buffered samples in oldest-first order
// without holding the lock past the copy. The caller takes ownership.
func (b *RuntimeRingBuffer) snapshotOrdered() []RuntimeSample {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.count == 0 {
		return nil
	}
	out := make([]RuntimeSample, b.count)
	start := (b.head - b.count + b.cap) % b.cap
	for i := 0; i < b.count; i++ {
		out[i] = b.samples[(start+i)%b.cap]
	}
	// Reset for the next interval. The fixed-size storage is reused;
	// only zero out the slots we wrote so prior pointers can be GC'd.
	for i := range b.samples {
		b.samples[i] = RuntimeSample{}
	}
	b.head = 0
	b.count = 0
	return out
}

// DrainAndAggregate returns a single aggregated snapshot from all buffered samples,
// then clears the buffer. Returns nil if no samples exist.
func (b *RuntimeRingBuffer) DrainAndAggregate() *gatewayrpc.Snapshot {
	samples := b.snapshotOrdered()
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

	result.Runtime.AggregatedSystemLoad = aggregateSystemLoad(samples, last, n)
	result.Runtime.AggregatedConnections = aggregateConnections(samples, n)
	result.Runtime.AggregatedDcs = aggregateDCs(samples)
	result.Runtime.AggregationSamples = int32(n)

	return result
}

// aggregateSystemLoad computes avg/max system-load metrics across samples,
// keeping the last sample's load averages and net byte counters as-is
// (cumulative counters — delta computed by control-plane).
func aggregateSystemLoad(samples []RuntimeSample, last RuntimeSample, n int) *gatewayrpc.AggregatedSystemLoad {
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

	return &gatewayrpc.AggregatedSystemLoad{
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
}

// aggregateConnections computes avg/max connection counts across samples.
func aggregateConnections(samples []RuntimeSample, n int) *gatewayrpc.AggregatedConnections {
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
	return &gatewayrpc.AggregatedConnections{
		ConnectionsAvg:       connSum / int32(n),
		ConnectionsMax:       connMax,
		ConnectionsMeAvg:     connMESum / int32(n),
		ConnectionsDirectAvg: connDirectSum / int32(n),
		ActiveUsersAvg:       usersSum / int32(n),
		ActiveUsersMax:       usersMax,
	}
}

// aggregateDCs computes per-DC avg/min coverage, avg/max rtt and min
// writers across samples.
func aggregateDCs(samples []RuntimeSample) []*gatewayrpc.AggregatedDCHealth {
	dcMap := make(map[int32]*gatewayrpc.AggregatedDCHealth)
	dcCounts := make(map[int32]int)
	for _, s := range samples {
		for _, dc := range s.Snapshot.GetRuntime().GetDcs() {
			accumulateDCSample(dcMap, dcCounts, dc)
		}
	}
	aggregatedDCs := make([]*gatewayrpc.AggregatedDCHealth, 0, len(dcMap))
	for dc, agg := range dcMap {
		cnt := dcCounts[dc]
		agg.CoveragePctAvg /= float64(cnt)
		agg.RttMsAvg /= float64(cnt)
		aggregatedDCs = append(aggregatedDCs, agg)
	}
	return aggregatedDCs
}

// accumulateDCSample folds one DC sample into the running aggregate map.
func accumulateDCSample(dcMap map[int32]*gatewayrpc.AggregatedDCHealth, dcCounts map[int32]int, dc *gatewayrpc.RuntimeDCSnapshot) {
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
