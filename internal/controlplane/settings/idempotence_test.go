package settings

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// repoRootFromTestFile returns the absolute path to src/ from this test file.
func repoRootFromTestFile(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	return root
}

func TestIdempotence_SchemaJSON(t *testing.T) {
	root := repoRootFromTestFile(t)
	want, err := os.ReadFile(filepath.Join(root, "internal/controlplane/settings/gen/schema.json"))
	if err != nil {
		t.Fatalf("read committed schema.json: %v", err)
	}
	got, err := RenderSchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimRight(want, "\n"), bytes.TrimRight(got, "\n")) {
		t.Fatal("schema.json drifted from registry; run `make gen-settings`")
	}
}

func TestIdempotence_ReferenceMarkdown(t *testing.T) {
	root := repoRootFromTestFile(t)
	want, err := os.ReadFile(filepath.Join(root, "docs/settings/reference.md"))
	if err != nil {
		t.Fatalf("read reference.md: %v", err)
	}
	got, err := RenderReferenceMarkdown()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimRight(want, "\n"), bytes.TrimRight(got, "\n")) {
		t.Fatal("docs/settings/reference.md drifted; run `make gen-settings`")
	}
}

func TestIdempotence_ExampleConfigTOML(t *testing.T) {
	root := repoRootFromTestFile(t)
	want, err := os.ReadFile(filepath.Join(root, "docs/settings/example.config.toml"))
	if err != nil {
		t.Fatalf("read example.config.toml: %v", err)
	}
	got, err := RenderExampleConfigTOML()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimRight(want, "\n"), bytes.TrimRight(got, "\n")) {
		t.Fatal("docs/settings/example.config.toml drifted; run `make gen-settings`")
	}
}
