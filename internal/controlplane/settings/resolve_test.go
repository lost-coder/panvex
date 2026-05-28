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
