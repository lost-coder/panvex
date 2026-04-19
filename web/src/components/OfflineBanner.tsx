import { useOnlineStatus } from "@/shared/hooks";

/**
 * Shown when the OS reports no network connectivity. Rendered above the
 * WsStatusBanner so "no network" and "network present but server
 * unreachable" appear as distinct cues — operators triaging an outage
 * want to know immediately whether the problem is client-side or
 * panel-side. A polite live region keeps screen readers informed
 * without interrupting focus.
 */
export function OfflineBanner() {
  const online = useOnlineStatus();
  if (online) return null;
  return (
    <div
      role="status"
      aria-live="polite"
      aria-atomic="true"
      className="sticky top-0 z-40 w-full bg-status-error/20 border-b border-status-error/40 text-status-error text-xs px-4 py-1.5 flex items-center gap-2"
    >
      <span aria-hidden="true" className="inline-block w-2 h-2 rounded-full bg-status-error" />
      <span>Соединение потеряно — изменения не будут сохранены.</span>
    </div>
  );
}
