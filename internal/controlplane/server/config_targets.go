package server

import (
	"fmt"
	"strings"
)

// editableConfigSections is the allowlist of top-level Telemt config
// sections an operator may store via the config-target endpoints. It
// mirrors Telemt's PATCH /v1/config EDITABLE_SECTIONS exactly — any other
// top-level key (e.g. access/server/network) is rejected with 400 on PUT.
// Note show_link is NOT editable: it is a legacy key Telemt auto-migrates
// to general.links.show on load and rejects on PATCH (telemt-audit F4).
var editableConfigSections = map[string]struct{}{
	"general": {}, "timeouts": {}, "censorship": {},
	"upstreams": {}, "dc_overrides": {},
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

// processOwnedPaths mirrors web/src/features/servers/config/guard.ts
// PROCESS_OWNED_PATHS: fields Telemt reads only at process start. The
// in-process Maestro reload cannot apply changes to them — only a real
// process restart can, and that machinery was removed. The web editor
// already keeps these out of CONFIG_FIELDS, but a config target can also
// be written directly against the API, so the server enforces the same
// invariant rather than reporting a false success.
var processOwnedPaths = map[string]struct{}{
	"general.data_path":        {},
	"general.quota_state_path": {},
	"general.disable_colors":   {},
}

// validateNoProcessOwnedFields returns an error if sections contains any of
// the process-owned dotted paths in processOwnedPaths.
func validateNoProcessOwnedFields(sections map[string]any) error {
	for path := range processOwnedPaths {
		section, field, _ := strings.Cut(path, ".")
		sub, ok := sections[section].(map[string]any)
		if !ok {
			continue
		}
		if _, present := sub[field]; present {
			return fmt.Errorf("field %s requires a process restart, which is not supported by in-process reload", path)
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
