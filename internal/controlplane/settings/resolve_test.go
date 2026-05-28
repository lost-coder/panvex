package settings

import "testing"

func TestEnvOverrideValue(t *testing.T) {
	field := FieldMeta{Name: "http.listen_address", Env: "PANVEX_HTTP_ADDR"}
	noEnv := FieldMeta{Name: "http.listen_address"}

	tests := []struct {
		name    string
		field   FieldMeta
		env     map[string]string
		wantVal string
		wantHit bool
	}{
		{"env set", field, map[string]string{"PANVEX_HTTP_ADDR": ":9090"}, ":9090", true},
		{"env empty", field, map[string]string{"PANVEX_HTTP_ADDR": ""}, "", false},
		{"env whitespace", field, map[string]string{"PANVEX_HTTP_ADDR": "  "}, "", false},
		{"env unset", field, map[string]string{}, "", false},
		{"field has no env", noEnv, map[string]string{"PANVEX_HTTP_ADDR": ":9090"}, "", false},
		{"nil env", field, nil, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hit := envOverrideValue(tt.field, tt.env)
			if got != tt.wantVal || hit != tt.wantHit {
				t.Errorf("envOverrideValue() = (%q, %v), want (%q, %v)", got, hit, tt.wantVal, tt.wantHit)
			}
		})
	}
}

func TestSeedValue(t *testing.T) {
	f := FieldMeta{
		Name:       "http.listen_address",
		Env:        "PANVEX_HTTP_ADDR",
		Toml:       "http.listen_address",
		Default:    ":8080",
		HasDefault: true,
	}

	tests := []struct {
		name     string
		env      map[string]string
		tomlVals map[string]string
		wantVal  string
		wantSrc  Source
		wantSeed bool
	}{
		{"env wins", map[string]string{"PANVEX_HTTP_ADDR": ":9090"}, map[string]string{"http.listen_address": ":7070"}, ":9090", SourceEnv, true},
		{"toml when no env", nil, map[string]string{"http.listen_address": ":7070"}, ":7070", SourceConfigFile, true},
		{"neither", nil, nil, "", SourceDefault, false},
		{"blank env falls to toml", map[string]string{"PANVEX_HTTP_ADDR": ""}, map[string]string{"http.listen_address": ":7070"}, ":7070", SourceConfigFile, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, src, seed := seedValue(f, tt.env, tt.tomlVals)
			if val != tt.wantVal || src != tt.wantSrc || seed != tt.wantSeed {
				t.Errorf("seedValue() = (%q, %v, %v), want (%q, %v, %v)", val, src, seed, tt.wantVal, tt.wantSrc, tt.wantSeed)
			}
		})
	}
}
