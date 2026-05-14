import { useTranslation } from "react-i18next";

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
}: Readonly<{
  gates: ServerDetailPageProps["server"]["gates"];
  upstreams: ServerDetailPageProps["server"]["upstreams"];
}>) {
  const { t } = useTranslation("servers");
  return (
    <section className="rounded-xs bg-bg-card border border-border p-4 grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-6">
      <div className="flex flex-col gap-3">
        <span className="text-sm font-semibold text-fg">{t("detail.gates.title")}</span>
        <GatesPanel gates={gates} />
      </div>
      <div className="flex flex-col gap-3 border-l border-divider pl-6">
        <div className="flex items-center justify-between">
          <span className="text-sm font-semibold text-fg">{t("detail.upstreams.title")}</span>
          <span className="text-[10px] font-mono text-fg-muted">
            {t("detail.upstreams.peers", { count: upstreams.length })}
          </span>
        </div>
        <UpstreamsList upstreams={upstreams} />
      </div>
    </section>
  );
}
