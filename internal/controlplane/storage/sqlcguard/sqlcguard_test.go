// Package sqlcguard_test enforces the rule that comes with the "third path":
// the domains that bypass storage.Store and speak sqlc directly (enrollment,
// bootstrap, agenttransport, settings) must only use queries that actually run
// on SQLite.
//
// The sqlc code is generated from the POSTGRES schema. Nothing stops a query
// from using Postgres-only syntax, and nothing at compile time notices — it
// fails at runtime, on SQLite installs only. That is not hypothetical: an
// enrollment query generated with `narg()::uuid` casts was unparseable by
// modernc/sqlite and broke enrollment on every SQLite deployment.
//
// This test walks the domain packages, collects every dbsqlc query they call,
// and PREPAREs each one against a freshly migrated SQLite database. A query
// that cannot even be prepared there fails the build.
package sqlcguard_test

import (
	"context"
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// domainPackages are the packages allowed to use dbsqlc directly. Adding one
// here is a deliberate architectural decision — see src/CLAUDE.md.
var domainPackages = []string{
	"../../enrollment",
	"../../bootstrap",
	"../../agenttransport",
	"../../settings",
}

// dbsqlcDir holds the generated code the queries are read from.
const dbsqlcDir = "../../../dbsqlc"

func TestDomainSQLCQueriesPrepareOnSQLite(t *testing.T) {
	used := collectUsedQueries(t)
	if len(used) == 0 {
		t.Fatal("found no dbsqlc calls in the domain packages; the scanner is broken, not the code")
	}

	t.Logf("domain packages use %d distinct dbsqlc queries", len(used))

	sqlByMethod := parseGeneratedSQL(t)

	db := migratedSQLiteDB(t)

	for method := range used {
		query, ok := sqlByMethod[method]
		if !ok {
			// A method we could not map back to its SQL const. Fail loudly
			// rather than skip: a silent skip is how this guard rots.
			t.Errorf("dbsqlc.%s is called by a domain package but no SQL const was found for it", method)
			continue
		}
		stmt, err := db.PrepareContext(context.Background(), query)
		if err != nil {
			t.Errorf("dbsqlc.%s does not prepare on SQLite: %v\n\nSQL:\n%s", method, err, query)
			continue
		}
		defer stmt.Close() //nolint:errcheck,gocritic // reason: prepared only to prove the SQL parses; deferred inside the loop is fine for a bounded query set.
	}
}

// collectUsedQueries parses the domain packages and returns every method name
// invoked on a *dbsqlc.Queries value. It keys off the receiver's name rather
// than full type resolution: the generated methods are unique enough that a
// call like `q.CreateEnrollmentAttempt(ctx, ...)` on anything else would be a
// false positive we want to see anyway.
func collectUsedQueries(t *testing.T) map[string]struct{} {
	t.Helper()
	used := map[string]struct{}{}
	generated := generatedMethodNames(t)

	for _, dir := range domainPackages {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if _, isGenerated := generated[sel.Sel.Name]; isGenerated {
					used[sel.Sel.Name] = struct{}{}
				}
				return true
			})
		}
	}
	return used
}

var (
	methodRe = regexp.MustCompile(`(?m)^func \(q \*Queries\) ([A-Za-z0-9_]+)\(`)
	constRe  = regexp.MustCompile("(?ms)^const ([a-zA-Z0-9_]+) = `(.*?)`$")
)

func generatedMethodNames(t *testing.T) map[string]struct{} {
	t.Helper()
	names := map[string]struct{}{}
	for _, src := range generatedSources(t) {
		for _, m := range methodRe.FindAllStringSubmatch(src, -1) {
			names[m[1]] = struct{}{}
		}
	}
	return names
}

// parseGeneratedSQL maps a generated method name to its SQL text. sqlc names
// the const after the method with a lowercase first letter.
func parseGeneratedSQL(t *testing.T) map[string]string {
	t.Helper()
	byConst := map[string]string{}
	for _, src := range generatedSources(t) {
		for _, m := range constRe.FindAllStringSubmatch(src, -1) {
			byConst[m[1]] = m[2]
		}
	}

	byMethod := map[string]string{}
	for name, query := range byConst {
		byMethod[strings.ToUpper(name[:1])+name[1:]] = query
	}
	return byMethod
}

func generatedSources(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(dbsqlcDir)
	if err != nil {
		t.Fatalf("read %s: %v", dbsqlcDir, err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dbsqlcDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		out = append(out, string(data))
	}
	return out
}

// migratedSQLiteDB opens a SQLite store (which runs the migrations) and returns
// the underlying *sql.DB so queries can be prepared against the real schema.
func migratedSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sqlcguard.db")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store.DB()
}
