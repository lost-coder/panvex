package settings

import "strings"

// envOverrideValue returns the explicit environment value for f when its
// env var is set to a non-empty (non-whitespace) value, and reports
// whether such an override applies. A field with no Env, or an unset /
// blank var, yields ("", false).
func envOverrideValue(f FieldMeta, env map[string]string) (string, bool) {
	if f.Env == "" || env == nil {
		return "", false
	}
	v, ok := env[f.Env]
	if !ok || strings.TrimSpace(v) == "" {
		return "", false
	}
	return v, true
}
