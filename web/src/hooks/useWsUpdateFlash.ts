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

import { useWsStatus } from "@/providers/EventsSynchronizer";

const FLASH_DURATION_MS = 1_200;

export function useWsUpdateFlash(): boolean {
  const { lastEventAt } = useWsStatus();
  const [flashing, setFlashing] = useState(false);

  useEffect(() => {
    if (lastEventAt === null) return;
    setFlashing(true);
    const id = window.setTimeout(() => setFlashing(false), FLASH_DURATION_MS);
    return () => window.clearTimeout(id);
  }, [lastEventAt]);

  return flashing;
}
