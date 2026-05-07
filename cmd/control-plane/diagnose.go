package main

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/postgres"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/shirou/gopsutil/v4/host"
)

// runDiagnose collects a one-shot health snapshot of the control-plane
// store and emits it as a copy-pasteable Markdown table. Operators run
// this with the panel down (or against an external store) when they need
// to attach the panel state to a support ticket. No HTTP traffic; the
// command opens the configured store directly and reads from it.
func runDiagnose(args []string) error {
	flags := flag.NewFlagSet("diagnose", flag.ContinueOnError)
	storageDriver := flags.String(flagStorageDriver, "", helpStorageDriver)
	storageDSN := flags.String(flagStorageDSN, "", helpStorageDSN)
	output := flags.String("out", "", "Optional path to write the report (default: stdout)")
	flags.Usage = func() {
		fmt.Fprintf(flags.Output(), "Usage: panvex-control-plane diagnose [flags]\n\n")
		fmt.Fprintf(flags.Output(), "Collects a one-shot health snapshot suitable for support tickets.\n")
		fmt.Fprintf(flags.Output(), "Output is a Markdown table on stdout (or -out <path>).\n\n")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	storageConfig, err := config.ResolveStorage(*storageDriver, *storageDSN)
	if err != nil {
		return err
	}

	report, err := collectDiagnostics(ctx, storageConfig)
	if err != nil {
		return err
	}

	out := io.Writer(os.Stdout)
	if *output != "" {
		f, err := os.OpenFile(*output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("open -out: %w", err)
		}
		defer f.Close()
		out = f
	}
	_, err = io.WriteString(out, report)
	return err
}

// diagnosticRow is one (key, value) pair in the rendered Markdown table.
type diagnosticRow struct {
	Key   string
	Value string
}

// ErrDiagnoseSoftFailures is returned by collectDiagnostics when the
// report rendered but one or more rows reflect a real problem
// (currently: DB Ping failed). Callers in CI / monitoring pipelines
// (`panvex-control-plane diagnose && echo OK`) treat any error as
// "panel is unhealthy" while the human-readable report still gets
// printed for the operator's eyes.
var ErrDiagnoseSoftFailures = errors.New("diagnose: one or more soft failures (see report)")

// collectDiagnostics opens the configured store and renders the report.
// Errors that block the whole snapshot (DSN parse, store open) bubble
// up as fatal. Soft failures (one row that we couldn't compute) are
// reflected inline with an "error: <msg>" cell so the rest of the
// table still renders, AND the function returns ErrDiagnoseSoftFailures
// so an automated `... && echo OK` chain doesn't false-pass.
func collectDiagnostics(ctx context.Context, storageConfig config.StorageConfig) (string, error) {
	rows := []diagnosticRow{
		{"Walltime", time.Now().UTC().Format(time.RFC3339)},
		{"Panel version", Version},
		{"Commit SHA", CommitSHA},
		{"Build time", BuildTime},
		{"Go runtime", runtime.Version()},
		{"GOOS/GOARCH", runtime.GOOS + "/" + runtime.GOARCH},
		{"Storage driver", storageConfig.Driver},
		{"Storage DSN", maskDSN(storageConfig.DSN)},
	}

	store, err := openStore(storageConfig)
	if err != nil {
		return "", fmt.Errorf("open store: %w", err)
	}
	defer store.Close()

	var softFailure bool

	pingStart := time.Now()
	pingErr := store.Ping(ctx)
	pingLatency := time.Since(pingStart)
	if pingErr != nil {
		rows = append(rows, diagnosticRow{"Ping", fmt.Sprintf("error: %v", pingErr)})
		softFailure = true
	} else {
		rows = append(rows, diagnosticRow{"Ping latency", pingLatency.Round(time.Microsecond).String()})
	}

	rows = append(rows, diagnosticRow{"Schema version", schemaVersionString(ctx, storageConfig)})

	rows = append(rows, collectRowCounts(ctx, store)...)
	rows = append(rows, certExpiryRow(ctx, store))
	rows = append(rows, encryptionFingerprintRow())
	rows = append(rows, poolStatsRows(store)...)
	rows = append(rows, systemUptimeRow())

	report := renderDiagnosticTable(rows)
	if softFailure {
		return report, ErrDiagnoseSoftFailures
	}
	return report, nil
}

// renderDiagnosticTable renders the rows as a tight two-column Markdown
// table. Operators paste this into a GitHub issue verbatim.
func renderDiagnosticTable(rows []diagnosticRow) string {
	var b strings.Builder
	b.WriteString("# Panvex control-plane diagnose\n\n")
	b.WriteString("| Field | Value |\n")
	b.WriteString("| --- | --- |\n")
	for _, r := range rows {
		// Escape pipes inside cell values so a DSN with a literal `|`
		// does not break the Markdown table layout.
		v := strings.ReplaceAll(r.Value, "|", `\|`)
		fmt.Fprintf(&b, "| %s | %s |\n", r.Key, v)
	}
	return b.String()
}

// maskDSN returns a redacted form of the DSN that hides any embedded
// userinfo password. SQLite paths typically have no userinfo so they
// pass through unchanged.
func maskDSN(dsn string) string {
	if dsn == "" {
		return "(empty)"
	}
	if parsed, err := url.Parse(dsn); err == nil && parsed.Scheme != "" {
		return parsed.Redacted()
	}
	return dsn
}

// collectRowCounts returns the row-count diagnostic rows. Failures per
// table are turned into "error: ..." cells so a single broken count
// does not blank the rest of the report.
func collectRowCounts(ctx context.Context, store storage.Store) []diagnosticRow {
	rows := []diagnosticRow{}

	if agents, err := store.ListAgents(ctx); err != nil {
		rows = append(rows, diagnosticRow{"Agents", fmt.Sprintf("error: %v", err)})
	} else {
		rows = append(rows, diagnosticRow{"Agents", fmt.Sprintf("%d", len(agents))})
	}

	if clients, err := store.ListClients(ctx); err != nil {
		rows = append(rows, diagnosticRow{"Clients", fmt.Sprintf("error: %v", err)})
	} else {
		rows = append(rows, diagnosticRow{"Clients", fmt.Sprintf("%d", len(clients))})
	}

	if groups, err := store.ListFleetGroups(ctx); err != nil {
		rows = append(rows, diagnosticRow{"Fleet groups", fmt.Sprintf("error: %v", err)})
	} else {
		rows = append(rows, diagnosticRow{"Fleet groups", fmt.Sprintf("%d", len(groups))})
	}

	if users, err := store.ListUsers(ctx); err != nil {
		rows = append(rows, diagnosticRow{"Users", fmt.Sprintf("error: %v", err)})
	} else {
		rows = append(rows, diagnosticRow{"Users", fmt.Sprintf("%d", len(users))})
	}

	if jobs, err := store.ListJobs(ctx); err != nil {
		rows = append(rows, diagnosticRow{"Jobs (total)", fmt.Sprintf("error: %v", err)})
	} else {
		byStatus := map[string]int{}
		for _, j := range jobs {
			byStatus[j.Status]++
		}
		rows = append(rows, diagnosticRow{"Jobs (total)", fmt.Sprintf("%d", len(jobs))})
		rows = append(rows, diagnosticRow{"Jobs by status", formatStatusMap(byStatus)})
	}

	since := time.Now().Add(-24 * time.Hour)
	// ListAuditEvents returns up to `limit` rows ordered ascending; the
	// row count we need is "audit events with created_at >= now-24h",
	// so over-fetch and filter in memory. The storage layer caps at
	// 1024 rows internally — sufficient for a sanity-check snapshot.
	if events, err := store.ListAuditEvents(ctx, 1024); err != nil {
		rows = append(rows, diagnosticRow{"Audit events (24h)", fmt.Sprintf("error: %v", err)})
	} else {
		count := 0
		for _, e := range events {
			if !e.CreatedAt.Before(since) {
				count++
			}
		}
		rows = append(rows, diagnosticRow{"Audit events (24h)", fmt.Sprintf("%d", count)})
	}

	return rows
}

// formatStatusMap renders a status->count map deterministically as
// `pending=3 running=1 succeeded=42`. Keys are sorted alphabetically
// so two runs against the same DB produce identical output (operators
// diff diagnose reports across deploys).
func formatStatusMap(m map[string]int) string {
	if len(m) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Tiny set; selection sort is overkill but keeps the code allocation-free.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

// certExpiryRow computes the days remaining on the panel root CA's
// not-after timestamp. A missing CA row is reported as "(no CA)" so
// the operator notices uninitialized state instead of seeing a
// hard error.
func certExpiryRow(ctx context.Context, store storage.Store) diagnosticRow {
	authority, err := store.GetCertificateAuthority(ctx)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return diagnosticRow{"Panel CA", "(no CA initialised)"}
		}
		return diagnosticRow{"Panel CA", fmt.Sprintf("error: %v", err)}
	}
	block, _ := pem.Decode([]byte(authority.CAPEM))
	if block == nil {
		return diagnosticRow{"Panel CA", "error: CA PEM decode failed"}
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return diagnosticRow{"Panel CA", fmt.Sprintf("error: parse cert: %v", err)}
	}
	remaining := time.Until(cert.NotAfter)
	days := int(remaining.Hours() / 24)
	return diagnosticRow{
		"Panel CA expiry",
		fmt.Sprintf("%s (%d days remaining)", cert.NotAfter.UTC().Format(time.RFC3339), days),
	}
}

