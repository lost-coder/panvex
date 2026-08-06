package telemtupdate

import (
	"testing"
)

func TestAssetName(t *testing.T) {
	tests := []struct {
		goarch, flavor string
		musl           bool
		want           string
		wantErr        bool
	}{
		{goarch: "amd64", want: "telemt-x86_64-linux-gnu"},
		{goarch: "amd64", flavor: "v3", want: "telemt-x86_64-v3-linux-gnu"},
		{goarch: "amd64", musl: true, want: "telemt-x86_64-linux-musl"},
		{goarch: "amd64", musl: true, flavor: "v3", want: "telemt-x86_64-v3-linux-musl"},
		{goarch: "arm64", want: "telemt-aarch64-linux-gnu"},
		{goarch: "arm64", musl: true, want: "telemt-aarch64-linux-musl"},
		{goarch: "arm64", flavor: "v3", wantErr: true},
		{goarch: "mips", wantErr: true},
		{goarch: "amd64", flavor: "avx512", wantErr: true},
	}
	for _, tt := range tests {
		name := tt.goarch
		if tt.musl {
			name += "+musl"
		}
		if tt.flavor != "" {
			name += "+" + tt.flavor
		}
		if tt.wantErr {
			name += "+err"
		}
		t.Run(name, func(t *testing.T) {
			got, err := assetName(tt.goarch, tt.musl, tt.flavor)
			if (err != nil) != tt.wantErr {
				t.Errorf("assetName(%q, %v, %q) error = %v, wantErr %v", tt.goarch, tt.musl, tt.flavor, err, tt.wantErr)
				return
			}
			if err == nil && got != tt.want {
				t.Errorf("assetName(%q, %v, %q) = %q, want %q", tt.goarch, tt.musl, tt.flavor, got, tt.want)
			}
		})
	}
}

func TestDetectMusl(t *testing.T) {
	found := func(string) ([]string, error) { return []string{"/lib/ld-musl-x86_64.so.1"}, nil }
	none := func(string) ([]string, error) { return nil, nil }
	if !detectMusl(found) || detectMusl(none) {
		t.Fatal("detectMusl wrong")
	}
}
