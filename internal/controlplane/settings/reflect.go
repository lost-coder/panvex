package settings

import (
	"fmt"
	"reflect"
)

// walkRegistry inspects the exported fields of a registry struct type
// and returns their parsed metadata. Each field must carry a
// `setting:"…"` tag; unrelated fields are not allowed in registry
// structs.
func walkRegistry(t reflect.Type, class Class) ([]FieldMeta, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("settings: walkRegistry needs struct, got %s", t.Kind())
	}
	out := make([]FieldMeta, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			return nil, fmt.Errorf("settings: %s: registry fields must be exported", sf.Name)
		}
		tag, ok := sf.Tag.Lookup("setting")
		if !ok {
			return nil, fmt.Errorf("settings: %s: missing `setting` tag", sf.Name)
		}
		meta, err := parseTag(tag)
		if err != nil {
			return nil, fmt.Errorf("settings: %s: %w", sf.Name, err)
		}
		meta.GoField = sf.Name
		meta.Class = class
		out = append(out, meta)
	}
	return out, nil
}
