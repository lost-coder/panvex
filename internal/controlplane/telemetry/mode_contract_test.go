package telemetry

import (
	"os"
	"reflect"
	"regexp"
	"testing"
)

// TestModeKindStringsMatchFrontend locks the Go ModeKind.String() outputs
// against the TS literal union in web/src/shared/api/types-pages/common.ts.
// Run from the package directory; the relative path resolves to the repo's
// web/ tree.
func TestModeKindStringsMatchFrontend(t *testing.T) {
	expected := []string{"me", "direct", "fallback", "me_down"}
	actual := []string{
		ModeME.String(),
		ModeDirect.String(),
		ModeFallback.String(),
		ModeMeDown.String(),
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("ModeKind.String() drift: got %v, want %v", actual, expected)
	}

	tsValues := readTSUnion(t, "ModeKind")
	if !reflect.DeepEqual(tsValues, expected) {
		t.Fatalf("TS ModeKind literals drifted: got %v, want %v", tsValues, expected)
	}
}

// TestSeverityVocabularyMatchesFrontend pins the severity strings the Go
// projector returns (SeverityAndReason) against the TS Severity union. The
// Go-side vocabulary is "ok"|"warn"|"critical"|"bad"; the TS union widens
// only to accept legacy "good" historically — current contract is the same
// four values.
func TestSeverityVocabularyMatchesFrontend(t *testing.T) {
	expected := []string{"ok", "warn", "critical", "bad"}
	tsValues := readTSUnion(t, "Severity")
	if !reflect.DeepEqual(tsValues, expected) {
		t.Fatalf("TS Severity literals drifted: got %v, want %v", tsValues, expected)
	}
}

// readTSUnion extracts the string-literal members of a TS type alias of the
// form `export type <name> = "a" | "b" | "c";` from common.ts. Quotes any
// regex pattern, so callers can pass literal alias names.
func readTSUnion(t *testing.T, name string) []string {
	t.Helper()
	const tsPath = "../../../web/src/shared/api/types-pages/common.ts"
	src, err := os.ReadFile(tsPath)
	if err != nil {
		t.Fatalf("read %s: %v", tsPath, err)
	}
	re := regexp.MustCompile(`export\s+type\s+` + regexp.QuoteMeta(name) + `\s*=\s*([^;]+);`)
	m := re.FindSubmatch(src)
	if len(m) != 2 {
		t.Fatalf("could not locate %s union in %s", name, tsPath)
	}
	literalRe := regexp.MustCompile(`"([^"]+)"`)
	matches := literalRe.FindAllSubmatch(m[1], -1)
	values := make([]string, 0, len(matches))
	for _, lit := range matches {
		values = append(values, string(lit[1]))
	}
	return values
}
