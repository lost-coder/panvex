import { cn } from "@/ui";
import type { ServerDetailPageProps } from "@/shared/api/types-pages/pages";

// ─── Upstreams list ───────────────────────────────────────────────────
export function UpstreamsList({
  upstreams,
}: {
  upstreams: ServerDetailPageProps["server"]["upstreams"];
}) {
  if (upstreams.length === 0) {
    return (
      <div className="text-xs font-mono text-fg-muted px-3 py-6 text-center">No upstreams reported.</div>
    );
  }
  return (
    <div className="flex flex-col gap-1.5">
      {upstreams.map((u) => (
        <div
          key={u.upstreamId}
          // Darker solid panel rows — opposite visual treatment from the
          // dashed Gates rows next to them. Reads as "these are distinct
          // things you can drill into" versus "these are state flags".
          className="flex items-center gap-2 px-3 py-2 rounded-xs bg-bg border border-divider"
        >
          <span
            className={cn(
              "h-1.5 w-1.5 rounded-full shrink-0",
              u.healthy ? "bg-status-ok" : "bg-status-error",
            )}
          />
          <span className="text-xs font-mono text-fg truncate">{u.address}</span>
          <span className="ml-auto text-[10px] font-mono text-fg-muted tabular-nums shrink-0">
            {u.effectiveLatencyMs != null ? `${u.effectiveLatencyMs.toFixed(0)}ms` : "—"}
          </span>
          <span className="text-[10px] font-mono text-fg-muted tabular-nums shrink-0">
            {u.routeKind}
          </span>
        </div>
      ))}
    </div>
  );
}
