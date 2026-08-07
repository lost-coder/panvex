#!/usr/bin/env python3
"""Filter semgrep JSON output through a triaged rule allowlist.

Reads `semgrep --json` on stdin, drops findings whose rule (last segment
of check_id) was triaged as a false-positive CLASS for this codebase on
2026-08-08, prints the rest, exits 1 if any remain.

The allowlist is a ratchet (archguard convention): entries may be
removed, never added without re-triaging and recording the reason here.
Suppressing a whole rule is deliberate — these are cases where the rule's
premise does not apply to this codebase, not individual noisy matches.
"""

import json
import sys

# rule-suffix -> why every match of it here is a false positive
ALLOWED = {
    "string-formatted-query": (
        "matches are table/column IDENTIFIERS from internal constants "
        "(VACUUM INTO, rotate-key table list, prune, migrateguard); SQL "
        "cannot bind identifiers as parameters. Values still go through "
        "placeholders — sqlc + rowserrcheck cover that side."
    ),
    "math-random-used": (
        "reconnect jitter / backoff spread, explicitly non-cryptographic; "
        "all key material uses crypto/rand (gosec G404 scope agrees)."
    ),
    "cookie-missing-secure": (
        "Secure is assigned dynamically via sessionCookieSecure() with "
        "__Host- prefix logic (Q3.U-S-13 / S-22); the rule only sees the "
        "literal struct field absent."
    ),
    "potential-dos-via-decompression-bomb": (
        "updates/download.go wraps both the archive body and every tar "
        "entry in io.LimitReader with explicit caps."
    ),
    "filepath-clean-misuse": (
        "ui.go cleans a ROOTED URL path (leading slash guarantees .. is "
        "eliminated) and serves from embed.FS, a sealed VFS that rejects "
        "non-ValidPath names anyway."
    ),
}

data = json.load(sys.stdin)
kept, suppressed = [], {}
for r in data.get("results", []):
    suffix = r["check_id"].rsplit(".", 1)[-1]
    if suffix in ALLOWED:
        suppressed[suffix] = suppressed.get(suffix, 0) + 1
    else:
        kept.append(r)

for suffix, n in sorted(suppressed.items()):
    print(f"  suppressed {n:2d}x {suffix} (triaged FP class)")

for r in kept:
    path = r["path"]
    line = r["start"]["line"]
    suffix = r["check_id"].rsplit(".", 1)[-1]
    msg = r["extra"]["message"].split("\n")[0][:160]
    print(f"\n  {path}:{line}  [{suffix}]\n    {msg}")

errors = data.get("errors", [])
if errors:
    print(f"\n  semgrep reported {len(errors)} internal error(s):")
    for e in errors[:5]:
        print(f"    {str(e.get('message', e))[:160]}")

if kept:
    print(f"\n  {len(kept)} finding(s) need triage: fix, or add the rule "
          f"suffix to ALLOWED in scripts/semgrep-filter.py with a reason.")
    sys.exit(1)
sys.exit(0)
