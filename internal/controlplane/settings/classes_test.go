package settings

import "testing"

func TestClassConstants(t *testing.T) {
	if ClassBootstrap == ClassOperational {
		t.Fatal("class constants must differ")
	}
}

func TestSourceConstants(t *testing.T) {
	got := []Source{SourceEnv, SourceConfigFile, SourceDB, SourceDefault}
	seen := map[Source]bool{}
	for _, s := range got {
		if seen[s] {
			t.Fatalf("duplicate source %q", s)
		}
		seen[s] = true
	}
}
