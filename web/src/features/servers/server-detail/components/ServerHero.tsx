import { StatusBeacon, cn, formatUptime } from "@/ui";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

import { ServerActionsDropdown } from "../ServerActionsDropdown";
import { RelativeTimeBadge } from "./RelativeTimeBadge";

/**
 * Desktop hero band (full-bleed border-y strip with name, status, meta
 * pills, and the actions dropdown). Hidden on mobile; phones use the
 * sticky `PageHeader` instead.
 */
export function ServerHero({
  server,
  pulseWord,
  relativeTime,
  relativeTimeStale,
  onReload,
  onBoostDetail,
  onRename,
  onChangeFleetGroup,
  onDeregister,
}: Readonly<{
  server: ServerDetailPageProps["server"];
  pulseWord: string;
  relativeTime: string | null;
  relativeTimeStale: boolean;
  onReload?: (() => void) | undefined;
  onBoostDetail?: (() => void) | undefined;
  onRename?: (() => void) | undefined;
  onChangeFleetGroup?: (() => void) | undefined;
  onDeregister?: (() => void) | undefined;
}>) {
  const { systemInfo } = server;
  return (
    <section className="hidden md:block border-y border-divider">
      <div className="px-4 md:px-8 py-4 flex flex-wrap items-center gap-x-4 gap-y-2">
        <StatusBeacon status={server.status} size="sm" />
        <h2 className="font-mono text-lg font-semibold text-fg truncate">{server.name}</h2>
        <span className="text-fg-faint">/</span>
        <span
          className={cn(
            "font-mono text-xs uppercase tracking-wider",
            (() => {
              if (server.status === "error") return "text-status-error";
              if (server.status === "warn") return "text-status-warn";
              return "text-status-ok";
            })(),
          )}
        >
          {pulseWord}
        </span>
        <div className="ml-auto flex items-center gap-2 flex-wrap justify-end">
          {server.ip && (
            <span className="font-mono text-[11px] text-fg-muted px-2 py-0.5 rounded-xs border border-divider bg-bg">
              {server.ip}
            </span>
          )}
          <span className="font-mono text-[11px] text-fg-muted px-2 py-0.5 rounded-xs border border-divider bg-bg">
            v{systemInfo.version}
          </span>
          <span className="font-mono text-[11px] text-fg-muted px-2 py-0.5 rounded-xs border border-divider bg-bg">
            up {formatUptime(systemInfo.uptimeSeconds)}
          </span>
          {systemInfo.configReloadCount > 0 && (
            <span className="font-mono text-[11px] text-fg-muted px-2 py-0.5 rounded-xs border border-divider bg-bg">
              reloads: {systemInfo.configReloadCount}
            </span>
          )}
          {server.telemtUnreachable && (
            <span className="font-mono text-[11px] text-neutral-400 px-2 py-0.5 rounded-xs border border-neutral-500/30 bg-neutral-500/10">
              Режим неизвестен
            </span>
          )}
          {relativeTime && <RelativeTimeBadge label={relativeTime} stale={relativeTimeStale} />}
          <ServerActionsDropdown
            onReload={onReload}
            onBoostDetail={onBoostDetail}
            onRename={onRename}
            onChangeFleetGroup={onChangeFleetGroup}
            onDeregister={onDeregister}
          />
        </div>
      </div>
    </section>
  );
}
