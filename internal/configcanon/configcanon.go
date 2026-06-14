// Package configcanon defines the ONE canonical serialization of managed Telemt
// config sections shared by the agent (which hashes its observed config) and the
// control plane (which hashes targets and compares against observed). Both sides
// MUST use this package so equal configs hash equal and drift is not spurious.
package configcanon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// CanonicalBytes returns a deterministic JSON encoding of v with all object keys
// sorted recursively.
func CanonicalBytes(v any) []byte {
	c := canonicalize(v)
	b, err := json.Marshal(c)
	if err != nil {
		return []byte("null")
	}
	return b
}

// Hash returns the hex SHA-256 of the canonical encoding of the managed config
// map. nil and empty map hash identically.
func Hash(m map[string]any) string {
	if m == nil {
		m = map[string]any{}
	}
	sum := sha256.Sum256(CanonicalBytes(m))
	return hex.EncodeToString(sum[:])
}

// canonicalize returns a value whose json.Marshal output has sorted object keys.
func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(orderedMap, 0, len(keys))
		for _, k := range keys {
			out = append(out, kv{k, canonicalize(t[k])})
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = canonicalize(t[i])
		}
		return out
	default:
		return v
	}
}

type kv struct {
	K string
	V any
}

// orderedMap marshals as a JSON object preserving insertion (sorted) order.
type orderedMap []kv

func (o orderedMap) MarshalJSON() ([]byte, error) {
	buf := []byte{'{'}
	for i, e := range o {
		if i > 0 {
			buf = append(buf, ',')
		}
		key, err := json.Marshal(e.K)
		if err != nil {
			return nil, err
		}
		val, err := json.Marshal(e.V)
		if err != nil {
			return nil, err
		}
		buf = append(buf, key...)
		buf = append(buf, ':')
		buf = append(buf, val...)
	}
	buf = append(buf, '}')
	return buf, nil
}
