import { useState, useEffect } from "react";

export function useRelativeTime(date: Date | undefined): { label: string; stale: boolean } {
  const [now, setNow] = useState(Date.now);

  useEffect(() => {
    if (!date) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [date]);

  if (!date) return { label: "", stale: false };
  const secs = Math.round((now - date.getTime()) / 1000);
  const label = secs < 2 ? "now" : secs < 60 ? `${secs}s` : `${Math.floor(secs / 60)}m`;
  return { label, stale: secs > 10 };
}
