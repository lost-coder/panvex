package server

import "testing"

func TestParseReleaseTag(t *testing.T) {
	tests := []struct {
		tag       string
		component string
		version   string
		ok        bool
	}{
		{"control-plane/v0.2.3", "control-plane", "0.2.3", true},
		{"agent/v0.1.5", "agent", "0.1.5", true},
		{"v1.0.0", "", "", false},
		{"invalid", "", "", false},
		{"agent/", "", "", false},
	}
	for _, tt := range tests {
		component, version, ok := ParseReleaseTag(tt.tag)
		if ok != tt.ok || component != tt.component || version != tt.version {
			t.Errorf("ParseReleaseTag(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.tag, component, version, ok, tt.component, tt.version, tt.ok)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"0.2.0", "0.1.0", 1},
		{"0.1.0", "0.2.0", -1},
		{"1.0.0", "0.9.9", 1},
		{"0.1.2", "0.1.10", -1},
	}
	for _, tt := range tests {
		got := CompareVersions(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
