package updates

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
		{"v1.2.3", "1.2.3", 0},
		{"1.2.3-rc1", "1.2.3", -1},
	}
	for _, tt := range tests {
		got, err := CompareVersions(tt.a, tt.b)
		if err != nil {
			t.Fatalf("CompareVersions(%q, %q) unexpected error: %v", tt.a, tt.b, err)
		}
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

// R1.5 (audit 2026-07-07 §1.14): malformed versions must be an error,
// not a silent zero ("1.x.3" used to compare equal to "1.0.3").
func TestCompareVersionsRejectsMalformed(t *testing.T) {
	for _, pair := range [][2]string{
		{"1.x.3", "1.0.3"},
		{"", "1.0.0"},
		{"1.0.0", "abc"},
		{"dev", "1.0.0"},
	} {
		if _, err := CompareVersions(pair[0], pair[1]); err == nil {
			t.Errorf("CompareVersions(%q, %q) must return an error", pair[0], pair[1])
		}
	}
}

func TestResolveAssetURLsNilRelease(t *testing.T) {
	b, c := ResolveAssetURLs(nil, "control-plane")
	if b != "" || c != "" {
		t.Fatalf("ResolveAssetURLs(nil, ...) = (%q, %q), want empty strings", b, c)
	}
}
