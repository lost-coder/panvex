package settings

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// LoaderInput supplies the loader with everything it needs to resolve
// bootstrap values without touching package-globals (helps tests).
type LoaderInput struct {
	ConfigPath string   // "" disables the TOML source
	Env        []string // form "KEY=VALUE"; empty disables env source
}

// SourceInfo annotates each loaded value with its origin.
type SourceInfo struct {
	Source     Source // env|config_file|default
	SourcePath string // populated when Source==SourceConfigFile
	EnvVar     string // populated when Source==SourceEnv
}

// SourceMap maps a setting name → where its value came from.
type SourceMap map[string]SourceInfo

// LoadBootstrap reads bootstrap settings from env > config.toml >
// defaults and populates a typed *Bootstrap. Errors aggregate every
// missing/invalid field into a single message.
func LoadBootstrap(in LoaderInput) (*Bootstrap, SourceMap, error) {
	envMap := envSliceToMap(in.Env)
	tomlVals, tomlErr := loadTOMLValues(in.ConfigPath)

	fields := AllFields()
	bs := &Bootstrap{}
	srcs := SourceMap{}
	bsValue := reflect.ValueOf(bs).Elem()

	var loadErrors []string
	if tomlErr != nil {
		loadErrors = append(loadErrors, tomlErr.Error())
	}

	for _, f := range fields {
		if f.Class != ClassBootstrap {
			continue
		}
		raw, src := resolveBootstrap(f, envMap, tomlVals, in.ConfigPath)
		if src == SourceDefault && !f.HasDefault {
			loadErrors = append(loadErrors,
				fmt.Sprintf("missing required setting %q (set %s or in config.toml [%s])",
					f.Name, f.Env, f.Toml))
			continue
		}
		if raw != "" {
			if _, err := Validate(f, raw); err != nil {
				loadErrors = append(loadErrors, err.Error())
				continue
			}
		}
		if err := assignToField(bsValue, f, raw); err != nil {
			loadErrors = append(loadErrors,
				fmt.Sprintf("settings: %s: %v", f.Name, err))
			continue
		}
		info := SourceInfo{Source: src}
		switch src {
		case SourceEnv:
			info.EnvVar = f.Env
		case SourceConfigFile:
			info.SourcePath = in.ConfigPath
		}
		srcs[f.Name] = info
	}

	if len(loadErrors) > 0 {
		return nil, nil, fmt.Errorf("settings load failed:\n  - %s",
			strings.Join(loadErrors, "\n  - "))
	}
	return bs, srcs, nil
}

func envSliceToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		m[kv[:eq]] = kv[eq+1:]
	}
	return m
}

func loadTOMLValues(path string) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}
	var raw map[string]any
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, fmt.Errorf("settings: read %s: %w", path, err)
	}
	flat := map[string]string{}
	flatten("", raw, flat)
	return flat, nil
}

func flatten(prefix string, src map[string]any, out map[string]string) {
	for k, v := range src {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch t := v.(type) {
		case map[string]any:
			flatten(key, t, out)
		case string:
			out[key] = t
		case bool:
			out[key] = strconv.FormatBool(t)
		case int64:
			out[key] = strconv.FormatInt(t, 10)
		case float64:
			out[key] = strconv.FormatFloat(t, 'f', -1, 64)
		case time.Duration:
			out[key] = t.String()
		default:
			// arrays/other types unsupported in registry today; ignore.
		}
	}
}

func resolveBootstrap(f FieldMeta, env, tomlVals map[string]string, tomlPath string) (string, Source) {
	if v, ok := env[f.Env]; ok && f.Env != "" && strings.TrimSpace(v) != "" {
		return v, SourceEnv
	}
	if f.Toml != "" && tomlPath != "" {
		if v, ok := tomlVals[f.Toml]; ok {
			return v, SourceConfigFile
		}
	}
	if f.HasDefault {
		return f.Default, SourceDefault
	}
	return "", SourceDefault
}

func assignToField(target reflect.Value, f FieldMeta, raw string) error {
	field := target.FieldByName(f.GoField)
	if !field.IsValid() || !field.CanSet() {
		return fmt.Errorf("registry field %s not settable", f.GoField)
	}
	switch f.Type {
	case TypeInt:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		field.SetInt(n)
	default:
		// For all string-shaped types in the bootstrap registry we
		// store the raw text. Typed parsing is performed by callers
		// via Validate as needed.
		field.SetString(raw)
	}
	return nil
}
