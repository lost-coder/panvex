package server

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
