// R-Q-08: Deployments + per-target connection-links card extracted from
// ClientDetailPage.tsx. The page hands over the deployments array and
// optional agent label resolver; the card owns the link-strip layout.

import {
  Badge,
  CopyButton,
  deployVariant,
  type ClientDeploymentData,
} from "@/ui";

interface LinksStripProps {
  links: { classic: string[]; secure: string[]; tls: string[] };
}

function LinksStrip({ links }: Readonly<LinksStripProps>) {
  type LinkGroup = { key: "tls" | "secure" | "classic"; label: string; items: string[] };
  const groups: LinkGroup[] = (
    [
      { key: "tls", label: "TLS", items: links.tls },
      { key: "secure", label: "Secure", items: links.secure },
      { key: "classic", label: "Classic", items: links.classic },
    ] satisfies LinkGroup[]
  ).filter((g) => g.items.length > 0);
  if (groups.length === 0) {
    return (
      <div className="mt-2 text-[11px] font-mono text-fg-muted">No links generated yet.</div>
    );
  }
  return (
    <div className="mt-2 flex flex-col gap-1.5">
      {groups.map((g) => (
        <div key={g.key} className="flex items-center gap-2 min-w-0">
          <span className="text-[10px] font-mono uppercase tracking-wider text-fg-muted shrink-0 w-[56px]">
            {g.label}
          </span>
          <span className="font-mono text-xs text-fg truncate min-w-0 flex-1">
            {g.items[0]}
          </span>
          <CopyButton text={g.items[0] ?? ""} />
          {g.items.length > 1 && (
            <span className="text-[10px] font-mono text-fg-muted shrink-0">
              +{g.items.length - 1}
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

export interface DeployLinksCardProps {
  deployments: ClientDeploymentData[];
  secretPendingRedeploy?: boolean | undefined;
  agentLabels?: Record<string, string> | undefined;
}

export function DeployLinksCard({
  deployments,
  secretPendingRedeploy,
  agentLabels,
}: Readonly<DeployLinksCardProps>) {
  if (deployments.length === 0) {
    return (
      <div className="rounded-xs bg-bg-card border border-divider p-4 text-sm text-fg-muted text-center">
        No deployments yet.
      </div>
    );
  }
  return (
    <section className="rounded-xs bg-bg-card border border-divider overflow-hidden">
      <header className="px-4 py-3 border-b border-divider flex items-center justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-semibold text-fg">Deployments & links</span>
          <span className="text-[11px] font-mono text-fg-muted">
            {deployments.length} node{deployments.length === 1 ? "" : "s"}
          </span>
        </div>
        {secretPendingRedeploy && <Badge variant="warn">Secret rotated — awaiting redeploy</Badge>}
      </header>
      <div className="flex flex-col">
        {deployments.map((d) => {
          const tone = deployVariant(d.status);
          const label = agentLabels?.[d.agentId];
          return (
            <div key={d.agentId} className="px-4 py-3 border-b border-divider last:border-b-0">
              <div className="flex items-center gap-2 flex-wrap">
                <span className="font-mono text-sm text-fg truncate">
                  {label ?? d.agentId}
                </span>
                {label && (
                  <span className="font-mono text-[10px] text-fg-muted truncate">
                    {d.agentId.slice(0, 8)}…
                  </span>
                )}
                <Badge variant={tone}>{d.status}</Badge>
                {d.desiredOperation && d.desiredOperation !== "none" && (
                  <Badge variant="accent">{d.desiredOperation}</Badge>
                )}
                <span className="ml-auto text-[11px] font-mono text-fg-muted tabular-nums">
                  {d.lastAppliedAtUnix > 0
                    ? new Date(d.lastAppliedAtUnix * 1000).toLocaleString()
                    : "never applied"}
                </span>
              </div>
              {d.lastError && (
                <div className="mt-1 text-[11px] font-mono text-status-error break-words">
                  {d.lastError}
                </div>
              )}
              <LinksStrip links={d.links} />
            </div>
          );
        })}
      </div>
    </section>
  );
}
