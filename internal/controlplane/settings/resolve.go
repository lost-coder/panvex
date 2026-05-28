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

// seedValue resolves the first-boot seed for f: an explicit env var wins,
// then a config.toml value, otherwise there is no seed (a registry
// default is left implicit and is never written to the store).
func seedValue(f FieldMeta, env, tomlVals map[string]string) (string, Source, bool) {
	if v, ok := envOverrideValue(f, env); ok {
		return v, SourceEnv, true
	}
	if f.Toml != "" && tomlVals != nil {
		if v, ok := tomlVals[f.Toml]; ok && strings.TrimSpace(v) != "" {
			return v, SourceConfigFile, true
		}
	}
	return "", SourceDefault, false
}
