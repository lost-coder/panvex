import { useState } from "react";

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
  const { mePool } = server;
  const [stateFilter, setStateFilter] = useState<
    "all" | "active" | "warmup" | "draining" | "degraded"
  >("all");

  if (!mePool || !mePool.enabled) {
    return (
      <div className="py-8 text-center text-fg-muted text-sm">
        ME Pool is not available on this server.
      </div>
    );
  }

  const { summary, generations, hardswap, contour, writersHealth, refill, writersList } = mePool;
  const coverageTone = (pct: number): string =>
    pct >= 100 ? "text-status-ok" : pct >= 70 ? "text-status-warn" : "text-status-error";

  const filteredWriters = writersList.filter((w) => {
    if (stateFilter === "all") return true;
    if (stateFilter === "degraded") return w.degraded;
    if (stateFilter === "draining") return w.draining;
    return w.state === stateFilter;
  });

  const writerColumns = [
    {
      key: "writerId",
      header: "#",
      render: (row: Readonly<ServerMeWriterData>) => <MonoValue>{row.writerId}</MonoValue>,
      className: "w-12",
    },
    {
      key: "dc",
      header: "DC",
      render: (row: Readonly<ServerMeWriterData>) => (
        <MonoValue>{row.dc != null ? `DC${row.dc}` : "—"}</MonoValue>
      ),
      className: "w-14",
    },
    {
      key: "endpoint",
      header: "Endpoint",
      render: (row: Readonly<ServerMeWriterData>) => (
        <MonoValue className="truncate">{row.endpoint}</MonoValue>
      ),
    },
    {
      key: "state",
      header: "State",
      render: (row: Readonly<ServerMeWriterData>) => (
        <Badge variant={row.degraded ? "warn" : row.state === "active" ? "ok" : "default"}>
          {row.state}
          {row.degraded ? " ⚠" : ""}
        </Badge>
      ),
      className: "w-24",
    },
    {
      key: "rtt",
      header: "RTT",
      render: (row: Readonly<ServerMeWriterData>) => (
        <MonoValue>{row.rttEmaMs != null ? `${row.rttEmaMs.toFixed(1)}ms` : "—"}</MonoValue>
      ),
      className: "w-20",
    },
    {
      key: "clients",
      header: "Clients",
      render: (row: Readonly<ServerMeWriterData>) => <MonoValue>{row.boundClients}</MonoValue>,
      className: "w-16",
    },
    {
      key: "idle",
      header: "Idle",
      render: (row: Readonly<ServerMeWriterData>) => (
        <MonoValue>{row.idleForSecs != null ? `${row.idleForSecs}s` : "—"}</MonoValue>
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
          label="Coverage"
          value={summary.coveragePct}
          unit="%"
          tone={coverageTone(summary.coveragePct)}
          caption={`${summary.aliveWriters} / ${summary.requiredWriters} writers alive`}
          barPct={summary.coveragePct}
        />
        <HealthHeroMetric
          label="Fresh coverage"
          value={summary.freshCoveragePct}
          unit="%"
          tone={coverageTone(summary.freshCoveragePct)}
          caption={`${summary.freshAliveWriters} fresh-alive`}
          barPct={summary.freshCoveragePct}
        />
        <HealthHeroMetric
          label="Endpoints"
          value={summary.availablePct}
          unit="%"
          tone={coverageTone(summary.availablePct)}
          caption={`${summary.availableEndpoints} / ${summary.configuredEndpoints} reachable`}
          barPct={summary.availablePct}
        />
      </div>

      {/* Mid-level mechanics: generations / writer state / refill.
          Each card is a compact labelled row-list so the panel reads
          as structured data rather than a free-form blob. */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
        <MiniCard title="Generations" hint={`${summary.configuredDcGroups} DC groups`}>
          <Kv k="Active" v={<span className="font-mono text-fg">{generations.active}</span>} />
          <Kv k="Warm" v={<span className="font-mono text-fg">{generations.warm}</span>} />
          <Kv
            k="Pending hardswap"
            v={
              generations.pendingHardswap > 0 ? (
                <Badge variant="warn">{generations.pendingHardswap}</Badge>
              ) : (
                <span className="font-mono text-fg">0</span>
              )
            }
          />
          <Kv
            k="Hardswap"
            v={
              <Badge variant={hardswap.enabled ? "ok" : "default"}>
                {hardswap.enabled ? "ON" : "OFF"}
                {hardswap.pending ? " · pending" : ""}
              </Badge>
            }
          />
          {generations.drainingGenerations.length > 0 && (
            <Kv
              k="Draining"
              v={
                <span className="font-mono text-[11px] text-fg-muted">
                  [{generations.drainingGenerations.join(", ")}]
                </span>
              }
            />
          )}
          <Kv
            k="Contour"
            v={
              <span className="font-mono text-[11px] text-fg-muted">
                A{contour.active} · W{contour.warm} · D{contour.draining}
              </span>
            }
          />
        </MiniCard>

        <MiniCard title="Writer state">
          <Kv
            k="Healthy"
            v={
              <span className="font-mono font-semibold text-status-ok tabular-nums">
                {writersHealth.healthy}
              </span>
            }
          />
          <Kv
            k="Degraded"
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
            k="Draining"
            v={
              <span className="font-mono font-semibold text-fg tabular-nums">
                {writersHealth.draining}
              </span>
            }
          />
          <Kv
            k="Required"
            v={
              <span className="font-mono text-fg-muted tabular-nums">
                {summary.requiredWriters}
              </span>
            }
          />
        </MiniCard>

        <MiniCard title="Refill" hint="inflight work">
          <Kv
            k="Inflight endpoints"
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
            k="Inflight DC"
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
                  className="font-mono text-[10px] px-1.5 py-0.5 rounded-xs bg-bg border border-divider text-fg-muted"
                >
                  DC{e.dc}
                  <span className="opacity-60">·{e.family}</span>{" "}
                  <span className="text-fg">{e.inflight}</span>
                </span>
              ))}
            </div>
          )}
        </MiniCard>
      </div>

      {/* Writers list with filter chips. */}
      <div className="flex flex-col gap-2">
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold text-fg">Writers</span>
            <span className="text-[11px] font-mono text-fg-muted">
              {filteredWriters.length}/{writersList.length}
            </span>
          </div>
          <div className="inline-flex items-center gap-0.5 p-0.5 rounded-xs border border-border-hi bg-bg">
            {(["all", "active", "warmup", "draining", "degraded"] as const).map((key) => {
              const active = stateFilter === key;
              return (
                <button
                  key={key}
                  type="button"
                  onClick={() => setStateFilter(key)}
                  className={cn(
                    "h-6 px-2 rounded-xs text-[10px] font-mono transition-colors",
                    active
                      ? "bg-bg-card-hi text-fg"
                      : "text-fg-muted hover:text-fg hover:bg-bg-hover",
                  )}
                >
                  {key}
                </button>
              );
            })}
          </div>
        </div>
        <DataTable
          columns={writerColumns}
          data={filteredWriters}
          keyExtractor={(row) => String(row.writerId)}
          emptyMessage="No writers match this filter"
        />
      </div>
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
  return (
    <div className="rounded-xs bg-bg-card border border-divider p-4 flex flex-col gap-2">
      <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted">
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
          className={cn(
            "h-full rounded-full",
            barPct >= 100
              ? "bg-status-ok"
              : barPct >= 70
                ? "bg-status-warn"
                : "bg-status-error",
          )}
          style={{ width: `${Math.max(0, Math.min(100, barPct))}%` }}
        />
      </div>
      <span className="text-[11px] font-mono text-fg-muted">{caption}</span>
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
        {hint && <span className="text-[10px] font-mono text-fg-muted">{hint}</span>}
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
