import { useWsStatus } from "@/app/providers/EventsSynchronizer";

/**
 * useEventAwareInterval returns the React Query refetchInterval that should
 * be used given the current WebSocket connection status.
 *
 * When the WS is `"open"`, mutations and state changes already arrive on
 * the live event channel — polling is reduced to a slow keep-alive (`slowMs`).
 * When the WS is connecting, reconnecting or closed, polling falls back to
 * the original fast cadence (`fastMs`) so the UI does not freeze if the live
 * feed is down.
 *
 * Closes audit pointers P-01 (polling storm while WS healthy) and BP-03
 * (shared util) — see AUDIT_2026-05-01.
 */
export function useEventAwareInterval(slowMs: number, fastMs: number): number {
  const { status } = useWsStatus();
  return status === "open" ? slowMs : fastMs;
}
