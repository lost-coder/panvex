package server

import "testing"

func TestConfigDriftProjection(t *testing.T) {
	target := map[string]any{"censorship": map[string]any{"tls_domain": "want"}}

	inSync := map[string]any{"censorship": map[string]any{"tls_domain": "want", "mask": true}, "general": map[string]any{"log_level": "info"}}
	if d, _ := configDrift(target, inSync); d {
		t.Fatalf("extra observed fields must not count as drift")
	}

	drifted := map[string]any{"censorship": map[string]any{"tls_domain": "actual"}}
	if d, fields := configDrift(target, drifted); !d || len(fields) == 0 {
		t.Fatalf("value mismatch on managed field must be drift, got d=%v fields=%v", d, fields)
	}

	missing := map[string]any{"general": map[string]any{"log_level": "info"}}
	if d, _ := configDrift(target, missing); !d {
		t.Fatalf("missing managed field must be drift")
	}

	if d, _ := configDrift(map[string]any{}, drifted); d {
		t.Fatalf("empty target -> in sync")
	}
}

func TestConfigDriftLeafTypesNotCollapsed(t *testing.T) {
	// bool true and string "true" must be treated as different (drift), not
	// collapsed by a %v-style comparison.
	if d, _ := configDrift(
		map[string]any{"general": map[string]any{"x": true}},
		map[string]any{"general": map[string]any{"x": "true"}},
	); !d {
		t.Fatalf("bool true vs string \"true\" must be drift")
	}
	// equal numeric forms (float64) must NOT drift.
	if d, _ := configDrift(
		map[string]any{"timeouts": map[string]any{"n": float64(30)}},
		map[string]any{"timeouts": map[string]any{"n": float64(30)}},
	); d {
		t.Fatalf("equal numeric leaves must be in sync")
	}
}
