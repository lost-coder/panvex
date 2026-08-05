package server

import (
	"reflect"
	"testing"
)

func TestApplyDiff(t *testing.T) {
	desired := map[string]any{
		"general":  map[string]any{"log_level": "normal", "fast_mode": true},
		"timeouts": map[string]any{"client_ack": float64(90)},
	}
	observed := map[string]any{
		"general":  map[string]any{"log_level": "silent", "fast_mode": true},
		"timeouts": map[string]any{"client_ack": float64(90)},
	}
	// only log_level differs
	got := applyDiff(desired, observed, nil)
	want := map[string]any{"general": map[string]any{"log_level": "normal"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diff = %#v, want %#v", got, want)
	}
}

func TestApplyDiffRestrictAndProcessOwned(t *testing.T) {
	desired := map[string]any{"general": map[string]any{
		"log_level": "normal", "ad_tag": "x", "data_path": "/new"}}
	observed := map[string]any{"general": map[string]any{
		"log_level": "silent", "ad_tag": "y", "data_path": "/old"}}
	// restrict to log_level only
	got := applyDiff(desired, observed, []string{"general.log_level"})
	if !reflect.DeepEqual(got, map[string]any{"general": map[string]any{"log_level": "normal"}}) {
		t.Fatalf("restricted diff = %#v", got)
	}
	// process-owned never emitted even when it differs
	all := applyDiff(desired, observed, nil)
	if g := all["general"].(map[string]any); g["data_path"] != nil {
		t.Fatal("data_path must be excluded from apply diff")
	}
}
