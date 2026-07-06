// P2-UX-10: subtle "data updated" affordance.
//
// Returns a boolean that flips true for ~1.2s whenever a new realtime event
// arrives. Callers apply a short transition class (e.g. ring / background
// flash) so operators notice that what they're looking at just refreshed.
//
// Intentionally coarse: any relevant event flips the flag — we don't try
// to scope it per-query yet (that would require per-query channels, which
// is a deeper refactor).

import { useEffect, useState } from "react";

import { useWsLastEventAt } from "@/app/providers/EventsSynchronizer";

const FLASH_DURATION_MS = 1_200;

export function useWsUpdateFlash(): boolean {
  // 7.1: единственный настоящий подписчик lastEventAt — читает store через
  // useSyncExternalStore-хук, а не общий контекст.
  const lastEventAt = useWsLastEventAt();
  const [flashing, setFlashing] = useState(false);

  useEffect(() => {
    if (lastEventAt === null) return;
    // R-Q-24: subscribing to lastEventAt is the canonical "react to an
    // external event" pattern; the cascading render the rule warns about
    // is the desired flash → reset cycle.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setFlashing(true);
    const id = globalThis.setTimeout(() => setFlashing(false), FLASH_DURATION_MS);
    return () => globalThis.clearTimeout(id);
  }, [lastEventAt]);

  return flashing;
}
