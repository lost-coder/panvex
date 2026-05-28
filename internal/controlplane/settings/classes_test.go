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

func TestApplyTierValid(t *testing.T) {
	for _, tier := range []ApplyTier{ApplyLive, ApplyRestart, ApplyConfig} {
		if !tier.Valid() {
			t.Errorf("%q.Valid() = false, want true", tier)
		}
	}
	if ApplyTier("bogus").Valid() {
		t.Error(`ApplyTier("bogus").Valid() = true, want false`)
	}
}

func TestTypeConstants(t *testing.T) {
	got := []Type{TypeInt, TypeDuration, TypeString, TypeBool, TypeHostPort, TypeURL, TypeEnum, TypeJSON}
	seen := map[Type]bool{}
	for _, ty := range got {
		if seen[ty] {
			t.Fatalf("duplicate type %q", ty)
		}
		seen[ty] = true
	}
}