// encryptionFingerprintRow returns the first 8 hex chars of the SHA256
// hash of PANVEX_ENCRYPTION_KEY. The fingerprint is one-way — operators
// cross-check that two panels share the same key without ever leaking
// the key itself.
func encryptionFingerprintRow() diagnosticRow {
	key := strings.TrimSpace(os.Getenv("PANVEX_ENCRYPTION_KEY"))
	if key == "" {
		return diagnosticRow{"Encryption key fingerprint", "(unset)"}
	}
	sum := sha256.Sum256([]byte(key))
	return diagnosticRow{"Encryption key fingerprint", hex.EncodeToString(sum[:])[:8]}
}

// poolStatsAware is the contract Diagnose needs to surface DB pool wait
// metrics. Both *sqlite.Store and *postgres.Store implement it; the
// storage.Store interface itself does not (it stays minimal).
type poolStatsAware interface {
	PoolStats() sql.DBStats
}

// poolStatsRows extracts a couple of high-signal pool metrics. The
// "WaitCount" / "WaitDuration" pair is the canonical signal of pool
// exhaustion: non-zero values mean callers are queuing for a free
// connection.
func poolStatsRows(store storage.Store) []diagnosticRow {
	psa, ok := store.(poolStatsAware)
	if !ok {
		return []diagnosticRow{{"DB pool stats", "(driver does not expose stats)"}}
	}
	stats := psa.PoolStats()
	return []diagnosticRow{
		{"DB pool open", fmt.Sprintf("%d (in-use=%d idle=%d)", stats.OpenConnections, stats.InUse, stats.Idle)},
		{"DB pool wait", fmt.Sprintf("count=%d total=%s", stats.WaitCount, stats.WaitDuration.Round(time.Millisecond))},
	}
}

