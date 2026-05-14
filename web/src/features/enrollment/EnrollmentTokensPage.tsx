// P3-FE-01: recomposed locally from UI-kit primitives.
// Phase-7 redesign: pulse row + status chips + denser token list with
// TTL countdown.
import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
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
  const { t } = useTranslation("enrollment");
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
    for (const tok of tokens) {
      c[tok.status]++;
      // "Expiring soon" = still active but TTL < 5 min. Surfacing this in the
      // pulse row catches operators who generated a token, walked away, and
      // are about to miss the bootstrap window.
      if (
        tok.status === "active" &&
        tok.expiresAtUnix - nowSec < 300 &&
        tok.expiresAtUnix > nowSec
      ) {
        c.expiringSoon++;
      }
    }
    return c;
  }, [tokens, nowSec]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return tokens.filter((tok) => {
      if (statusFilter !== "all" && tok.status !== statusFilter) return false;
      if (!q) return true;
      return (
        tok.value.toLowerCase().includes(q) ||
        (tok.fleetGroupId ?? "").toLowerCase().includes(q)
      );
    });
  }, [tokens, query, statusFilter]);

  const activeHint = (active: number, expiringSoon: number): string => {
    if (expiringSoon > 0) return t("tokens.pulse.expiringSoon", { count: expiringSoon });
    if (active === 0) return t("tokens.pulse.noneReady");
    return t("tokens.pulse.readyToEnroll");
  };

  const statusChips: { id: StatusFilter; label: string; count: number }[] = [
    { id: "all", label: t("tokens.filters.all"), count: counts.all },
    { id: "active", label: t("tokens.filters.active"), count: counts.active },
    { id: "consumed", label: t("tokens.filters.consumed"), count: counts.consumed },
    { id: "expired", label: t("tokens.filters.expired"), count: counts.expired },
    { id: "revoked", label: t("tokens.filters.revoked"), count: counts.revoked },
  ];

  return (
    <div className="flex flex-col">
      <PageHeader
        title={t("tokens.title")}
        subtitle={t("tokens.subtitle", { count: tokens.length })}
        trailing={
          <Button size="sm" onClick={onCreateToken}>
            {t("tokens.newToken")}
          </Button>
        }
      />

      <div className="px-4 md:px-8 pb-8 flex flex-col gap-5">
        <PulseRow
          ticks={[
            {
              label: t("tokens.pulse.active"),
              value: counts.active.toLocaleString(),
              hint: activeHint(counts.active, counts.expiringSoon),
              tone: activeTone(counts.active, counts.expiringSoon),
            },
            {
              label: t("tokens.pulse.consumed"),
              value: counts.consumed.toLocaleString(),
              hint: t("tokens.pulse.agentsBootstrapped"),
            },
            {
              label: t("tokens.pulse.expired"),
              value: counts.expired.toLocaleString(),
              hint:
                counts.expired > 0
                  ? t("tokens.pulse.notConsumedInTtl")
                  : t("tokens.pulse.nonePastTtl"),
            },
            {
              label: t("tokens.pulse.revoked"),
              value: counts.revoked.toLocaleString(),
              hint:
                counts.revoked > 0
                  ? t("tokens.pulse.manuallyCancelled")
                  : t("tokens.pulse.noRevocations"),
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
            placeholder: t("tokens.filters.searchPlaceholder"),
          }}
        />

        {tokens.length === 0 && (
          <EmptyState
            title={t("tokens.empty.title")}
            description={t("tokens.empty.description")}
            action={
              <Button size="sm" onClick={onCreateToken}>
                {t("tokens.newToken")}
              </Button>
            }
          />
        )}
        {tokens.length > 0 && filtered.length === 0 && (
          <EmptyState
            title={
              statusFilter === "all"
                ? t("tokens.empty.filterTitleAll")
                : t("tokens.empty.filterTitleStatus", {
                    status: t(`tokens.filters.${statusFilter}`).toLowerCase(),
                  })
            }
            description={t("tokens.empty.filterDescription")}
          />
        )}
        {tokens.length > 0 && filtered.length > 0 && (
          <TokenList tokens={filtered} onRevoke={onRevoke} nowSec={nowSec} />
        )}
      </div>
    </div>
  );
}
