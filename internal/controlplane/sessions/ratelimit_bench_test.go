package sessions

import (
	"strconv"
	"testing"
	"time"
)

// BenchmarkRateLimiterAllowManyKeys measures Allow with a realistically sized
// bucket map (one bucket per source IP). The stale-bucket sweep is O(buckets)
// under the limiter's lock and used to run on EVERY call once the map passed
// 128 entries; it is amortised over sweepEvery calls now.
func BenchmarkRateLimiterAllowManyKeys(b *testing.B) {
	limiter := NewRateLimiter(1_000_000, time.Minute)
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	keys := make([]string, 2000)
	for i := range keys {
		keys[i] = "10.0." + strconv.Itoa(i/256) + "." + strconv.Itoa(i%256)
		limiter.Allow(keys[i], now)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Allow(keys[i%len(keys)], now)
	}
}
