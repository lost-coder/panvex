package settings

import (
	"bytes"
	"encoding/json"
)

type schemaEntry struct {
	Name    string   `json:"name"`
	Class   Class    `json:"class"`
	Type    Type     `json:"type"`
	Default *string  `json:"default,omitempty"`
	Min     string   `json:"min,omitempty"`
	Max     string   `json:"max,omitempty"`
	Values  []string `json:"values,omitempty"`
	Env     string   `json:"env,omitempty"`
	Toml    string   `json:"toml,omitempty"`
	Secret  bool     `json:"secret,omitempty"`
	Store   string   `json:"store,omitempty"`
	Restart bool     `json:"restart,omitempty"`
	Desc    string   `json:"desc"`
}

// RenderSchemaJSON returns the canonical JSON encoding of the registry
// for consumption by the dashboard.
func RenderSchemaJSON() ([]byte, error) {
	fields := AllFields()
	out := make([]schemaEntry, 0, len(fields))
	for _, f := range fields {
		e := schemaEntry{
			Name:    f.Name,
			Class:   f.Class,
			Type:    f.Type,
			Min:     f.Min,
			Max:     f.Max,
			Values:  f.Values,
			Env:     f.Env,
			Toml:    f.Toml,
			Secret:  f.Secret,
			Store:   f.Store,
			Restart: f.Restart,
			Desc:    f.Desc,
		}
		if f.HasDefault && !f.Secret {
			d := f.Default
			e.Default = &d
		}
		out = append(out, e)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
