package settings

import (
	"testing"
)

func TestParseTag_BootstrapHostPort(t *testing.T) {
	in := `name=http.listen_address, type=hostport, default=:8080,
        env=PANVEX_HTTP_ADDR, toml=http.listen_address,
        desc='HTTP bind address.'`
	got, err := parseTag(in)
	if err != nil {
		t.Fatalf("parseTag returned error: %v", err)
	}
	want := FieldMeta{
		Name:       "http.listen_address",
		Type:       TypeHostPort,
		Default:    ":8080",
		HasDefault: true,
		Env:        "PANVEX_HTTP_ADDR",
		Toml:       "http.listen_address",
		Desc:       "HTTP bind address.",
	}
	if got.Name != want.Name || got.Type != want.Type || got.Default != want.Default ||
		!got.HasDefault || got.Env != want.Env || got.Toml != want.Toml || got.Desc != want.Desc {
		t.Fatalf("parseTag mismatch:\n got = %+v\nwant = %+v", got, want)
	}
}
