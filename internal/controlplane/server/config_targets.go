package server

import "fmt"

// editableConfigSections is the allowlist of top-level Telemt config
// sections an operator may store via the config-target endpoints. It
// mirrors the agent's editable contract — any other top-level key
// (e.g. access/server/network) is rejected with 400 on PUT.
var editableConfigSections = map[string]struct{}{
	"general": {}, "timeouts": {}, "censorship": {},
	"upstreams": {}, "show_link": {}, "dc_overrides": {},
}

// validateEditableSections returns an error if any top-level key is not
// in the editable allowlist.
func validateEditableSections(sections map[string]any) error {
	for k := range sections {
		if _, ok := editableConfigSections[k]; !ok {
			return fmt.Errorf("section not editable: %s", k)
		}
	}
	return nil
}

// resolveEffectiveConfig merges a group config target with an agent override.
// The agent override wins per field (deep-merged: nested section tables merge
// key-by-key; scalars and arrays in the override replace the group value).
// Either argument may be nil. The result is a fresh map (inputs are not mutated).
func resolveEffectiveConfig(group, override map[string]any) map[string]any {
	out := deepCopyConfigMap(group)
	if out == nil {
		out = map[string]any{}
	}
	deepMergeConfigInto(out, override)
	return out
}

func deepCopyConfigMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if sub, ok := v.(map[string]any); ok {
			out[k] = deepCopyConfigMap(sub)
		} else {
			out[k] = v
		}
	}
	return out
}

// deepMergeConfigInto overlays patch onto base: matching sub-maps merge
// recursively; every other value (scalars, arrays) replaces.
func deepMergeConfigInto(base, patch map[string]any) {
	for k, pv := range patch {
		pvMap, pIsMap := pv.(map[string]any)
		bvMap, bIsMap := base[k].(map[string]any)
		if pIsMap && bIsMap {
			deepMergeConfigInto(bvMap, pvMap)
		} else if pIsMap {
			base[k] = deepCopyConfigMap(pvMap)
		} else {
			base[k] = pv
		}
	}
}
