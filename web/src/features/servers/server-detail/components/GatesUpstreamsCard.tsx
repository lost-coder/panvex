import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

import { GatesPanel } from "./GatesPanel";
import { UpstreamsList } from "./UpstreamsList";

/**
 * Desktop "one card, two columns" composition for Gates and Upstreams,
 * split by a vertical divider. The two halves intentionally use
 * different visual languages (dashed boolean rows vs solid entity
 * panels) so they read as distinct content types at a glance.
 */
export function GatesUpstreamsCard({
  gates,
  upstreams,
}: {
  gates: ServerDetailPageProps["server"]["gates"];
  upstreams: ServerDetailPageProps["server"]["upstreams"];
}) {
  return (
    <section className="rounded-xs bg-bg-card border border-border p-4 grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-6">
      <div className="flex flex-col gap-3">
        <span className="text-sm font-semibold text-fg">Gates</span>
        <GatesPanel gates={gates} />
      </div>
      <div className="flex flex-col gap-3 border-l border-divider pl-6">
        <div className="flex items-center justify-between">
          <span className="text-sm font-semibold text-fg">Upstreams</span>
          <span className="text-[10px] font-mono text-fg-muted">
            {upstreams.length} peer{upstreams.length === 1 ? "" : "s"}
          </span>
        </div>
        <UpstreamsList upstreams={upstreams} />
      </div>
    </section>
  );
}
