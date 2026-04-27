import { useEffect, useState } from "react";

/**
 * Tracks the browser's online/offline state via globalThis.navigator.onLine
 * plus the `online`/`offline` DOM events. These events fire when the
 * OS-level network layer flips, so they miss captive-portal-style
 * "connected but can't reach the server" failures — those already
 * surface via WsStatusBanner (WebSocket reconnecting) and the normal
 * per-request 5xx/timeout handling. This hook is the cheap, coarse
 * coverage for "no network at all".
 */
export function useOnlineStatus(): boolean {
  const [online, setOnline] = useState<boolean>(() =>
    typeof navigator === "undefined" ? true : navigator.onLine,
  );

  useEffect(() => {
    const handleOnline = () => setOnline(true);
    const handleOffline = () => setOnline(false);
    globalThis.addEventListener("online", handleOnline);
    globalThis.addEventListener("offline", handleOffline);
    return () => {
      globalThis.removeEventListener("online", handleOnline);
      globalThis.removeEventListener("offline", handleOffline);
    };
  }, []);

  return online;
}
