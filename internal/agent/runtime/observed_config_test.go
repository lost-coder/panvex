package runtime

import (
	"testing"

	"github.com/lost-coder/panvex/internal/configcanon"
)

func TestObservedConfigDeltaGate(t *testing.T) {
	sections := map[string]any{"censorship": map[string]any{"tls_domain": "a"}}
	oc := &observedConfigReporter{}

	h1, j1 := oc.next(sections)
	if h1 != configcanon.Hash(sections) || j1 == "" {
		t.Fatalf("first report must include hash + json, got h=%q j=%q", h1, j1)
	}
	h2, j2 := oc.next(sections)
	if h2 != h1 || j2 != "" {
		t.Fatalf("unchanged report must omit json, got h=%q j=%q", h2, j2)
	}
	sections2 := map[string]any{"censorship": map[string]any{"tls_domain": "b"}}
	h3, j3 := oc.next(sections2)
	if h3 == h1 || j3 == "" {
		t.Fatalf("changed report must include json, got h=%q j=%q", h3, j3)
	}
}
