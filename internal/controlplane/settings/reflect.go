package settings

import (
	"fmt"
	"reflect"
	"sync"
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

// AllFields returns the canonical-ordered concatenation of the Bootstrap and
// Operational registries. Used by codegen and the HTTP layer; never returns an
// error because the registry is statically validated by the test suite.
//
// The result is computed once: it is derived from struct tags, which are fixed
// at compile time. It used to re-run reflection over ~60 fields and re-parse
// every tag on each call — and the settings getters call it on every read, so
// that was happening on request paths.
//
// The returned slice is shared. Callers must not mutate it.
var AllFields = sync.OnceValue(func() []FieldMeta {
	bs, err := walkRegistry(reflect.TypeOf(Bootstrap{}), ClassBootstrap)
	if err != nil {
		panic("settings: invalid Bootstrap registry: " + err.Error())
	}
	op, err := walkRegistry(reflect.TypeOf(Operational{}), ClassOperational)
	if err != nil {
		panic("settings: invalid Operational registry: " + err.Error())
	}
	return append(bs, op...)
})

// fieldByName indexes AllFields by setting name so the getters can look up a
// field's registry default in O(1) instead of scanning the whole registry on
// every settings read.
var fieldByName = sync.OnceValue(func() map[string]FieldMeta {
	fields := AllFields()
	index := make(map[string]FieldMeta, len(fields))
	for _, f := range fields {
		index[f.Name] = f
	}
	return index
})

// defaultFor returns the registry default for a setting. The registry is the
// SINGLE source of these values — getters must not carry their own fallback
// literal, which is how auth.password_min_length ended up with a 10 in the
// registry and another 10 in the getter.
func defaultFor(name string) (string, bool) {
	f, ok := fieldByName()[name]
	if !ok || !f.HasDefault {
		return "", false
	}
	return f.Default, true
}
