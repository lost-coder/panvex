package server

import (
	"bytes"

	"github.com/lost-coder/panvex/internal/configcanon"
)

// configDrift reports whether observed drifts from the (effective) target. Drift
// is a PROJECTION of observed onto target: for every leaf (path,value) in target,
// observed must contain an equal value. Fields present in observed but absent
// from target are NOT drift (the operator does not manage them). Returns the
// drifted bool and the list of mismatching dotted paths.
func configDrift(target, observed map[string]any) (bool, []string) {
	var diffs []string
	walkConfigTarget("", target, observed, &diffs)
	return len(diffs) > 0, diffs
}

func walkConfigTarget(prefix string, target, observed map[string]any, diffs *[]string) {
	for k, tv := range target {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		ov, present := observed[k]
		tvMap, tIsMap := tv.(map[string]any)
		ovMap, oIsMap := ov.(map[string]any)
		switch {
		case tIsMap:
			if !present || !oIsMap {
				*diffs = append(*diffs, path)
				continue
			}
			walkConfigTarget(path, tvMap, ovMap, diffs)
		default:
			if !present || !configLeafEqual(tv, ov) {
				*diffs = append(*diffs, path)
			}
		}
	}
}

// configLeafEqual compares scalar/array leaves by the shared canonical encoding
// so representation differences (e.g. JSON number forms) do not produce false
// drift, while distinct types (e.g. bool true vs string "true") are not collapsed.
func configLeafEqual(a, b any) bool {
	return bytes.Equal(configcanon.CanonicalBytes(a), configcanon.CanonicalBytes(b))
}
