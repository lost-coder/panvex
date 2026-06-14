import { useState } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/ui/lib/cn";
import { Badge } from "@/ui/primitives/Badge";
import { DataTable } from "@/ui/components/DataTable";
import { MonoValue } from "@/ui/primitives";
import type {
  ServerDetailPageProps,
  ServerMeWriterData,
} from "@/shared/api/types-pages/pages";

/**
 * ME Pool renders Middle-Proxy health for a single node. The data lives
 * at three concerns:
 *   • top-line ratios (writer coverage + fresh coverage + endpoint
 *     availability) — what an operator scans first,
 *   • mid-level mechanics (generations, writer state, refill activity)
 *     — what you look at when the top-line dips,
 *   • individual writers — drill-down table at the bottom.
 *
 * Previous implementation stacked every number as a tile with equal
 * visual weight, which flattened the data hierarchy. This layout
 * foregrounds the ratios, groups the mechanics into three compact
 * side-by-side cards, and keeps the writers list as a distinct
 * sub-section with state filtering.
 */
export function MePoolTab({ server }: Readonly<{ server: ServerDetailPageProps["server"] }>) {
  const { t } = useTranslation("servers");
  const { mePool } = server;
  const [stateFilter, setStateFilter] = useState<
    "all" | "active" | "warmup" | "draining" | "degraded"
  >("all");

  if (!mePool?.enabled) {
    return (
      <div className="py-8 text-center text-fg-muted text-sm">
        {t("detail.mePool.unavailable")}
      </div>
    );
  }

  const { summary, generations, hardswap, contour, writersHealth, refill, writersList } = mePool;
  const coverageTone = (pct: number): string => {
    if (pct >= 100) return "text-status-ok";
    if (pct >= 70) return "text-status-warn";
    return "text-status-error";
  };

  const writerStateVariant = (row: Readonly<ServerMeWriterData>): "warn" | "ok" | "default" => {
    if (row.degraded) return "warn";
    if (row.state === "active") return "ok";
    return "default";
  };

  const filteredWriters = writersList.filter((w) => {
    if (stateFilter === "all") return true;
    if (stateFilter === "degraded") return w.degraded;
    if (stateFilter === "draining") return w.draining;
    return w.state === stateFilter;
  });

  const writerColumns = [
    {
      key: "writerId",
      header: t("detail.mePool.writerCol.id"),
      render: (row: Readonly<ServerMeWriterData>) => <MonoValue>{row.writerId}</MonoValue>,
      className: "w-12",
    },
    {
      key: "dc",
      header: t("detail.mePool.writerCol.dc"),
      render: (row: Readonly<ServerMeWriterData>) => (
        <MonoValue>{row.dc == null ? "—" : `DC${row.dc}`}</MonoValue>
      ),
      className: "w-14",
    },
    {
      key: "endpoint",
      header: t("detail.mePool.writerCol.endpoint"),
      render: (row: Readonly<ServerMeWriterData>) => (
        <MonoValue className="truncate">{row.endpoint}</MonoValue>
      ),
    },
    {
      key: "state",
      header: t("detail.mePool.writerCol.state"),
      render: (row: Readonly<ServerMeWriterData>) => (
        <Badge variant={writerStateVariant(row)}>
          {row.state}
          {row.degraded ? " ⚠" : ""}
        </Badge>
      ),
      className: "w-24",
    },
    {
      key: "rtt",
      header: t("detail.mePool.writerCol.rtt"),
      render: (row: Readonly<ServerMeWriterData>) => (
        <MonoValue>{row.rttEmaMs == null ? "—" : `${row.rttEmaMs.toFixed(1)}ms`}</MonoValue>
      ),
      className: "w-20",
    },
    {
      key: "clients",
      header: t("detail.mePool.writerCol.clients"),
      render: (row: Readonly<ServerMeWriterData>) => <MonoValue>{row.boundClients}</MonoValue>,
      className: "w-16",
    },
    {
      key: "idle",
      header: t("detail.mePool.writerCol.idle"),
      render: (row: Readonly<ServerMeWriterData>) => (
        <MonoValue>{row.idleForSecs == null ? "—" : `${row.idleForSecs}s`}</MonoValue>
      ),
      className: "w-16",
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      {/* Health hero — three large ratios, each with a bar. Reads left
          to right as the operator's first scan: are writers covered,
          is the fresh pool healthy, are enough endpoints reachable. */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <HealthHeroMetric
          label={t("detail.mePool.coverage")}
          value={summary.coveragePct}
          unit="%"
          tone={coverageTone(summary.coveragePct)}
          caption={t("detail.mePool.coverageCaption", {
            alive: summary.aliveWriters,
            required: summary.requiredWriters,
          })}
          barPct={summary.coveragePct}
        />
        <HealthHeroMetric
          label={t("detail.mePool.freshCoverage")}
          value={summary.freshCoveragePct}
          unit="%"
          tone={coverageTone(summary.freshCoveragePct)}
          caption={t("detail.mePool.freshCoverageCaption", { count: summary.freshAliveWriters })}
          barPct={summary.freshCoveragePct}
        />
        <HealthHeroMetric
          label={t("detail.mePool.endpoints")}
          value={summary.availablePct}
          unit="%"
          tone={coverageTone(summary.availablePct)}
          caption={t("detail.mePool.endpointsCaption", {
            available: summary.availableEndpoints,
            configured: summary.configuredEndpoints,
          })}
          barPct={summary.availablePct}
        />
      </div>

      {/* Mid-level mechanics: generations / writer state / refill.
          Each card is a compact labelled row-list so the panel reads
          as structured data rather than a free-form blob. */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <MiniCard
          title={t("detail.mePool.generations")}
          hint={t("detail.mePool.generationsHint", { count: summary.configuredDcGroups })}
        >
          <Kv k={t("detail.mePool.active")} v={<span className="font-mono text-fg">{generations.active}</span>} />
          <Kv k={t("detail.mePool.warm")} v={<span className="font-mono text-fg">{generations.warm}</span>} />
          <Kv
            k={t("detail.mePool.pendingHardswap")}
            v={
              generations.pendingHardswap > 0 ? (
                <Badge variant="warn">{generations.pendingHardswap}</Badge>
              ) : (
                <span className="font-mono text-fg">0</span>
              )
            }
          />
          <Kv
            k={t("detail.mePool.hardswap")}
            v={
              <Badge variant={hardswap.enabled ? "ok" : "default"}>
                {hardswap.enabled ? t("detail.mePool.hardswapOn") : t("detail.mePool.hardswapOff")}
                {hardswap.pending ? t("detail.mePool.hardswapPending") : ""}
              </Badge>
            }
          />
          {generations.drainingGenerations.length > 0 && (
            <Kv
              k={t("detail.mePool.draining")}
              v={
                <span className="font-mono text-micro text-fg-muted">
                  [{generations.drainingGenerations.join(", ")}]
                </span>
              }
            />
          )}
          <Kv
            k={t("detail.mePool.contour")}
            v={
              <span className="font-mono text-micro text-fg-muted">
                A{contour.active} · W{contour.warm} · D{contour.draining}
              </span>
            }
          />
        </MiniCard>

        <MiniCard title={t("detail.mePool.writerState")}>
          <Kv
            k={t("detail.mePool.healthy")}
            v={
              <span className="font-mono font-semibold text-status-ok tabular-nums">
                {writersHealth.healthy}
              </span>
            }
          />
          <Kv
            k={t("detail.mePool.degraded")}
            v={
              <span
                className={cn(
                  "font-mono font-semibold tabular-nums",
                  writersHealth.degraded > 0 ? "text-status-warn" : "text-fg",
                )}
              >
                {writersHealth.degraded}
              </span>
            }
          />
          <Kv
            k={t("detail.mePool.draining")}
            v={
              <span className="font-mono font-semibold text-fg tabular-nums">
                {writersHealth.draining}
              </span>
            }
          />
          <Kv
            k={t("detail.mePool.required")}
            v={
              <span className="font-mono text-fg-muted tabular-nums">
                {summary.requiredWriters}
              </span>
            }
          />
        </MiniCard>

        <MiniCard title={t("detail.mePool.refill")} hint={t("detail.mePool.refillHint")}>
          <Kv
            k={t("detail.mePool.inflightEndpoints")}
            v={
              <span
                className={cn(
                  "font-mono font-semibold tabular-nums",
                  refill.inflightEndpoints > 0 ? "text-status-warn" : "text-fg",
                )}
              >
                {refill.inflightEndpoints}
              </span>
            }
          />
          <Kv
            k={t("detail.mePool.inflightDc")}
            v={
              <span
                className={cn(
                  "font-mono font-semibold tabular-nums",
                  refill.inflightDcs > 0 ? "text-status-warn" : "text-fg",
                )}
              >
                {refill.inflightDcs}
              </span>
            }
          />
          {refill.byDc.length > 0 && (
            <div className="flex flex-wrap gap-1 pt-2 border-t border-dashed border-divider">
              {refill.byDc.map((e) => (
                <span
                  key={`${e.dc}-${e.family}`}
                  className="font-mono text-nano px-1.5 py-0.5 rounded-xs bg-bg border border-divider text-fg-muted"
                >
                  {`DC${e.dc}`}
                  <span className="opacity-60">·{e.family}</span>{" "}
                  <span className="text-fg">{e.inflight}</span>
                </span>
              ))}
            </div>
          )}
        </MiniCard>
      </div>

      {/* Writers list with filter chips. U-18: the agent's telemetry only
          carries the aggregate me_writers_summary, not a per-writer array,
          so writersList is always empty. Rather than render a filterable
          table that is permanently "0/0 · No writers match" (reads as broken),
          show the per-writer section only when data actually arrives, and a
          one-line note otherwise. */}
      {writersList.length > 0 ? (
        <div className="flex flex-col gap-2">
          <div className="flex items-center justify-between gap-3 flex-wrap">
            <div className="flex items-center gap-2">
              <span className="text-sm font-semibold text-fg">{t("detail.mePool.writers")}</span>
              <span className="text-micro font-mono text-fg-muted">
                {filteredWriters.length}/{writersList.length}
              </span>
            </div>
            <div className="inline-flex items-center gap-0.5 p-0.5 rounded-xs border border-border-hi bg-bg">
              {(["all", "active", "warmup", "draining", "degraded"] as const).map((key) => {
                const active = stateFilter === key;
                const labelKey = (() => {
                  if (key === "all") return "detail.mePool.filterAll";
                  if (key === "active") return "detail.mePool.filterActive";
                  if (key === "warmup") return "detail.mePool.filterWarmup";
                  if (key === "draining") return "detail.mePool.filterDraining";
                  return "detail.mePool.filterDegraded";
                })();
                return (
                  <button
                    key={key}
                    type="button"
                    onClick={() => setStateFilter(key)}
                    className={cn(
                      "h-6 px-2 rounded-xs text-nano font-mono transition-colors",
                      active
                        ? "bg-bg-card-hi text-fg"
                        : "text-fg-muted hover:text-fg hover:bg-bg-hover",
                    )}
                  >
                    {t(labelKey)}
                  </button>
                );
              })}
            </div>
          </div>
          <DataTable
            columns={writerColumns}
            data={filteredWriters}
            keyExtractor={(row) => String(row.writerId)}
            emptyMessage={t("detail.mePool.writerEmpty")}
          />
        </div>
      ) : (
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-fg">{t("detail.mePool.writers")}</span>
          <span className="text-micro text-fg-muted">{t("detail.mePool.writersUnavailable")}</span>
        </div>
      )}
    </div>
  );
}

function HealthHeroMetric({
  label,
  value,
  unit,
  tone,
  caption,
  barPct,
}: Readonly<{
  label: string;
  value: number;
  unit: string;
  tone: string;
  caption: string;
  barPct: number;
}>) {
  const barTone = (() => {
    if (barPct >= 100) return "bg-status-ok";
    if (barPct >= 70) return "bg-status-warn";
    return "bg-status-error";
  })();
  return (
    <div className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-2">
      <span className="text-nano font-mono uppercase tracking-wider text-fg-muted">
        {label}
      </span>
      <div className="flex items-baseline gap-1">
        <span className={cn("text-3xl font-mono font-semibold leading-none tabular-nums", tone)}>
          {value}
        </span>
        <span className="text-sm font-mono text-fg-muted">{unit}</span>
      </div>
      <div className="h-1.5 w-full rounded-full bg-border overflow-hidden">
        <div
          className={cn("h-full rounded-full", barTone)}
          style={{ width: `${Math.max(0, Math.min(100, barPct))}%` }}
        />
      </div>
      <span className="text-micro font-mono text-fg-muted">{caption}</span>
    </div>
  );
}

function MiniCard({
  title,
  hint,
  children,
}: Readonly<{
  title: string;
  hint?: string;
  children: React.ReactNode;
}>) {
  return (
    <div className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-2">
      <div className="flex items-baseline justify-between">
        <span className="text-sm font-semibold text-fg">{title}</span>
        {hint && <span className="text-nano font-mono text-fg-muted">{hint}</span>}
      </div>
      <div className="flex flex-col gap-1.5">{children}</div>
    </div>
  );
}

function Kv({ k, v }: Readonly<{ k: string; v: React.ReactNode }>) {
  return (
    <div className="flex items-center justify-between gap-3 text-xs">
      <span className="text-fg-muted">{k}</span>
      <span>{v}</span>
    </div>
  );
}
