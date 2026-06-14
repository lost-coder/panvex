package configcanon

import "testing"

func TestHashStableAcrossKeyOrder(t *testing.T) {
	a := map[string]any{"censorship": map[string]any{"tls_domain": "x", "mask": true}}
	b := map[string]any{"censorship": map[string]any{"mask": true, "tls_domain": "x"}}
	if Hash(a) != Hash(b) {
		t.Fatalf("hash must be key-order independent")
	}
}

func TestHashDiffersOnValue(t *testing.T) {
	a := map[string]any{"general": map[string]any{"log_level": "info"}}
	b := map[string]any{"general": map[string]any{"log_level": "debug"}}
	if Hash(a) == Hash(b) {
		t.Fatalf("hash must change when a value changes")
	}
}

func TestCanonicalBytesSorted(t *testing.T) {
	got := string(CanonicalBytes(map[string]any{"b": 1, "a": 2}))
	if got != `{"a":2,"b":1}` {
		t.Fatalf("canonical bytes not sorted: %s", got)
	}
}

func TestHashEmptyAndNilEqual(t *testing.T) {
	if Hash(nil) != Hash(map[string]any{}) {
		t.Fatalf("nil and empty must hash equal")
	}
}
