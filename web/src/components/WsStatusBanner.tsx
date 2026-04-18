// P2-UX-10: visible cue that the realtime feed is disrupted.
//
// Shown when the WebSocket transitions into reconnecting or closed state.
// Hides itself while the socket is healthy so it does not occupy screen
// real estate during the 99%+ happy-path. The banner is intentionally
// unobtrusive — a single line at the top of the main content area.

import { useWsStatus } from "@/providers/EventsSynchronizer";

export function WsStatusBanner() {
  const { status, reconnectAttempts } = useWsStatus();

  if (status === "open" || status === "connecting") {
    return null;
  }

  const isReconnecting = status === "reconnecting";
  const label = isReconnecting
    ? reconnectAttempts > 1
      ? `Reconnecting to live feed (attempt ${reconnectAttempts})...`
      : "Reconnecting to live feed..."
    : "Live feed disconnected. Data may be stale.";

  return (
    <div
      role="status"
      aria-live="polite"
      className="sticky top-0 z-30 w-full bg-status-warn/15 border-b border-status-warn/30 text-status-warn text-xs px-4 py-1.5 flex items-center gap-2"
    >
      <span
        aria-hidden="true"
        className="inline-block w-2 h-2 rounded-full bg-status-warn animate-pulse"
      />
      <span>{label}</span>
    </div>
  );
}