// systemUptimeRow reports the host uptime via gopsutil. Falls back to
// "(unavailable)" on platforms where the call errors out (e.g.
// stripped-down container without /proc/uptime).
func systemUptimeRow() diagnosticRow {
	uptime, err := host.Uptime()
	if err != nil {
		return diagnosticRow{"System uptime", fmt.Sprintf("error: %v", err)}
	}
	return diagnosticRow{"System uptime", (time.Duration(uptime) * time.Second).String()}
}

// schemaVersionString returns the highest applied goose version on the
// configured store. The goose ledger is `goose_db_version` on both
// SQLite and Postgres.
func schemaVersionString(ctx context.Context, storageConfig config.StorageConfig) string {
	switch storageConfig.Driver {
	case config.StorageDriverSQLite:
		db, err := sql.Open("sqlite", storageConfig.DSN)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		defer db.Close()
		return queryGooseVersion(ctx, db)
	case config.StorageDriverPostgres:
		db, err := sql.Open("pgx", storageConfig.DSN)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		defer db.Close()
		return queryGooseVersion(ctx, db)
	default:
		return "(unsupported driver)"
	}
}

// queryGooseVersion reads MAX(version_id) from the goose ledger.
// The cap is the highest applied schema version — this is what
// operators actually care about when comparing two deployments.
func queryGooseVersion(ctx context.Context, db *sql.DB) string {
	var v sql.NullInt64
	err := db.QueryRowContext(ctx, "SELECT MAX(version_id) FROM goose_db_version WHERE is_applied = 1").Scan(&v)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	if !v.Valid {
		return "(no migrations applied)"
	}
	return fmt.Sprintf("%d", v.Int64)
}

// Compile-time guards that the concrete Store types still satisfy
// the poolStatsAware contract; if either drops PoolStats() the build
// breaks before diagnose silently degrades.
var (
	_ poolStatsAware = (*sqlite.Store)(nil)
	_ poolStatsAware = (*postgres.Store)(nil)
)
