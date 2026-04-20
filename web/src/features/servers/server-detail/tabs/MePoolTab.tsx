import { FieldLabel, MonoValue } from "@/ui/primitives";
import { SectionHeader } from "@/ui/layout/SectionHeader";
import { Badge } from "@/ui/primitives/Badge";
import { DataTable } from "@/ui/components/DataTable";
import { coverageColor } from "@/ui/lib/status";
import type { ServerDetailPageProps, ServerMeWriterData } from "@/shared/api/types-pages/pages";

export function MePoolTab({ server }: { server: ServerDetailPageProps["server"] }) {
  const { mePool } = server;

  if (!mePool || !mePool.enabled) {
    return (
      <div className="py-8 text-center text-fg-muted text-sm">
        ME Pool is not available on this server.
      </div>
    );
  }

  const writerColumns = [
    {
      key: "writerId",
      header: "Writer ID",
      render: (row: ServerMeWriterData) => <MonoValue>{row.writerId}</MonoValue>,
    },
    {
      key: "dc",
      header: "DC",
      render: (row: ServerMeWriterData) => (
        <MonoValue>{row.dc != null ? `DC${row.dc}` : "—"}</MonoValue>
      ),
    },
    {
      key: "endpoint",
      header: "Endpoint",
      render: (row: ServerMeWriterData) => (
        <MonoValue className="truncate">{row.endpoint}</MonoValue>
      ),
    },
    {
      key: "state",
      header: "State",
      render: (row: ServerMeWriterData) => (
        <Badge variant={row.degraded ? "warn" : row.state === "active" ? "ok" : "default"}>
          {row.state}
          {row.degraded && " ⚠"}
        </Badge>
      ),
    },
    {
      key: "rtt",
      header: "RTT",
      render: (row: ServerMeWriterData) => (
        <MonoValue>{row.rttEmaMs != null ? `${row.rttEmaMs.toFixed(1)}ms` : "—"}</MonoValue>
      ),
    },
    {
      key: "clients",
      header: "Clients",
      render: (row: ServerMeWriterData) => <MonoValue>{row.boundClients}</MonoValue>,
    },
    {
      key: "idle",
      header: "Idle",
      render: (row: ServerMeWriterData) => (
        <MonoValue>{row.idleForSecs != null ? `${row.idleForSecs}s` : "—"}</MonoValue>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-6 pt-2">
      {/* ME Writers Summary — 9-field 3×3 grid */}
      <div>
        <SectionHeader title="ME Writers Summary" />
        <div className="grid grid-cols-3 gap-2">
          {[
            { label: "Alive Writers", value: mePool.summary.aliveWriters },
            { label: "Available Endpoints", value: mePool.summary.availableEndpoints },
            { label: "Available %", value: `${mePool.summary.availablePct}%` },
            { label: "Configured DC Groups", value: mePool.summary.configuredDcGroups },
            { label: "Configured Endpoints", value: mePool.summary.configuredEndpoints },
            {
              label: "Coverage %",
              value: `${mePool.summary.coveragePct}%`,
              color: coverageColor(mePool.summary.coveragePct),
            },
            { label: "Fresh Alive Writers", value: mePool.summary.freshAliveWriters },
            {
              label: "Fresh Coverage %",
              value: `${mePool.summary.freshCoveragePct}%`,
              color: coverageColor(mePool.summary.freshCoveragePct),
            },
            { label: "Required Writers", value: mePool.summary.requiredWriters },
          ].map(({ label, value, color }) => (
            <div key={label} className="rounded-xs bg-bg-card p-3 flex flex-col gap-0.5">
              <span
                className={`text-lg font-mono font-semibold leading-none ${color ?? "text-fg"}`}
              >
                {value}
              </span>
              <FieldLabel>{label}</FieldLabel>
            </div>
          ))}
        </div>
      </div>

      {/* Three side-by-side cards: Generations / Contour / Writers Health */}
      <div className="grid grid-cols-3 gap-2">
        {/* Generations */}
        <div className="rounded-xs bg-bg-card p-3 flex flex-col gap-2">
          <FieldLabel>Generations</FieldLabel>
          <div className="flex flex-col gap-1 text-sm">
            <div className="flex justify-between">
              <span className="text-fg-muted">Active</span>
              <span className="font-mono text-fg">{mePool.generations.active}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-fg-muted">Warm</span>
              <span className="font-mono text-fg">{mePool.generations.warm}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-fg-muted">Pending Hardswap</span>
              <span className="font-mono text-fg">
                {mePool.generations.pendingHardswap > 0 ? (
                  <Badge variant="warn">{mePool.generations.pendingHardswap}</Badge>
                ) : (
                  mePool.generations.pendingHardswap
                )}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-fg-muted">Hardswap</span>
              <Badge variant={mePool.hardswap.enabled ? "ok" : "default"}>
                {mePool.hardswap.enabled ? "ON" : "OFF"}
              </Badge>
            </div>
            {mePool.generations.drainingGenerations.length > 0 && (
              <div className="flex justify-between">
                <span className="text-fg-muted">Draining</span>
                <span className="font-mono text-xs text-fg">
                  [{mePool.generations.drainingGenerations.join(", ")}]
                </span>
              </div>
            )}
          </div>
        </div>

        {/* Contour */}
        <div className="rounded-xs bg-bg-card p-3 flex flex-col gap-2">
          <FieldLabel>Contour</FieldLabel>
          <div className="flex flex-col gap-1 text-sm">
            <div className="flex justify-between">
              <span className="text-fg-muted">Active</span>
              <span className="font-mono text-fg">{mePool.contour.active}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-fg-muted">Warm</span>
              <span className="font-mono text-fg">{mePool.contour.warm}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-fg-muted">Draining</span>
              <span className="font-mono text-fg">{mePool.contour.draining}</span>
            </div>
          </div>
        </div>

        {/* Writers Health */}
        <div className="rounded-xs bg-bg-card p-3 flex flex-col gap-2">
          <FieldLabel>Writers Health</FieldLabel>
          <div className="flex flex-col gap-1 text-sm">
            <div className="flex justify-between">
              <span className="text-fg-muted">Healthy</span>
              <span className="font-mono text-status-ok">{mePool.writersHealth.healthy}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-fg-muted">Degraded</span>
              <span
                className={`font-mono ${mePool.writersHealth.degraded > 0 ? "text-status-warn" : "text-fg"}`}
              >
                {mePool.writersHealth.degraded}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-fg-muted">Draining</span>
              <span className="font-mono text-fg">{mePool.writersHealth.draining}</span>
            </div>
          </div>
        </div>
      </div>

      {/* Refill */}
      <div className="rounded-xs bg-bg-card p-4 flex flex-col gap-2">
        <FieldLabel>Refill</FieldLabel>
        <div className="grid grid-cols-2 gap-2">
          <div className="flex flex-col gap-0.5">
            <span className="text-2xl font-mono font-semibold text-fg leading-none">
              {mePool.refill.inflightEndpoints}
            </span>
            <FieldLabel>Inflight Endpoints</FieldLabel>
          </div>
          <div className="flex flex-col gap-0.5">
            <span className="text-2xl font-mono font-semibold text-fg leading-none">
              {mePool.refill.inflightDcs}
            </span>
            <FieldLabel>Inflight DC</FieldLabel>
          </div>
        </div>
        {mePool.refill.byDc.length > 0 && (
          <div className="flex flex-wrap gap-1 mt-1">
            {mePool.refill.byDc.map((entry) => (
              <Badge key={`${entry.dc}-${entry.family}`} variant="default">
                DC {entry.dc} ({entry.family}): {entry.inflight}
              </Badge>
            ))}
          </div>
        )}
      </div>

      {/* Writers table */}
      <div>
        <SectionHeader title="Writers" />
        <DataTable
          columns={writerColumns}
          data={mePool.writersList}
          keyExtractor={(row) => String(row.writerId)}
          emptyMessage="No writers"
        />
      </div>
    </div>
  );
}
