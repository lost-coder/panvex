// settingsgen writes schema.json + reference.md + example.config.toml.
// Run via `go run ./cmd/control-plane/gensettings` or `make gen-settings`.
//
// Placed in a dedicated sub-package (not cmd/control-plane/) to avoid needing
// //go:build !settingsgen on every existing file in the main package.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/lost-coder/panvex/internal/controlplane/settings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gen-settings: %v\n", err)
		os.Exit(1)
	}
}

// repoRoot returns the repository root by walking up from this source file.
// This works regardless of the working directory (go generate, make, or direct invocation).
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	// file = .../cmd/control-plane/gensettings/main.go → go up 3 levels
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func run() error {
	root := repoRoot()
	schema, err := settings.RenderSchemaJSON()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "internal/controlplane/settings/gen/schema.json"), schema, 0o644); err != nil {
		return err
	}
	ref, err := settings.RenderReferenceMarkdown()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "docs/settings/reference.md"), ref, 0o644); err != nil {
		return err
	}
	tomlBody, err := settings.RenderExampleConfigTOML()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, "docs/settings/example.config.toml"), tomlBody, 0o644); err != nil {
		return err
	}
	return nil
}
