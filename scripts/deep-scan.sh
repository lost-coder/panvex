#!/bin/bash
# Tier-3: deep local analysis, run manually (`make deep-scan`).
#
# The 2026-08-07 CI simplification split checks into three tiers:
#   1. pre-push  — fast gate (lint, unit, build)
#   2. CI        — path-gated pipeline, 9 required checks, Sonar as SAST
#   3. THIS      — heavy/free tools that spend developer-machine time
#                  instead of runner minutes, see the whole tree and the
#                  whole git history instead of a PR diff
#
# Every section is tolerant: a missing tool is reported and skipped, a
# finding marks the run failed but does not stop later sections. Exit
# code 1 if any section failed.
#
# Tools (all free):
#   semgrep      pip venv    ~/.local/bin/semgrep      SAST Go+TS (OWASP)
#   osv-scanner  go install  ~/go/bin/osv-scanner      CVE: go.mod + npm lock
#   govulncheck  go install  ~/go/bin/govulncheck      Go CVE w/ reachability
#   gitleaks     release bin ~/go/bin/gitleaks         secrets, FULL history
#   npm audit    bundled     —                         npm advisories
set -u
cd "$(dirname "$0")/.." || exit 1

PATH="$HOME/go/bin:$HOME/.local/bin:$PATH"
declare -A RESULT
FAILED=0

run_section() {
    local name="$1" tool="$2"; shift 2
    echo
    echo "═══ deep-scan: $name ═══"
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "--- $tool not installed, skipping (see header for install source)"
        RESULT[$name]="SKIPPED (no $tool)"
        return
    fi
    if "$@"; then
        RESULT[$name]="OK"
    else
        RESULT[$name]="FAILED"
        FAILED=1
    fi
}

# ── SAST: semgrep over Go + TS with community security rules ─────────────
# p/golang + p/typescript are language correctness/security packs;
# p/security-audit adds cross-language OWASP-style checks.
#
# Rules are served from a LOCAL cache and the scan runs offline. Letting
# semgrep resolve p/... configs itself stalls for many minutes when its
# API endpoint is slow (observed 2026-08-08: the CDN answered curl in
# 0.2 s while semgrep's own client timed out repeatedly), so the cache
# is refreshed via curl with a hard 30 s cap and the scan never touches
# the network. Re-run refresh manually to pick up new rules.
SEMGREP_CACHE="$HOME/.cache/semgrep-rules"
refresh_semgrep_rules() {
    mkdir -p "$SEMGREP_CACHE"
    local p
    for p in golang typescript security-audit; do
        curl -sSfL --max-time 30 "https://semgrep.dev/c/p/$p" \
            -o "$SEMGREP_CACHE/$p.yml.tmp" \
            && mv "$SEMGREP_CACHE/$p.yml.tmp" "$SEMGREP_CACHE/$p.yml" \
            || echo "--- refresh of p/$p failed, keeping cached copy (if any)"
    done
}
semgrep_scan() {
    # Refresh only when a pack is missing entirely; otherwise scan from
    # cache (refresh by hand: rm ~/.cache/semgrep-rules/*.yml).
    [ -s "$SEMGREP_CACHE/golang.yml" ] || refresh_semgrep_rules
    if ! [ -s "$SEMGREP_CACHE/golang.yml" ]; then
        echo "--- no cached rules and semgrep.dev unreachable"
        return 2
    fi
    semgrep scan --quiet --metrics=off --json \
        --config "$SEMGREP_CACHE/golang.yml" \
        --config "$SEMGREP_CACHE/typescript.yml" \
        --config "$SEMGREP_CACHE/security-audit.yml" \
        --exclude 'web/node_modules' --exclude '*.gen.go' --exclude '*.gen.ts' \
        --exclude '*.pb.go' \
        --exclude 'internal/dbsqlc' --exclude 'web/dist' --exclude 'web/storybook-static' \
        | python3 scripts/semgrep-filter.py
}
run_section "semgrep (SAST Go+TS)" semgrep semgrep_scan

# ── Go CVEs with reachability (only flags vulns in code paths we call) ───
run_section "govulncheck" govulncheck govulncheck ./...

# ── Lockfile CVEs from the OSV database (Go + npm in one pass) ───────────
osv_scan() {
    osv-scanner scan source --lockfile go.mod --lockfile web/package-lock.json
}
run_section "osv-scanner (go.mod + npm lock)" osv-scanner osv_scan

# ── npm advisories (removed from CI as a Trivy duplicate; free locally) ──
npm_audit() { (cd web && npm audit --audit-level=moderate); }
run_section "npm audit" npm npm_audit

# ── Secrets over the ENTIRE git history ──────────────────────────────────
# The CI gitleaks action only scans the commit range of a push/PR, so a
# secret that landed long ago never resurfaces there. This scans all
# commits every time.
gitleaks_scan() { gitleaks detect --source . --redact --exit-code 1; }
run_section "gitleaks (full history)" gitleaks gitleaks_scan

# ── Summary ──────────────────────────────────────────────────────────────
echo
echo "═══ deep-scan summary ═══"
for name in "${!RESULT[@]}"; do
    printf "  %-32s %s\n" "$name" "${RESULT[$name]}"
done
[ "$FAILED" = 0 ] && echo "deep-scan: clean" || echo "deep-scan: FINDINGS ABOVE"
exit $FAILED
