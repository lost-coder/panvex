package settings

import "testing"

// BenchmarkSettingsGetterOnDefault measures a typed settings read that falls
// through to the registry default — the common case, since operators leave most
// settings alone. It used to re-run reflection over the whole registry and
// re-parse ~60 struct tags on every call (AllFields), then linearly scan the
// result for the field. Both are memoised now.
func BenchmarkSettingsGetterOnDefault(b *testing.B) {
	store := NewOperationalStore(nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if store.MetricsPollInterval() <= 0 {
			b.Fatal("MetricsPollInterval() = 0, want the registry default")
		}
	}
}
