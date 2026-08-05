package server

var applyProcessOwned = map[string]struct{}{
	"general.data_path": {}, "general.quota_state_path": {}, "general.disable_colors": {},
}

// applyDiff returns the nested subset of desired whose leaves differ from
// observed, excluding process-owned paths. When restrictPaths is non-empty,
// only those dotted paths are considered. An empty result means the node is
// already in sync.
func applyDiff(desired, observed map[string]any, restrictPaths []string) map[string]any {
	var allow map[string]struct{}
	if len(restrictPaths) > 0 {
		allow = make(map[string]struct{}, len(restrictPaths))
		for _, p := range restrictPaths {
			allow[p] = struct{}{}
		}
	}
	out := map[string]any{}
	var walk func(prefix string, d, o map[string]any, dst map[string]any)
	walk = func(prefix string, d, o map[string]any, dst map[string]any) {
		for k, dv := range d {
			if k == "__schema_version" {
				continue
			}
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if dm, ok := dv.(map[string]any); ok {
				om, _ := o[k].(map[string]any)
				sub := map[string]any{}
				walk(path, dm, om, sub)
				if len(sub) > 0 {
					dst[k] = sub
				}
				continue
			}
			if _, owned := applyProcessOwned[path]; owned {
				continue
			}
			if allow != nil {
				if _, ok := allow[path]; !ok {
					continue
				}
			}
			ov, present := o[k]
			if !present || !configLeafEqual(dv, ov) {
				dst[k] = dv
			}
		}
	}
	walk("", desired, observed, out)
	return out
}
