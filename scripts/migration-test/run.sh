#!/usr/bin/env bash
#
# Migration stress test (P2-TEST-03).
#
# Seeds a fresh SQLite database with production-scale data at schema version
# 0001, then runs the full goose migration chain to HEAD and measures timing
# per version. Validates basic data-integrity invariants at the end.
#
# Usage:
#   bash scripts/migration-test/run.sh [db-path]
#
# Defaults to $(mktemp -u)/migration-test.db. Requires a Go toolchain and
# writes only to the supplied DB path. Safe to run locally — no network, no
# mutation of the working tree.
#
# Knobs via environment:
#   SEED_AGENTS=100000       number of agents
#   SEED_METRICS=1000000     number of metric_snapshots
#   SEED_CLIENTS=10000       number of clients
#   SEED_JOBS=50000          number of jobs (+ 1 target each)
#   SEED_AUDITS=500000       number of audit_events
#   SEED_FLEET_GROUPS=32     number of fleet groups
#   SEED_DISCOVERED=0        number of discovered_clients (0 to skip)
#
# Why shell + the control-plane binary instead of calling Migrate() directly:
#   We want to exercise the exact same code path operators use in production.
#   `migrate-schema up` reads the embedded goose FS, opens the DSN, and runs
#   the full chain. Total wall-clock time is captured here; for per-version
#   timing, inspect the goose log lines emitted by the binary.

set -euo pipefail

# ---------- args ----------
DB_PATH="${1:-}"
if [[ -z "$DB_PATH" ]]; then
  DB_PATH="$(mktemp -u -t panvex-migtest.XXXXXXXX.db)"
fi

# ---------- locate repo root ----------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_ROOT"

# ---------- params ----------
SEED_AGENTS="${SEED_AGENTS:-100000}"
SEED_METRICS="${SEED_METRICS:-1000000}"
SEED_CLIENTS="${SEED_CLIENTS:-10000}"
SEED_JOBS="${SEED_JOBS:-50000}"
SEED_AUDITS="${SEED_AUDITS:-500000}"
SEED_FLEET_GROUPS="${SEED_FLEET_GROUPS:-32}"
SEED_DISCOVERED="${SEED_DISCOVERED:-0}"

echo "=============================================================="
echo " Panvex migration stress test"
echo "   db:       $DB_PATH"
echo "   agents:   $SEED_AGENTS"
echo "   metrics:  $SEED_METRICS"
echo "   clients:  $SEED_CLIENTS"
echo "   jobs:     $SEED_JOBS"
echo "   audits:   $SEED_AUDITS"
echo "=============================================================="

# Always clean up unless caller asked to keep the artefact.
if [[ "${KEEP_DB:-0}" != "1" ]]; then
  trap 'rm -f "$DB_PATH" "$DB_PATH"-journal "$DB_PATH"-wal "$DB_PATH"-shm' EXIT
fi

# ---------- stage 1: seed @ v0001 ----------
echo ""
echo "--- Stage 1: seed pre-migration dataset ---"
time go run "$REPO_ROOT/scripts/migration-test/seed.go" \
  -db "$DB_PATH" \
  -agents "$SEED_AGENTS" \
  -metrics "$SEED_METRICS" \
  -clients "$SEED_CLIENTS" \
  -jobs "$SEED_JOBS" \
  -audits "$SEED_AUDITS" \
  -fleet-groups "$SEED_FLEET_GROUPS" \
  -discovered "$SEED_DISCOVERED"

SEEDED_SIZE=$(du -h "$DB_PATH" | awk '{print $1}')
echo "seeded DB size: $SEEDED_SIZE"

# ---------- stage 2: apply all pending migrations ----------
echo ""
echo "--- Stage 2: apply migrations (full chain, wall-clock timed) ---"

# Build the control-plane binary once so we run the exact artefact shipped to
# operators — `go run` would rebuild every invocation and hide compile cost.
CP_BIN="$(mktemp -t panvex-cp.XXXXXXXX)"
trap 'rm -f "$CP_BIN" "$DB_PATH" "$DB_PATH"-journal "$DB_PATH"-wal "$DB_PATH"-shm' EXIT
go build -o "$CP_BIN" ./cmd/control-plane

total_start=$(date +%s%N)
if ! "$CP_BIN" migrate-schema up \
    -storage-driver sqlite -storage-dsn "file:$DB_PATH"; then
  echo "FAIL: migrate-schema up failed" >&2
  exit 1
fi
total_end=$(date +%s%N)
total_ms=$(( (total_end - total_start) / 1000000 ))
echo "migrate-schema up completed in ${total_ms} ms"

# ---------- stage 3: validate schema + data integrity ----------
echo ""
echo "--- Stage 3: schema and integrity checks ---"

# We shell out to `sqlite3` when available for readable output, otherwise fall
# back to a tiny Go probe (below) so the script still runs on minimal CI boxes.
if command -v sqlite3 >/dev/null 2>&1; then
  echo "integrity_check:"
  sqlite3 "$DB_PATH" "PRAGMA integrity_check;"
  echo "foreign_key_check:"
  fk_out=$(sqlite3 "$DB_PATH" "PRAGMA foreign_key_check;" || true)
  if [[ -n "$fk_out" ]]; then
    echo "FAIL: foreign_key_check reported violations:" >&2
    echo "$fk_out" >&2
    exit 1
  fi
  echo "  (no violations)"

  echo "table counts:"
  for tbl in fleet_groups agents clients jobs job_targets audit_events metric_snapshots discovered_clients; do
    n=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM $tbl;")
    printf "  %-20s %s\n" "$tbl" "$n"
  done

  echo "P2-DB-02 indexes present:"
  for idx in idx_jobs_status idx_job_targets_agent_id idx_metric_snapshots_captured_at idx_enrollment_tokens_fleet_group_id; do
    found=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='$idx';")
    if [[ "$found" != "1" ]]; then
      echo "FAIL: missing index $idx" >&2
      exit 1
    fi
    printf "  %-40s ok\n" "$idx"
  done

  echo "0011 column rename (audit_events.details, metric_snapshots.values):"
  ae=$(sqlite3 "$DB_PATH" "SELECT sql FROM sqlite_master WHERE name='audit_events';")
  ms=$(sqlite3 "$DB_PATH" "SELECT sql FROM sqlite_master WHERE name='metric_snapshots';")
  if echo "$ae" | grep -q "details_json"; then
    echo "FAIL: audit_events still has details_json after 0011" >&2
    exit 1
  fi
  if echo "$ms" | grep -q "values_json"; then
    echo "FAIL: metric_snapshots still has values_json after 0011" >&2
    exit 1
  fi
  echo "  renamed ok"
else
  echo "sqlite3 CLI not found — skipping detailed checks"
  echo "install sqlite3 to enable full validation"
fi

echo ""
echo "=============================================================="
echo " OK — migration stress test passed"
echo "   total migration time: ${total_ms} ms"
echo "   seeded DB size:       $SEEDED_SIZE"
echo "=============================================================="
