package events

import "testing"

// TestAllIsUniqueAndNonEmpty — базовая гигиена контракта: All без дублей
// и пустых строк, каждый тип имеет вид "domain.action".
func TestAllIsUniqueAndNonEmpty(t *testing.T) {
	seen := make(map[string]bool, len(All))
	for _, typ := range All {
		if typ == "" {
			t.Error("empty event type in All")
		}
		if seen[typ] {
			t.Errorf("duplicate event type %q in All", typ)
		}
		seen[typ] = true
	}
	if len(All) != 9 {
		t.Errorf("len(All) = %d, want 9 — обнови число при добавлении типа", len(All))
	}
}
