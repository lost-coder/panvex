package server

import (
	"reflect"
	"testing"
)

func TestResolveEffectiveConfig(t *testing.T) {
	group := map[string]any{
		"censorship": map[string]any{"tls_domain": "group.example", "fake_cert_len": float64(100)},
		"general":    map[string]any{"log_level": "info"},
	}
	override := map[string]any{
		"censorship": map[string]any{"tls_domain": "node.example"}, // override wins, sibling kept
		"timeouts":   map[string]any{"client_handshake": float64(5)},
	}
	got := resolveEffectiveConfig(group, override)
	want := map[string]any{
		"censorship": map[string]any{"tls_domain": "node.example", "fake_cert_len": float64(100)},
		"general":    map[string]any{"log_level": "info"},
		"timeouts":   map[string]any{"client_handshake": float64(5)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestResolveEffectiveConfigNilOverride(t *testing.T) {
	group := map[string]any{"general": map[string]any{"log_level": "info"}}
	got := resolveEffectiveConfig(group, nil)
	if !reflect.DeepEqual(got, group) {
		t.Fatalf("nil override should equal group: %#v", got)
	}
}

func TestResolveEffectiveConfigDoesNotMutateInputs(t *testing.T) {
	group := map[string]any{"censorship": map[string]any{"tls_domain": "g"}}
	override := map[string]any{"censorship": map[string]any{"tls_domain": "o"}}
	_ = resolveEffectiveConfig(group, override)
	if group["censorship"].(map[string]any)["tls_domain"] != "g" {
		t.Fatalf("group input was mutated")
	}
	if override["censorship"].(map[string]any)["tls_domain"] != "o" {
		t.Fatalf("override input was mutated")
	}
}
