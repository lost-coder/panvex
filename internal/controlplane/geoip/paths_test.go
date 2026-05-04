package geoip_test

import (
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/geoip"
)

func TestResolveDirEnvWins(t *testing.T) {
	t.Setenv("PANVEX_GEOIP_DIR", "/explicit/dir")
	got := geoip.ResolveDir("/data/panvex.db", "/var/lib/panvex/geoip")
	if got != "/explicit/dir" {
		t.Errorf("got %q, want /explicit/dir", got)
	}
}

func TestResolveDirSQLiteFallback(t *testing.T) {
	t.Setenv("PANVEX_GEOIP_DIR", "")
	got := geoip.ResolveDir("/data/panvex.db", "/var/lib/panvex/geoip")
	want := "/data/geoip"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveDirGenericFallback(t *testing.T) {
	t.Setenv("PANVEX_GEOIP_DIR", "")
	got := geoip.ResolveDir("", "/var/lib/panvex/geoip")
	want := "/var/lib/panvex/geoip"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestPathFor(t *testing.T) {
	cases := map[geoip.Kind]string{
		geoip.KindCity: filepath.Join("/dir", "city.mmdb"),
		geoip.KindASN:  filepath.Join("/dir", "asn.mmdb"),
	}
	for k, want := range cases {
		if got := geoip.PathFor("/dir", k); got != want {
			t.Errorf("PathFor(%q) = %q, want %q", k, got, want)
		}
	}
}
