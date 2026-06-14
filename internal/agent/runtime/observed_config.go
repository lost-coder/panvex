package runtime

import "github.com/lost-coder/panvex/internal/configcanon"

// observedConfigReporter delta-gates the observed managed-config report: it always
// returns the current canonical hash, and returns the full canonical JSON only
// when the hash changed since the last call. Not safe for concurrent use; called
// from the single snapshot-building goroutine.
type observedConfigReporter struct {
	lastHash string
}

// next returns (hash, jsonOrEmpty) for the given current editable sections.
func (o *observedConfigReporter) next(sections map[string]any) (string, string) {
	hash := configcanon.Hash(sections)
	if hash == o.lastHash {
		return hash, ""
	}
	o.lastHash = hash
	if sections == nil {
		sections = map[string]any{}
	}
	return hash, string(configcanon.CanonicalBytes(sections))
}
