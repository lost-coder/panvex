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
  const label = (() => {
    if (secs < 2) return "now";
    if (secs < 60) return `${secs}s`;
    return `${Math.floor(secs / 60)}m`;
  })();
  return { label, stale: secs > 10 };
}
