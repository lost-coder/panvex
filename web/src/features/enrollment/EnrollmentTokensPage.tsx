// P3-FE-01: recomposed locally from UI-kit primitives.
// Phase-7 redesign: pulse row + status chips + denser token list with
// TTL countdown.
import { useMemo, useState } from "react";
import {
  Button,
  EmptyState,
  FilterBar,
  FilterChip,
  PageHeader,
  PulseRow,
} from "@/ui";
import { TokenList } from "@/features/enrollment/TokenList";
import { useNowSec } from "@/shared/hooks/useNowSec";
import type {
  EnrollmentTokensPageProps,
  EnrollmentTokenData,
} from "@/shared/api/types-pages/pages";

type StatusFilter = "all" | EnrollmentTokenData["status"];

type TickTone = "default" | "ok" | "warn" | "error";

function activeHint(active: number, expiringSoon: number): string {
  if (expiringSoon > 0) return `${expiringSoon} expiring <5m`;
  if (active === 0) return "none ready to consume";
  return "ready to enroll";
}

function activeTone(active: number, expiringSoon: number): TickTone {
  if (expiringSoon > 0) return "warn";
  if (active > 0) return "ok";
  return "default";
}

// ─── Main ─────────────────────────────────────────────────────────────────────

export function EnrollmentTokensPage({
  tokens,
  onCreateToken,
  onRevoke,
}: Readonly<EnrollmentTokensPageProps>) {
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("active");
  const [query, setQuery] = useState("");

  // Auto-refreshing "now" — the mount-time snapshot would freeze after a
  // few minutes and "expiring <5m" would keep counting already-expired
  // tokens as near-expiry.
  const nowSec = useNowSec();

  const counts = useMemo(() => {
    const c = {
      all: tokens.length,
      active: 0,
      consumed: 0,
      expired: 0,
      revoked: 0,
      expiringSoon: 0,
    };
    for (const t of tokens) {
      c[t.status]++;
      // "Expiring soon" = still active but TTL < 5 min. Surfacing this in the
      // pulse row catches operators who generated a token, walked away, and
      // are about to miss the bootstrap window.
      if (
        t.status === "active" &&
        t.expiresAtUnix - nowSec < 300 &&
        t.expiresAtUnix > nowSec
      ) {
        c.expiringSoon++;
      }
    }
    return c;
  }, [tokens, nowSec]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return tokens.filter((t) => {
      if (statusFilter !== "all" && t.status !== statusFilter) return false;
      if (!q) return true;
      return (
        t.value.toLowerCase().includes(q) ||
        (t.fleetGroupId ?? "").toLowerCase().includes(q)
      );
    });
  }, [tokens, query, statusFilter]);

  const statusChips: { id: StatusFilter; label: string; count: number }[] = [
    { id: "all", label: "All", count: counts.all },
    { id: "active", label: "Active", count: counts.active },
    { id: "consumed", label: "Consumed", count: counts.consumed },
    { id: "expired", label: "Expired", count: counts.expired },
    { id: "revoked", label: "Revoked", count: counts.revoked },
  ];

  return (
    <div className="flex flex-col">
      <PageHeader
        title="Enrollment Tokens"
        subtitle={`${tokens.length} token${tokens.length === 1 ? "" : "s"} · agent bootstrap`}
        trailing={
          <Button size="sm" onClick={onCreateToken}>
            + New Token
          </Button>
        }
      />

      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        <PulseRow
          ticks={[
            {
              label: "Active",
              value: counts.active.toLocaleString(),
              hint: activeHint(counts.active, counts.expiringSoon),
              tone: activeTone(counts.active, counts.expiringSoon),
            },
            {
              label: "Consumed",
              value: counts.consumed.toLocaleString(),
              hint: "agents bootstrapped",
            },
            {
              label: "Expired",
              value: counts.expired.toLocaleString(),
              hint: counts.expired > 0 ? "not consumed in TTL" : "none past TTL",
            },
            {
              label: "Revoked",
              value: counts.revoked.toLocaleString(),
              hint: counts.revoked > 0 ? "manually cancelled" : "no revocations",
              tone: counts.revoked > 0 ? "error" : "default",
            },
          ]}
        />

        {/* Filter bar — status chips + search. Default chip is "Active" since
            that's what operators hit this page to check. */}
        <FilterBar
          chips={statusChips.map((c) => (
            <FilterChip
              key={c.id}
              active={statusFilter === c.id}
              onClick={() => setStatusFilter(c.id)}
              count={c.count}
            >
              {c.label}
            </FilterChip>
          ))}
          search={{
            value: query,
            onChange: setQuery,
            placeholder: "Search token or fleet…",
          }}
        />

        {tokens.length === 0 && (
          <EmptyState
            title="No enrollment tokens"
            description="Generate a token to onboard a new Panvex agent. Each token is single-use and expires after its TTL."
            action={
              <Button size="sm" onClick={onCreateToken}>
                + New Token
              </Button>
            }
          />
        )}
        {tokens.length > 0 && filtered.length === 0 && (
          <EmptyState
            title={`No ${statusFilter === "all" ? "" : statusFilter + " "}tokens match`}
            description="Widen the filter or clear the search."
          />
        )}
        {tokens.length > 0 && filtered.length > 0 && (
          <TokenList tokens={filtered} onRevoke={onRevoke} nowSec={nowSec} />
        )}
      </div>
    </div>
  );
}
