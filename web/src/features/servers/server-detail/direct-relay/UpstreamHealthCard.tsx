import { SectionHeader } from "@/ui";

export interface UpstreamHealthCardProps {
  healthy: number;
  total: number;
  failRatePct5m: number;
  failRateKnown: boolean;
  currentDirectConnections: number;
}

export function UpstreamHealthCard({
  healthy,
  total,
  failRatePct5m,
  failRateKnown,
  currentDirectConnections,
}: Readonly<UpstreamHealthCardProps>) {
  return (
    <section className="bg-bg-card border border-border rounded-md p-4 flex flex-col gap-3">
      <SectionHeader title="Upstream health" />
      <dl className="grid grid-cols-3 gap-4 font-mono text-sm">
        <div>
          <dt className="text-fg-muted text-xs">Healthy / Total</dt>
          <dd className="text-fg text-lg">
            {healthy}/{total}
          </dd>
        </div>
        <div>
          <dt className="text-fg-muted text-xs">Fail rate (5m)</dt>
          <dd className="text-fg text-lg">
            {failRateKnown ? `${failRatePct5m.toFixed(1)}%` : "unknown"}
          </dd>
        </div>
        <div>
          <dt className="text-fg-muted text-xs">Direct connections</dt>
          <dd className="text-fg text-lg">{currentDirectConnections}</dd>
        </div>
      </dl>
    </section>
  );
}
