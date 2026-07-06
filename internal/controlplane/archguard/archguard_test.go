// Package archguard contains architecture guard tests for the
// control-plane layering rules (P8.2). See src/CLAUDE.md, section
// "Слои control-plane".
package archguard

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePrefix = "github.com/lost-coder/panvex/"

// serverFreePackages must never import internal/controlplane/server. These are
// the domain/leaf packages the HTTP layer depends ON; the dependency edge only
// ever points server -> domain. A domain package that needs something from
// server takes it via an interface defined on the domain side (the pattern:
// gateway.Deps). Extend this list whenever a new domain package is extracted.
var serverFreePackages = []string{
	"internal/controlplane/agents",
	"internal/controlplane/api",
	"internal/controlplane/batchwriter",
	"internal/controlplane/clients",
	"internal/controlplane/configtargets",
	"internal/controlplane/fleet",
	"internal/controlplane/gateway",
	"internal/controlplane/history",
	"internal/controlplane/jobs",
	"internal/controlplane/metrics",
	"internal/controlplane/updates",
	"internal/controlplane/webhooks",
}

// storeAccessAllowlist is a RATCHET: the server files that still reach
// s.store directly. The domain-service PRs (P8.2f-j) delete their file from
// this list as each handler group moves behind a domain service. Adding a NEW
// entry is an architecture violation — new handlers go through a domain
// service (src/CLAUDE.md, "Слои control-plane"). The list only ever shrinks.
//
// Seeded with:
//
//	cd internal/controlplane/server && grep -l 's\.store\.' $(ls *.go | grep -v _test)
var storeAccessAllowlist = map[string]bool{
	"agent_flow.go":              true,
	"audit_trail.go":             true,
	"authority.go":               true,
	"config_apply_batches.go":    true,
	"fleet_scope.go":             true,
	"gateway_deps.go":            true,
	"geoip_settings.go":          true,
	"http_agent_transport.go":    true,
	"http_agents.go":             true,
	"http_clients_helpers.go":    true,
	"http_config_apply.go":       true,
	"http_enrollment.go":         true,
	"http_fleet_groups.go":       true,
	"http_health.go":             true,
	"http_history.go":            true,
	"http_inventory.go":          true,
	"http_jobs.go":               true,
	"http_provision_outbound.go": true,
	"http_recovery.go":           true,
	"http_retention.go":          true,
	"http_telemetry.go":          true,
	"lifecycle.go":               true,
	"metrics_poller.go":          true,
	"panel_settings.go":          true,
	"state_restore.go":           true,
	"subscription_viewmodel.go":  true,
	"telemetry_runtime.go":       true,
	"timeseries_rollup.go":       true,
	"user_appearance.go":         true,
}

// repoRoot walks up from the test's CWD until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test dir")
		}
		dir = parent
	}
}

// TestDomainPackagesDoNotImportServer enforces the layering direction:
// server -> domain packages, never the reverse. A domain package that
// needs something from server must take it via an interface defined on
// the domain side (see gateway.Deps for the pattern).
func TestDomainPackagesDoNotImportServer(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fset := token.NewFileSet()
	for _, pkg := range serverFreePackages {
		dir := filepath.Join(root, filepath.FromSlash(pkg))
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue // package not extracted yet — later P8.2 PRs add it
		}
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, imp := range f.Imports {
				val, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatalf("unquote import in %s: %v", path, err)
				}
				if val == modulePrefix+"internal/controlplane/server" ||
					strings.HasPrefix(val, modulePrefix+"internal/controlplane/server/") {
					t.Errorf("%s/%s imports %s — domain packages must not depend on the HTTP layer", pkg, e.Name(), val)
				}
			}
		}
	}
}

// TestServerFilesDoNotAccessStoreDirectly is the "handlers -> service,
// not store" ratchet. It walks every non-test file in the server
// package and fails when a file OUTSIDE the allowlist reads or writes
// data through an `s.store.<Method>` call. The allowlist only ever
// shrinks.
//
// It matches `s.store.<X>` specifically — a data access on the store —
// not a bare `s.store` reference. `if s.store == nil` guards and passing
// `s.store` to a constructor (dependency wiring) are legitimate and do
// NOT count, matching the seed grep `s\.store\.`.
func TestServerFilesDoNotAccessStoreDirectly(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dir := filepath.Join(root, "internal", "controlplane", "server")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			// Match `s.store.<X>`: an outer selector whose base is the
			// `s.store` selector. That is a method/field access ON the
			// store — the direct data access we forbid.
			outer, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			inner, ok := outer.X.(*ast.SelectorExpr)
			if !ok || inner.Sel.Name != "store" {
				return true
			}
			recv, ok := inner.X.(*ast.Ident)
			if !ok || recv.Name != "s" {
				return true
			}
			seen[name] = true
			if !storeAccessAllowlist[name] {
				pos := fset.Position(inner.Pos())
				t.Errorf("%s: direct s.store access at %s — route through a domain service (internal/controlplane/<domain>); see src/CLAUDE.md", name, pos)
			}
			return true
		})
	}
	// Ratchet hygiene: entries whose files no longer touch s.store must
	// be deleted, so the allowlist cannot silently re-grow.
	for name := range storeAccessAllowlist {
		if !seen[name] {
			t.Errorf("allowlist entry %q is stale (file gone or clean) — remove it", name)
		}
	}
}
