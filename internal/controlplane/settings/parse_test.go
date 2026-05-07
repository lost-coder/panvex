package settings

import (
	"strings"
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

func TestParseTag_OperationalIntWithBounds(t *testing.T) {
	in := `name=auth.password_min_length, type=int, default=10,
        min=8, max=64, restart=false,
        store=panel_settings.password_min_length,
        desc='Minimum length for new passwords.'`
	got, err := parseTag(in)
	if err != nil {
		t.Fatalf("parseTag err: %v", err)
	}
	if got.Min != "8" || got.Max != "64" {
		t.Fatalf("min/max: got (%q,%q)", got.Min, got.Max)
	}
	if got.Restart {
		t.Fatalf("restart should be false")
	}
	if got.Store != "panel_settings.password_min_length" {
		t.Fatalf("store wrong: %q", got.Store)
	}
}

func TestParseTag_EnumValues(t *testing.T) {
	in := `name=tls.mode, type=enum, values=proxy|direct, default=proxy,
        env=PANVEX_TLS_MODE, toml=tls.mode, desc='TLS termination mode.'`
	got, err := parseTag(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Values) != 2 || got.Values[0] != "proxy" || got.Values[1] != "direct" {
		t.Fatalf("values: %#v", got.Values)
	}
}

func TestParseTag_Secret(t *testing.T) {
	in := `name=auth.encryption_key, type=string, secret=true,
        env=PANVEX_ENCRYPTION_KEY, desc='Master at-rest key.'`
	got, err := parseTag(in)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Secret {
		t.Fatal("Secret should be true")
	}
}

func TestParseTag_Errors(t *testing.T) {
	cases := []struct {
		name, in, wantSubstr string
	}{
		{"missing name",
			`type=int, default=1, desc='x'`, "missing required attribute 'name'"},
		{"missing type",
			`name=foo, default=1, desc='x'`, "missing required attribute 'type'"},
		{"missing desc",
			`name=foo, type=int, default=1`, "missing required attribute 'desc'"},
		{"unknown key",
			`name=foo, type=int, desc='x', wat=1`, "unknown tag attribute"},
		{"unterminated quote",
			`name=foo, type=int, desc='x`, "unterminated single quote"},
		{"missing equals",
			`name=foo, type=int, desc='x', loose`, "has no '='"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseTag(tc.in)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Fatalf("err = %v, want substring %q", err, tc.wantSubstr)
			}
		})
	}
}
